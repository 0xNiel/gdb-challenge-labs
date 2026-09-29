// Package api is labd's internal HTTP API (spec "Orchestrator → Internal HTTP API"). It
// listens on loopback only and every route except /healthz needs the bearer secret shared
// with web (S11).
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"

	"gdblabs/labd/internal/orch"
)

// Sessions is what the API needs from the session manager (*orch.Manager).
type Sessions interface {
	Start(ctx context.Context, req orch.StartReq) (orch.SessionInfo, error)
	Stop(ctx context.Context, id, reason string) (orch.SessionInfo, error)
	List() []orch.SessionInfo
	Stats() orch.Stats
}

// ReloadFunc re-reads labd.yaml and challenges.json and applies them; it returns a summary.
type ReloadFunc func(ctx context.Context) (map[string]any, error)

// RetryAfterS is what a client is told to wait when the queue is full.
const RetryAfterS = 30

// stopReasons are the reasons web may give for DELETE /internal/sessions/{id}.
var stopReasons = map[string]bool{orch.ReasonUserStop: true, orch.ReasonAdminKill: true, orch.ReasonSolved: true}

// Server holds the handlers.
type Server struct {
	sessions Sessions
	secret   []byte
	reload   ReloadFunc
	log      *slog.Logger
	// ReadRSS returns a lab's cgroup memory in MiB; replaceable in tests.
	ReadRSS func(cgroupPath string) float64
}

// New returns a Server. An empty secret is refused: the API must never run unauthenticated.
func New(sessions Sessions, secret string, reload ReloadFunc, log *slog.Logger) (*Server, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("LABD_INTERNAL_SECRET is empty; the internal API refuses to run without it (S11)")
	}
	return &Server{sessions: sessions, secret: []byte(secret), reload: reload, log: log, ReadRSS: cgroupRSSMiB}, nil
}

// Listen binds addr and refuses anything that is not a loopback address (S11). Config
// validation already checks this; this checks what was actually bound.
func Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	if ta, ok := ln.Addr().(*net.TCPAddr); !ok || !ta.IP.IsLoopback() {
		ln.Close()
		return nil, fmt.Errorf("internal API bound to %s, which is not loopback (S11)", ln.Addr())
	}
	return ln, nil
}

// Handler returns the router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.Handle("POST /internal/sessions", s.auth(s.startSession))
	mux.Handle("DELETE /internal/sessions/{id}", s.auth(s.stopSession))
	mux.Handle("GET /internal/sessions", s.auth(s.listSessions))
	mux.Handle("GET /internal/stats", s.auth(s.stats))
	mux.Handle("POST /internal/reload", s.auth(s.doReload))
	return mux
}

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), s.secret) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="labd"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}
		h(w, r)
	})
}

type startBody struct {
	UserID        int64  `json:"user_id"`
	ChallengeSlug string `json:"challenge_slug"`
}

// startResp is the spec's response. ws_token is filled by the gateway in Phase 3.
type startResp struct {
	SessionID     string     `json:"session_id"`
	WSToken       string     `json:"ws_token"`
	State         orch.State `json:"state"`
	QueuePosition int        `json:"queue_position"`
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	var b startBody
	if err := decode(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if b.UserID <= 0 || b.ChallengeSlug == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "user_id and challenge_slug are required")
		return
	}
	in, err := s.sessions.Start(r.Context(), orch.StartReq{UserID: b.UserID, ChallengeSlug: b.ChallengeSlug})
	switch {
	case errors.Is(err, orch.ErrQueueFull):
		w.Header().Set("Retry-After", fmt.Sprint(RetryAfterS))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "queue_full", "retry_after_s": RetryAfterS})
		return
	case errors.Is(err, orch.ErrUnknownChallenge):
		writeError(w, http.StatusNotFound, "unknown_challenge", b.ChallengeSlug)
		return
	case errors.Is(err, orch.ErrClosed):
		writeError(w, http.StatusServiceUnavailable, "shutting_down", "")
		return
	case err != nil:
		s.log.Error("start session", "user_id", b.UserID, "challenge", b.ChallengeSlug, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, startResp{SessionID: in.ID, State: in.State, QueuePosition: in.QueuePosition})
}

func (s *Server) stopSession(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &b); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	if b.Reason == "" {
		b.Reason = orch.ReasonUserStop
	}
	if !stopReasons[b.Reason] {
		writeError(w, http.StatusBadRequest, "bad_request", "reason must be user_stop, admin_kill or solved")
		return
	}
	in, err := s.sessions.Stop(r.Context(), r.PathValue("id"), b.Reason)
	if errors.Is(err, orch.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": in.ID, "state": in.State})
}

type sessionView struct {
	orch.SessionInfo
	RSSMB float64 `json:"rss_mb"`
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	list := s.sessions.List()
	out := make([]sessionView, 0, len(list))
	for _, in := range list {
		out = append(out, sessionView{SessionInfo: in, RSSMB: s.rss(in)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) rss(in orch.SessionInfo) float64 {
	if in.CgroupPath == "" || in.State != orch.StateRunning {
		return 0
	}
	return s.ReadRSS(in.CgroupPath)
}

func (s *Server) stats(w http.ResponseWriter, _ *http.Request) {
	st := s.sessions.Stats()
	var per []map[string]any
	for _, in := range s.sessions.List() {
		if in.State == orch.StateRunning {
			per = append(per, map[string]any{"session_id": in.ID, "rss_mb": s.rss(in)})
		}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	writeJSON(w, http.StatusOK, map[string]any{
		"active": st.Active, "creating": st.Creating, "running": st.Running, "ending": st.Ending,
		"queued": st.Queued, "max_sessions": st.MaxSessions, "slots_free": st.SlotsFree, "max_queue": st.MaxQueue,
		"host": readHost(),
		"labd": map[string]any{
			"goroutines": runtime.NumGoroutine(),
			"rss_mb":     selfRSSMiB(),
			"heap_mb":    round1(float64(ms.HeapAlloc) / (1 << 20)),
		},
		"sessions": per,
	})
}

func (s *Server) doReload(w http.ResponseWriter, r *http.Request) {
	if s.reload == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "")
		return
	}
	sum, err := s.reload(r.Context())
	if err != nil {
		s.log.Error("reload", "err", err)
		writeError(w, http.StatusInternalServerError, "reload_failed", err.Error())
		return
	}
	if sum == nil {
		sum = map[string]any{}
	}
	sum["ok"] = true
	writeJSON(w, http.StatusOK, sum)
}

// decode reads a small JSON body and rejects unknown fields.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, kind, detail string) {
	body := map[string]string{"error": kind}
	if detail != "" {
		body["detail"] = detail
	}
	writeJSON(w, code, body)
}
