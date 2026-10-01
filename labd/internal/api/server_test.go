package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
)

const secret = "test-secret"

// stubSessions behaves like the manager for one slot and a queue of one.
type stubSessions struct {
	mu     sync.Mutex
	byUser map[int64]orch.SessionInfo
	byID   map[string]orch.SessionInfo
	n      int
	drain  bool
}

func newStub() *stubSessions {
	return &stubSessions{byUser: map[int64]orch.SessionInfo{}, byID: map[string]orch.SessionInfo{}}
}

func (s *stubSessions) Start(_ context.Context, r orch.StartReq) (orch.SessionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ChallengeSlug != "perf" {
		return orch.SessionInfo{}, orch.ErrUnknownChallenge
	}
	if in, ok := s.byUser[r.UserID]; ok {
		return in, nil
	}
	in := orch.SessionInfo{UserID: r.UserID, ChallengeSlug: r.ChallengeSlug, CgroupPath: "/cg"}
	switch len(s.byID) {
	case 0:
		in.State = orch.StateCreating
	case 1:
		in.State, in.QueuePosition = orch.StateQueued, 1
	default:
		return orch.SessionInfo{}, orch.ErrQueueFull
	}
	s.n++
	in.ID = "s" + string(rune('0'+s.n))
	s.byUser[r.UserID], s.byID[in.ID] = in, in
	return in, nil
}

func (s *stubSessions) Stop(_ context.Context, id, _ string) (orch.SessionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, ok := s.byID[id]
	if !ok {
		return orch.SessionInfo{}, orch.ErrNotFound
	}
	delete(s.byID, id)
	delete(s.byUser, in.UserID)
	in.State = orch.StateEnding
	return in, nil
}

func (s *stubSessions) List() []orch.SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []orch.SessionInfo
	for _, in := range s.byID {
		in.State = orch.StateRunning
		out = append(out, in)
	}
	return out
}

func (s *stubSessions) Stats() orch.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := orch.Stats{Active: 1, Running: 1, MaxSessions: 1, MaxQueue: 1, ConfiguredMaxSessions: 1, Draining: s.drain}
	if s.drain {
		st.MaxSessions = 0
	}
	return st
}

func (s *stubSessions) SetDrain(on bool) { s.mu.Lock(); s.drain = on; s.mu.Unlock() }

