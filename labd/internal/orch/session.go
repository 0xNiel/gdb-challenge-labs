package orch

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/store"
)

// State is a session's place in the spec's state machine ("Orchestrator → Session state").
type State string

const (
	StateQueued    State = "queued"
	StateCreating  State = "creating"
	StateRunning   State = "running"
	StateEnding    State = "ending"
	StateEnded     State = "ended"
	StateFailed    State = "failed"
	StateAbandoned State = "abandoned"
)

// End reasons. The first six are the spec's; the rest are labd's own outcomes.
const (
	ReasonIdle         = "idle_timeout"
	ReasonHardTTL      = "hard_ttl"
	ReasonWSClosed     = "ws_closed" // Phase 3
	ReasonUserStop     = "user_stop"
	ReasonAdminKill    = "admin_kill"
	ReasonSolved       = "solved"
	ReasonTaskExited   = "task_exited"   // the lab's process exited on its own
	ReasonQueueTimeout = "queue_timeout" // queued for longer than the queue timeout
	ReasonCreateFailed = "create_failed" // image missing, runsc error, ...
	ReasonReconciled   = "reconciled"    // closed by the reconciler after a labd restart
)

// Container labels. The first four are the spec's; the limits make a container
// self-describing, so the reconciler needs no external state to rebuild its timers.
const (
	LabelSessionID   = "lab.session_id"
	LabelUserID      = "lab.user_id"
	LabelChallenge   = "lab.challenge"
	LabelCreatedAt   = "lab.created_at" // RFC 3339, UTC; the hard TTL counts from here
	LabelTTLMinutes  = "lab.ttl_minutes"
	LabelIdleMinutes = "lab.idle_minutes"
)

// Challenge is one entry of challenges.json as labd needs it.
type Challenge struct {
	Slug    string        `json:"slug"`
	Image   string        `json:"image"`
	Limits  config.Limits `json:"limits"`
	Enabled bool          `json:"enabled"`
}

// Session is a lab session. Every field is guarded by Manager.mu.
type Session struct {
	ID            string
	ContainerID   string
	UserID        int64
	ChallengeSlug string
	Image         string
	Limits        config.Limits // challenge limits merged with the defaults
	State         State
	EndReason     string
	Extended      bool
	CreatedAt     time.Time // request received
	AdmittedAt    time.Time // slot acquired; the container's lab.created_at
	StartedAt     time.Time // task running
	EndedAt       time.Time

	ctr          Container
	out          *Output
	idle         clock.Timer
	hard         clock.Timer
	abandon      clock.Timer
	idleWindow   time.Duration
	idleDeadline time.Time
	hardDeadline time.Time
	stopReason   string     // a stop requested while the container was being created
	writeMu      sync.Mutex // serialises row writes so the newest state is written last

	grace    clock.Timer   // armed while no WebSocket is attached (Phase 3)
	connGen  int           // generation of the current WebSocket; 0 when none ever attached
	attached bool          // a WebSocket is attached now
	ended    chan struct{} // closed when the session is ended, failed or abandoned
	commands int           // command_entered lines seen by the gateway
}

// SessionInfo is a copy of a session's public state.
type SessionInfo struct {
	ID             string    `json:"session_id"`
	UserID         int64     `json:"user_id"`
	ChallengeSlug  string    `json:"challenge_slug"`
	State          State     `json:"state"`
	QueuePosition  int       `json:"queue_position"` // 1-based; 0 when not queued
	EndReason      string    `json:"end_reason,omitempty"`
	Extended       bool      `json:"extended"`
	CreatedAt      time.Time `json:"created_at"`
	StartedAt      time.Time `json:"started_at,omitzero"`
	EndedAt        time.Time `json:"ended_at,omitzero"`
	StartLatencyMS int64     `json:"start_latency_ms,omitempty"` // slot acquired to task running
	IdleDeadline   time.Time `json:"idle_deadline,omitzero"`
	HardDeadline   time.Time `json:"hard_deadline,omitzero"`
	ContainerID    string    `json:"container_id,omitempty"`
	Commands       int       `json:"commands"`
	Attached       bool      `json:"ws_attached"`
	CgroupPath     string    `json:"-"`
}

func (s *Session) info(pos int) SessionInfo {
	in := SessionInfo{
		ID: s.ID, UserID: s.UserID, ChallengeSlug: s.ChallengeSlug, State: s.State,
		QueuePosition: pos, EndReason: s.EndReason, Extended: s.Extended,
		CreatedAt: s.CreatedAt, StartedAt: s.StartedAt, EndedAt: s.EndedAt,
		IdleDeadline: s.idleDeadline, HardDeadline: s.hardDeadline, ContainerID: s.ContainerID,
		Commands: s.commands, Attached: s.attached,
	}
	if !s.StartedAt.IsZero() && !s.AdmittedAt.IsZero() {
		in.StartLatencyMS = s.StartedAt.Sub(s.AdmittedAt).Milliseconds()
	}
	if s.ctr != nil {
		in.CgroupPath = s.ctr.CgroupPath()
	}
	return in
}

func (s *Session) record() store.Session {
	return store.Session{
		ID: s.ID, UserID: s.UserID, ChallengeSlug: s.ChallengeSlug, Image: s.Image,
		State: string(s.State), CreatedAt: s.CreatedAt, StartedAt: s.StartedAt, EndedAt: s.EndedAt,
		EndReason: s.EndReason, ContainerID: s.ContainerID, Extended: s.Extended, Commands: s.commands,
	}
}

// Output keeps the last OutputScrollback bytes a lab's terminal printed and passes new output
// to at most a few subscribers (the gateway has one per session). Delivery to a subscriber
// blocks while its channel is full: that is the backpressure the spec asks for, and the
// gateway ends a subscription that stays full for too long (slow consumer), which unblocks
// the terminal again.
type Output struct {
	mu   sync.Mutex
	buf  []byte
	subs map[*outSub]struct{}
}

type outSub struct {
	ch   chan []byte
	gone chan struct{}
	once sync.Once
}

// OutputScrollback is how much terminal output a session keeps for a reconnecting client.
const OutputScrollback = 64 << 10

func newOutput() *Output { return &Output{subs: map[*outSub]struct{}{}} }

func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.buf = append(o.buf, p...)
	if over := len(o.buf) - OutputScrollback; over > 0 {
		o.buf = append(o.buf[:0], o.buf[over:]...)
	}
	subs := make([]*outSub, 0, len(o.subs))
	for s := range o.subs {
		subs = append(subs, s)
	}
	o.mu.Unlock()
	for _, s := range subs {
		select {
		case s.ch <- append([]byte(nil), p...):
		case <-s.gone:
		}
	}
	return len(p), nil
}

// Snapshot returns the scrollback.
func (o *Output) Snapshot() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.buf...)
}

// Subscribe returns the scrollback so far and a channel (capacity depth) of everything
// written after it, with nothing lost in between. cancel ends the subscription; call it
// exactly when done reading, or the terminal blocks once the channel fills.
func (o *Output) Subscribe(depth int) (scrollback []byte, ch <-chan []byte, cancel func()) {
	s := &outSub{ch: make(chan []byte, depth), gone: make(chan struct{})}
	o.mu.Lock()
	scrollback = append([]byte(nil), o.buf...)
	o.subs[s] = struct{}{}
	o.mu.Unlock()
	return scrollback, s.ch, func() {
		s.once.Do(func() {
			close(s.gone)
			o.mu.Lock()
			delete(o.subs, s)
			o.mu.Unlock()
		})
	}
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
