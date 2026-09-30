package term

import (
	"context"
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
}

// readLimit caps one incoming WebSocket message (ADR 0004).
const readLimit = 64 << 10

// Server is the WebSocket gateway.
type Server struct {
	o Options

	mu    sync.Mutex
	conns map[string]*conn        // the live connection per session
	state map[string]*sessionTerm // per-session state that survives reconnects

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
	// Origin first, so a cross-site page cannot burn a user's single-use token.
	if !s.originOK(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	uid, err := s.o.Tokens.Verify(r.URL.Query().Get("t"), id, s.o.Clock.Now())
	if err != nil {
		s.o.Log.Info("ws: token rejected", "session_id", id, "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	info, err := s.o.Sessions.Get(id)
	if err != nil {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if info.UserID != uid {
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
	c := &conn{srv: s, ws: ws, id: id, uid: uid, ctrl: make(chan []byte, 16)}
	c.run(r.Context(), info)
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

// takeOver registers c as the session's connection and closes the one it replaces.
func (s *Server) takeOver(c *conn) {
	s.mu.Lock()
	old := s.conns[c.id]
	s.conns[c.id] = c
	s.mu.Unlock()
	if old != nil {
		old.replaced()
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
