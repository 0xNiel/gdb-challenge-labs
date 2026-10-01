package term

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/orch"
)

// Sessions is what the gateway needs from the session manager (*orch.Manager).
type Sessions interface {
	Get(id string) (orch.SessionInfo, error)
	WriteInput(id string, p []byte) error
	Output(id string) (*orch.Output, error)
	Resize(ctx context.Context, id string, cols, rows uint32) error
	Extend(id string) (orch.SessionInfo, error)
	ClientAttached(id string) (int, error)
	ClientDetached(id string, gen int)
	Ended(id string) (<-chan struct{}, func() orch.SessionInfo, error)
	AddCommands(id string, n int)
}

// Options configures a Server. Zero values take the spec's defaults.
type Options struct {
	Sessions Sessions
	Tokens   *Tokens
	Recorder *Recorder // command capture; nil disables it (tests)
	Clock    clock.Clock
	Log      *slog.Logger

	// SiteOrigin is the only Origin accepted, e.g. "https://labs.example.com" (S12).
	SiteOrigin string
	// AllowNoOrigin accepts requests without an Origin header (dev page, Go client).
	AllowNoOrigin bool

	InputRate, InputBurst, OutputRate, OutputBurst int
	SlowConsumer                                   time.Duration // output stuck this long closes the socket (spec 10 s)
	TTLEvery                                       time.Duration // ttl frame period (spec 30 s)
	QueuePoll                                      time.Duration // how often a queued client's position is refreshed
	CreatingPoll                                   time.Duration // how often a creating session is checked for running
	OutDepth                                       int           // output frames buffered per connection (plan: 256)
}

func (o *Options) defaults() {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	def := func(v *int, d int) {
		if *v == 0 {
			*v = d
		}
	}
	def(&o.InputRate, InputRate)
	def(&o.InputBurst, InputBurst)
	def(&o.OutputRate, OutputRate)
	def(&o.OutputBurst, OutputBurst)
	def(&o.OutDepth, 256)
	defd := func(v *time.Duration, d time.Duration) {
		if *v == 0 {
			*v = d
		}
	}
	defd(&o.SlowConsumer, 10*time.Second)
	defd(&o.TTLEvery, 30*time.Second)
	defd(&o.QueuePoll, time.Second)
	// A lab is created in about 250 ms; polling it once a second added up to a second to
	// every start (Phase 4, P2: request to "running" was 1006 ms for every session).
	defd(&o.CreatingPoll, 50*time.Millisecond)
}

// readLimit caps one incoming WebSocket message (ADR 0004).
const readLimit = 64 << 10

// Server is the WebSocket gateway.
type Server struct {
	o Options

	mu      sync.Mutex
	conns   map[string]*conn        // the live connection per session
	state   map[string]*sessionTerm // per-session state that survives reconnects
	closing bool                    // Shutdown has begun; new connections are refused

	// active counts handlers in flight. http.Server.Shutdown does not track hijacked
	// connections, so the gateway waits for its own in Shutdown.
	active sync.WaitGroup

	unknownFrames atomic.Int64
}

// sessionTerm is per-session gateway state: the input bucket and the capture splitter
// outlive a single connection, so a reconnect cannot reset either.
type sessionTerm struct {
	mu    sync.Mutex
	in    *inputLimiter
	split Splitter
	seq   int
}

// New returns a gateway.
func New(o Options) *Server {
	o.defaults()
	return &Server{o: o, conns: map[string]*conn{}, state: map[string]*sessionTerm{}}
}

// Handler serves GET /ws/term/{session_id}?t=<token>.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/term/{id}", s.serveWS)
	return mux
}

// UnknownFrames counts control frames of an unknown type (they are ignored).
func (s *Server) UnknownFrames() int64 { return s.unknownFrames.Load() }

func (s *Server) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return s.o.AllowNoOrigin
	}
	return s.o.SiteOrigin != "" && strings.EqualFold(origin, s.o.SiteOrigin)
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	remote := remoteOf(r)
	// Refused before the token is looked at, so a shutdown cannot burn it.
	if !s.enter() {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	defer s.active.Done()
	// Origin first, so a cross-site page cannot burn a user's single-use token.
	if !s.originOK(r) {
		s.o.Log.Info("ws: origin rejected", "session_id", id, "origin", r.Header.Get("Origin"), "remote", remote)
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	uid, err := s.o.Tokens.Verify(r.URL.Query().Get("t"), id, s.o.Clock.Now())
	if err != nil {
		s.o.Log.Info("ws: token rejected", "session_id", id, "err", err, "remote", remote)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	info, err := s.o.Sessions.Get(id)
	if err != nil {
		s.o.Log.Info("ws: no such session", "session_id", id, "user_id", uid, "remote", remote)
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if info.UserID != uid {
		s.o.Log.Info("ws: token user does not own the session", "session_id", id, "user_id", uid, "remote", remote)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Origin checked above against site_host, not the Host header
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return // Accept has written the HTTP error
	}
	ws.SetReadLimit(readLimit)
	c := &conn{srv: s, ws: ws, id: id, uid: uid, remote: remote, since: s.o.Clock.Now(),
		peerCode: -1, ctrl: make(chan []byte, 16)}
	s.o.Log.Info("ws: connected", "session_id", id, "user_id", uid, "remote", remote, "state", info.State)
	c.run(r.Context(), info)
}

// remoteOf is the peer address, plus the client Caddy reports in production. The
// forwarded address is for the log only; nothing trusts it.
func remoteOf(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return r.RemoteAddr + " (for " + fwd + ")"
	}
	return r.RemoteAddr
}

// enter admits one handler unless Shutdown has begun.
func (s *Server) enter() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.active.Add(1)
	return true
}

// Shutdown closes every connection with 1000 and waits, until ctx is done, for their
// handlers to finish. Labs keep running: the next labd adopts them within the reconnect
// grace, and clients reconnect to it. Later connections get 503.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	conns := make([]*conn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	if len(conns) > 0 {
		s.o.Log.Info("ws: closing connections for shutdown", "connections", len(conns))
	}
	for _, c := range conns {
		c.closeBy(closedByShutdown, websocket.StatusNormalClosure, "")
	}
	done := make(chan struct{})
	go func() { s.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("ws: connections still closing: %w", ctx.Err())
	}
}

// termState returns the per-session state, creating it on first use.
func (s *Server) termState(id string) *sessionTerm {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state[id]
	if st == nil {
		st = &sessionTerm{in: newInputLimiter(s.o.InputRate, s.o.InputBurst)}
		s.state[id] = st
	}
	return st
}

// takeOver registers c as the session's connection and closes the one it replaces. A
// connection that arrives while Shutdown runs is closed at once.
func (s *Server) takeOver(c *conn) {
	s.mu.Lock()
	old := s.conns[c.id]
	s.conns[c.id] = c
	closing := s.closing
	s.mu.Unlock()
	if old != nil {
		s.o.Log.Info("ws: replaced", "session_id", c.id, "user_id", c.uid, "old_remote", old.remote, "new_remote", c.remote)
		old.replaced()
	}
	if closing {
		c.closeBy(closedByShutdown, websocket.StatusNormalClosure, "")
	}
}

// release forgets c if it is still the session's connection.
func (s *Server) release(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[c.id] == c {
		delete(s.conns, c.id)
	}
}

func (s *Server) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state, id)
}