func newTestServer(t *testing.T, reload ReloadFunc) *httptest.Server {
	t.Helper()
	srv, err := New(newStub(), secret, reload, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.ReadRSS = func(string) float64 { return 23.5 }
	srv.MintToken = func(id string, uid int64) string { return fmt.Sprintf("tok-%s-%d", id, uid) }
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func call(t *testing.T, ts *httptest.Server, method, path, body, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPI_AuthRequired(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	for _, r := range []struct{ method, path string }{
		{"POST", "/internal/sessions"}, {"DELETE", "/internal/sessions/x"}, {"GET", "/internal/sessions"},
		{"GET", "/internal/stats"}, {"POST", "/internal/reload"}, {"POST", "/internal/drain"},
	} {
		for _, tok := range []string{"", "wrong", secret + "x"} {
			if code, _ := call(t, ts, r.method, r.path, `{}`, tok); code != http.StatusUnauthorized {
				t.Errorf("%s %s with token %q: %d, want 401", r.method, r.path, tok, code)
			}
		}
	}
	if code, body := call(t, ts, "GET", "/healthz", "", ""); code != 200 || body["ok"] != true {
		t.Fatalf("healthz without auth: %d %v", code, body)
	}
}

func TestAPI_StartSameUserStop(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	code, a := call(t, ts, "POST", "/internal/sessions", `{"user_id":1,"challenge_slug":"perf"}`, secret)
	if code != 200 || a["state"] != "creating" || a["session_id"] == "" {
		t.Fatalf("start: %d %v", code, a)
	}
	if a["ws_token"] != "tok-"+a["session_id"].(string)+"-1" {
		t.Fatalf("ws_token %v", a["ws_token"])
	}
	for _, k := range []string{"session_id", "ws_token", "state", "queue_position"} {
		if _, ok := a[k]; !ok {
			t.Errorf("start response lacks %s", k)
		}
	}
	_, again := call(t, ts, "POST", "/internal/sessions", `{"user_id":1,"challenge_slug":"perf"}`, secret)
	if again["session_id"] != a["session_id"] {
		t.Fatalf("same user got %v, want %v", again["session_id"], a["session_id"])
	}
	_, q := call(t, ts, "POST", "/internal/sessions", `{"user_id":2,"challenge_slug":"perf"}`, secret)
	if q["state"] != "queued" || q["queue_position"] != float64(1) {
		t.Fatalf("second user: %v", q)
	}

	id := a["session_id"].(string)
	if code, body := call(t, ts, "DELETE", "/internal/sessions/"+id, `{"reason":"admin_kill"}`, secret); code != 200 || body["state"] != "ending" {
		t.Fatalf("delete: %d %v", code, body)
	}
	if code, _ := call(t, ts, "DELETE", "/internal/sessions/"+id, "", secret); code != 404 {
		t.Fatalf("delete twice: %d, want 404", code)
	}
	if code, _ := call(t, ts, "DELETE", "/internal/sessions/s2", `{"reason":"because"}`, secret); code != 400 {
		t.Fatalf("bad reason: %d, want 400", code)
	}
}

func TestAPI_StartErrors(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	for body, want := range map[string]int{
		`{"user_id":1,"challenge_slug":"nope"}`:       404,
		`{"user_id":0,"challenge_slug":"perf"}`:       400,
		`{"user_id":1}`:                               400,
		`not json`:                                    400,
		`{"user_id":1,"challenge_slug":"perf","x":1}`: 400,
	} {
		if code, _ := call(t, ts, "POST", "/internal/sessions", body, secret); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	call(t, ts, "POST", "/internal/sessions", `{"user_id":1,"challenge_slug":"perf"}`, secret)
	call(t, ts, "POST", "/internal/sessions", `{"user_id":2,"challenge_slug":"perf"}`, secret)
	code, body := call(t, ts, "POST", "/internal/sessions", `{"user_id":3,"challenge_slug":"perf"}`, secret)
	if code != 503 || body["error"] != "queue_full" || body["retry_after_s"] != float64(RetryAfterS) {
		t.Fatalf("queue full: %d %v", code, body)
	}
}

func TestAPI_ListAndStats(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	call(t, ts, "POST", "/internal/sessions", `{"user_id":1,"challenge_slug":"perf"}`, secret)
	code, body := call(t, ts, "GET", "/internal/sessions", "", secret)
	list, _ := body["sessions"].([]any)
	if code != 200 || len(list) != 1 || list[0].(map[string]any)["rss_mb"] != 23.5 {
		t.Fatalf("list: %d %v", code, body)
	}
	code, st := call(t, ts, "GET", "/internal/stats", "", secret)
	if code != 200 {
		t.Fatalf("stats: %d", code)
	}
	for _, k := range []string{"active", "queued", "slots_free", "max_sessions", "host", "labd", "sessions",
		"draining", "configured_max_sessions", "pending_pull"} {
		if _, ok := st[k]; !ok {
			t.Errorf("stats lacks %s", k)
		}
	}
	labd, _ := st["labd"].(map[string]any)
	if g, _ := labd["goroutines"].(float64); g < 1 {
		t.Fatalf("labd.goroutines %v", labd["goroutines"])
	}
}

func TestAPI_ReloadRereadsConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "labd.yaml")
	write := func(n string) {
		if err := os.WriteFile(path, []byte("max_sessions: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reload := func(context.Context) (map[string]any, error) {
		cfg, err := config.Load(path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"max_sessions": cfg.MaxSessions}, nil
	}
	ts := newTestServer(t, reload)
	write("7")
	if code, body := call(t, ts, "POST", "/internal/reload", "", secret); code != 200 || body["max_sessions"] != float64(7) {
		t.Fatalf("reload: %d %v", code, body)
	}
	write("3")
	if _, body := call(t, ts, "POST", "/internal/reload", "", secret); body["max_sessions"] != float64(3) {
		t.Fatalf("reload did not re-read the file: %v", body)
	}
	write("-1")
	if code, body := call(t, ts, "POST", "/internal/reload", "", secret); code != 500 || body["error"] != "reload_failed" {
		t.Fatalf("invalid config: %d %v", code, body)
	}
}

func TestAPI_RefusesEmptySecretAndNonLoopback(t *testing.T) {
	t.Parallel()
	if _, err := New(newStub(), "  ", nil, slog.Default()); err == nil {
		t.Fatal("empty secret accepted")
	}
	if ln, err := Listen("0.0.0.0:0"); err == nil {
		ln.Close()
		t.Fatal("non-loopback listen accepted")
	}
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

// ADR 0018: POST /internal/drain turns draining on and off and answers with the stats.
func TestAPI_Drain(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	code, st := call(t, ts, "POST", "/internal/drain", `{"drain": true}`, secret)
	if code != 200 || st["draining"] != true || st["max_sessions"] != 0.0 || st["configured_max_sessions"] != 1.0 {
		t.Fatalf("drain on: %d %v", code, st)
	}
	code, st = call(t, ts, "POST", "/internal/drain", `{"drain": false}`, secret)
	if code != 200 || st["draining"] != false || st["max_sessions"] != 1.0 {
		t.Fatalf("drain off: %d %v", code, st)
	}
	for _, bad := range []string{`{}`, `{"drain":"yes"}`, `not json`} {
		if code, _ := call(t, ts, "POST", "/internal/drain", bad, secret); code != 400 {
			t.Errorf("body %s: %d, want 400", bad, code)
		}
	}
}

func TestAPI_PendingPull(t *testing.T) {
	t.Parallel()
	srv, err := New(newStub(), secret, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.ReadRSS = func(string) float64 { return 0 }
	srv.PendingPull = func(context.Context) int { return 3 }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	if _, st := call(t, ts, "GET", "/internal/stats", "", secret); st["pending_pull"] != 3.0 {
		t.Fatalf("pending_pull %v", st["pending_pull"])
	}
}
