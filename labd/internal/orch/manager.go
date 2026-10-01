package orch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/store"
)

// Errors returned by the Manager. The API maps them to status codes.
var (
	ErrNotFound         = errors.New("session not found")
	ErrQueueFull        = errors.New("queue full")
	ErrUnknownChallenge = errors.New("unknown or disabled challenge")
	ErrNotRunning       = errors.New("session is not running")
	ErrAlreadyExtended  = errors.New("session already extended")
	ErrClosed           = errors.New("manager closed")
)

// DefaultQueueTimeout is how long a session may wait in the queue (spec: 2 min).
const DefaultQueueTimeout = 2 * time.Minute

// opTimeout bounds one create or teardown against containerd.
const opTimeout = 60 * time.Second

// ManagerConfig is what the Manager needs at start. Everything but BaseSpec and Runtime can
// change later through the Set* methods (config reload).
type ManagerConfig struct {
	BaseSpec      []byte // labd/sandbox/sandbox-base.json
	Runtime       string // RuntimeRunsc in production (S1)
	MaxSessions   int
	MaxQueue      int
	DefaultLimits config.Limits
	QueueTimeout  time.Duration // 0 means DefaultQueueTimeout
	WSGrace       time.Duration // ws_reconnect_grace_s; 0 means DefaultWSGrace
	Challenges    []Challenge
}

// DefaultWSGrace is how long a running lab waits for its WebSocket to come back (spec: 60 s).
const DefaultWSGrace = 60 * time.Second

// Manager owns every session: the slot semaphore, the per-user cap, the FIFO queue and the
// idle and hard timers (spec "Orchestrator → Concurrency cap").
//
// The semaphore is a counter under mu rather than a buffered channel, so the cap can be
// lowered at runtime by withholding slots as they free, without killing anything.
type Manager struct {
	rt   Runtime
	st   store.Store
	clk  clock.Clock
	log  *slog.Logger
	base []byte
	rtID string

	mu           sync.Mutex
	closed       bool
	cap          int
	maxQueue     int
	inUse        int // sessions holding a slot: creating, running, ending
	defaults     config.Limits
	queueTimeout time.Duration
	wsGrace      time.Duration
	challenges   map[string]Challenge
	sessions     map[string]*Session // every session not yet ended, failed or abandoned
	byUser       map[int64]*Session  // the user's queued, creating or running session
	queue        []*Session          // FIFO; head is next to be admitted

	ops sync.WaitGroup // create and teardown goroutines
}

// NewManager returns a Manager with no sessions. Call Reconcile before serving requests.
func NewManager(cfg ManagerConfig, rt Runtime, st store.Store, clk clock.Clock, log *slog.Logger) *Manager {
	if cfg.QueueTimeout == 0 {
		cfg.QueueTimeout = DefaultQueueTimeout
	}
	if cfg.Runtime == "" {
		cfg.Runtime = RuntimeRunsc
	}
	if cfg.WSGrace == 0 {
		cfg.WSGrace = DefaultWSGrace
	}
	m := &Manager{
		rt: rt, st: st, clk: clk, log: log, base: cfg.BaseSpec, rtID: cfg.Runtime,
		cap: cfg.MaxSessions, maxQueue: cfg.MaxQueue, defaults: cfg.DefaultLimits,
		queueTimeout: cfg.QueueTimeout, wsGrace: cfg.WSGrace,
		sessions: map[string]*Session{}, byUser: map[int64]*Session{},
	}
	m.SetChallenges(cfg.Challenges)
	return m
}

// StartReq is a request to start a lab.
type StartReq struct {
	UserID        int64
	ChallengeSlug string
}

// Start returns the user's existing queued, creating or running session, or starts a new
// one: admitted if a slot is free and nobody is queued, queued if the queue has room,
// ErrQueueFull otherwise. Creation continues in the background; poll Get or List.
func (m *Manager) Start(_ context.Context, req StartReq) (SessionInfo, error) {
	if req.UserID <= 0 || req.ChallengeSlug == "" {
		return SessionInfo{}, fmt.Errorf("user_id and challenge_slug are required")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return SessionInfo{}, ErrClosed
	}
	if s := m.byUser[req.UserID]; s != nil {
		in := s.info(m.positionLocked(s))
		m.mu.Unlock()
		return in, nil
	}
	ch, ok := m.challenges[req.ChallengeSlug]
	if !ok || !ch.Enabled {
		m.mu.Unlock()
		return SessionInfo{}, ErrUnknownChallenge
	}
	now := m.clk.Now()
	s := &Session{
		ID: newUUID(), UserID: req.UserID, ChallengeSlug: ch.Slug, Image: ch.Image,
		Limits: mergeLimits(ch.Limits, m.defaults), CreatedAt: now, out: newOutput(),
		ended: make(chan struct{}),
	}
	s.ContainerID = "lab-" + s.ID
	var admitted bool
	switch {
	case m.inUse < m.cap && len(m.queue) == 0:
		m.admitLocked(s, now)
		admitted = true
	case len(m.queue) < m.maxQueue:
		s.State = StateQueued
		m.queue = append(m.queue, s)
		id := s.ID
		s.abandon = m.clk.AfterFunc(m.queueTimeout, func() { m.abandon(id) })
	default:
		m.mu.Unlock()
		return SessionInfo{}, ErrQueueFull
	}
	m.sessions[s.ID] = s
	m.byUser[s.UserID] = s
	in := s.info(m.positionLocked(s))
	m.mu.Unlock()

	m.persist(s)
	m.event(s, "lab_requested", nil)
	if admitted {
		m.launchCreate(s)
	} else {
		m.event(s, "lab_queued", map[string]any{"position": in.QueuePosition})
	}
	return in, nil
}

// Stop ends a session for reason (user_stop, admin_kill, ...). A queued session is
// abandoned, a running one is torn down in the background (state ending), and one being
// created is torn down as soon as creation finishes. Stopping an ending session is a no-op.
func (m *Manager) Stop(_ context.Context, id, reason string) (SessionInfo, error) {
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil {
		m.mu.Unlock()
		return SessionInfo{}, ErrNotFound
	}
	switch s.State {
	case StateQueued:
		m.removeQueuedLocked(s, reason)
		in := s.info(0)
		m.mu.Unlock()
		m.persist(s)
		m.event(s, "lab_ended", map[string]any{"reason": reason, "duration_s": 0})
		return in, nil
	case StateCreating:
		if s.stopReason == "" {
			s.stopReason = reason
		}
		// The user may start a new lab at once; this one is torn down when its create ends.
		if m.byUser[s.UserID] == s {
			delete(m.byUser, s.UserID)
		}
	case StateRunning:
		m.beginEndLocked(s, reason)
		in := s.info(0)
		m.mu.Unlock()
		m.persist(s)
		m.launchTeardown(s)
		return in, nil
	}
	in := s.info(0)
	m.mu.Unlock()
	return in, nil
}

// Touch records terminal activity: it resets the idle timer. Ignored unless running.
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil && s.State == StateRunning {
		m.armIdleLocked(s)
	}
}

// Extend lengthens a running session's idle window by extend_minutes, once per session
// (spec: "extend (once, +15 min idle budget)"). The hard TTL does not move.
func (m *Manager) Extend(id string) (SessionInfo, error) {
	m.mu.Lock()
	s := m.sessions[id]
	switch {
	case s == nil:
		m.mu.Unlock()
		return SessionInfo{}, ErrNotFound
	case s.State != StateRunning:
		m.mu.Unlock()
		return SessionInfo{}, ErrNotRunning
	case s.Extended:
		m.mu.Unlock()
		return SessionInfo{}, ErrAlreadyExtended
	}
	s.Extended = true
	s.idleWindow += minutes(s.Limits.ExtendMinutes)
	m.armIdleLocked(s)
	in := s.info(0)
	m.mu.Unlock()
	m.persist(s)
	return in, nil
}

// WriteInput sends keystrokes to a running session's terminal and counts as activity.
func (m *Manager) WriteInput(id string, p []byte) error {
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil || s.State != StateRunning {
		m.mu.Unlock()
		return ErrNotRunning
	}
	m.armIdleLocked(s)
	stdin, _ := s.ctr.IO()
	m.mu.Unlock()
	_, err := stdin.Write(p)
	return err
}

// Output returns a session's terminal output (scrollback and live).
func (m *Manager) Output(id string) (*Output, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil, ErrNotFound
	}
	return s.out, nil
}

// Get returns one live session.
func (m *Manager) Get(id string) (SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return SessionInfo{}, ErrNotFound
	}
	return s.info(m.positionLocked(s)), nil
}

// List returns every live session (queued through ending), oldest first.
func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.info(m.positionLocked(s)))
	}
	slices.SortFunc(out, func(a, b SessionInfo) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// Stats are the counters behind GET /internal/stats.
type Stats struct {
	Active      int `json:"active"` // sessions holding a slot: creating + running + ending
	Creating    int `json:"creating"`
	Running     int `json:"running"`
	Ending      int `json:"ending"`
	Queued      int `json:"queued"`
	MaxSessions int `json:"max_sessions"`
	SlotsFree   int `json:"slots_free"`
	MaxQueue    int `json:"max_queue"`
}

// Stats returns the current counters.
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Stats{Active: m.inUse, Queued: len(m.queue), MaxSessions: m.cap, MaxQueue: m.maxQueue,
		SlotsFree: max(m.cap-m.inUse, 0)}
	for _, s := range m.sessions {
		switch s.State {
		case StateCreating:
			st.Creating++
		case StateRunning:
			st.Running++
		case StateEnding:
			st.Ending++
		}
	}
	return st
}

// SetCap changes max_sessions. Raising it admits queued sessions at once; lowering it
// withholds slots as sessions end and never kills a running one (spec).
func (m *Manager) SetCap(n int) {
	m.mu.Lock()
	m.cap = max(n, 0)
	admitted := m.dispatchLocked()
	m.mu.Unlock()
	m.launchAdmitted(admitted)
}

// SetMaxQueue changes max_queue. Sessions already queued stay queued.
func (m *Manager) SetMaxQueue(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxQueue = max(n, 0)
}

// SetDefaultLimits changes the limits used for challenges that do not set their own. Only
// sessions started afterwards are affected.
func (m *Manager) SetDefaultLimits(l config.Limits) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults = l
}

// SetChallenges replaces the challenge list (challenges.json). Running sessions keep the
// image and limits they started with.
func (m *Manager) SetChallenges(cs []Challenge) {
	byslug := make(map[string]Challenge, len(cs))
	for _, c := range cs {
		byslug[c.Slug] = c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.challenges = byslug
}

// Challenges returns the current challenge list, sorted by slug.
func (m *Manager) Challenges() []Challenge {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := slices.Collect(maps.Values(m.challenges))
	slices.SortFunc(out, func(a, b Challenge) int {
		if a.Slug < b.Slug {
			return -1
		}
		if a.Slug > b.Slug {
			return 1
		}
		return 0
	})
	return out
}

// Resize sets the terminal size of a running session.
func (m *Manager) Resize(ctx context.Context, id string, cols, rows uint32) error {
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil || s.State != StateRunning {
		m.mu.Unlock()
		return ErrNotRunning
	}
	c := s.ctr
	m.mu.Unlock()
	return c.Resize(ctx, cols, rows)
}

// ClientAttached records that a WebSocket took over the session's terminal and cancels the
// reconnect grace timer. It returns the connection's generation for ClientDetached.
func (m *Manager) ClientAttached(id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return 0, ErrNotFound
	}
	s.connGen++
	s.attached = true
	if s.grace != nil {
		s.grace.Stop()
		s.grace = nil
	}
	return s.connGen, nil
}

// ClientDetached records that the WebSocket of generation gen closed. Unless a newer
// connection has taken over, a running session gets ws_reconnect_grace_s to reconnect and
// is then stopped with reason ws_closed (spec state machine: "WS closed 60s").
func (m *Manager) ClientDetached(id string, gen int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil || gen != s.connGen || m.closed {
		return
	}
	s.attached = false
	if s.State != StateRunning {
		return
	}
	if s.grace != nil {
		s.grace.Stop()
	}
	s.grace = m.clk.AfterFunc(m.wsGrace, func() { m.graceExpired(id, gen) })
}

func (m *Manager) graceExpired(id string, gen int) {
	m.mu.Lock()
	s := m.sessions[id]
	if m.closed || s == nil || s.State != StateRunning || s.attached || s.connGen != gen {
		m.mu.Unlock()
		return
	}
	m.beginEndLocked(s, ReasonWSClosed)
	m.mu.Unlock()
	m.persist(s)
	m.launchTeardown(s)
}

// Ended returns a channel closed when the session ends, fails or is abandoned, and a
// function returning its final state (valid once the channel is closed).
func (m *Manager) Ended(id string) (<-chan struct{}, func() SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil, nil, ErrNotFound
	}
	return s.ended, func() SessionInfo {
		m.mu.Lock()
		defer m.mu.Unlock()
		return s.info(0)
	}, nil
}

// AddCommands counts command lines the gateway captured for a session (sessions.commands).
func (m *Manager) AddCommands(id string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		s.commands += n
	}
}

// SetWSGrace changes ws_reconnect_grace_s for disconnects from now on.
func (m *Manager) SetWSGrace(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d > 0 {
		m.wsGrace = d
	}
}

// Close stops admitting sessions and stops every timer. Containers keep running: the next
// labd adopts them (Reconcile). It waits for in-flight creates and teardowns.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, s := range m.sessions {
		stopTimers(s)
	}
	m.mu.Unlock()
	m.ops.Wait()
}

// Settle waits for every create and teardown started so far. Tests and shutdown use it.
func (m *Manager) Settle() { m.ops.Wait() }

// ---------------------------------------------------------------- internals

// admitLocked gives s a slot. The caller launches the create after unlocking.
func (m *Manager) admitLocked(s *Session, now time.Time) {
	m.inUse++
	s.State = StateCreating
	s.AdmittedAt = now
	if s.abandon != nil {
		s.abandon.Stop()
		s.abandon = nil
	}
}

// dispatchLocked admits queued sessions, head first, while slots are free.
func (m *Manager) dispatchLocked() []*Session {
	var admitted []*Session
	now := m.clk.Now()
	for !m.closed && m.inUse < m.cap && len(m.queue) > 0 {
		s := m.queue[0]
		m.queue = m.queue[1:]
		m.admitLocked(s, now)
		admitted = append(admitted, s)
	}
	return admitted
}

func (m *Manager) launchAdmitted(ss []*Session) {
	for _, s := range ss {
		m.persist(s)
		m.launchCreate(s)
	}
}

func (m *Manager) positionLocked(s *Session) int {
	if s.State != StateQueued {
		return 0
	}
	return slices.Index(m.queue, s) + 1
}

// removeQueuedLocked takes s out of the queue as abandoned.
func (m *Manager) removeQueuedLocked(s *Session, reason string) {
	if i := slices.Index(m.queue, s); i >= 0 {
		m.queue = slices.Delete(m.queue, i, i+1)
	}
	stopTimers(s)
	s.State, s.EndReason, s.EndedAt = StateAbandoned, reason, m.clk.Now()
	m.forgetLocked(s)
	close(s.ended)
}

// abandon is the queue timeout.
func (m *Manager) abandon(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil || s.State != StateQueued {
		m.mu.Unlock()
		return
	}
	m.removeQueuedLocked(s, ReasonQueueTimeout)
	waited := s.EndedAt.Sub(s.CreatedAt)
	m.mu.Unlock()
	m.persist(s)
	m.event(s, "lab_ended", map[string]any{"reason": ReasonQueueTimeout, "duration_s": 0, "queued_s": int(waited.Seconds())})
}

func (m *Manager) forgetLocked(s *Session) {
	delete(m.sessions, s.ID)
	if m.byUser[s.UserID] == s {
		delete(m.byUser, s.UserID)
	}
}

// launchCreate and launchTeardown never start work once the Manager is closed: the labs
// keep running for the next labd, and Close's ops.Wait must not race a new Add.
func (m *Manager) launchCreate(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.ops.Add(1)
	go func() {
		defer m.ops.Done()
		m.create(s)
	}()
}

func (m *Manager) launchTeardown(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.ops.Add(1)
	go func() {
		defer m.ops.Done()
		m.teardown(s)
	}()
}

// create builds the spec, creates and starts the container, and moves s to running.
func (m *Manager) create(s *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	m.mu.Lock()
	labels := map[string]string{
		LabelSessionID:   s.ID,
		LabelUserID:      strconv.FormatInt(s.UserID, 10),
		LabelChallenge:   s.ChallengeSlug,
		LabelCreatedAt:   s.AdmittedAt.UTC().Format(time.RFC3339Nano),
		LabelTTLMinutes:  strconv.Itoa(s.Limits.TTLMinutes),
		LabelIdleMinutes: strconv.Itoa(s.Limits.IdleMinutes),
	}
	params := SpecParams{ID: s.ContainerID, Limits: s.Limits, Annotations: labels, Runtime: m.runtimeForSpec()}
	image := s.Image
	m.mu.Unlock()

	cwd, err := m.rt.ImageWorkingDir(ctx, image)
	if err != nil {
		m.fail(s, nil, err)
		return
	}
	params.Cwd = cwd
	spec, err := BuildSpec(m.base, params)
	if err != nil {
		m.fail(s, nil, err)
		return
	}
	c, err := m.rt.Create(ctx, CreateOpts{ID: s.ContainerID, Image: image, Spec: spec, Labels: labels})
	if err != nil {
		m.fail(s, nil, err)
		return
	}
	go m.pump(s, c)
	if err := c.Start(ctx); err != nil {
		m.fail(s, c, err)
		return
	}

	m.mu.Lock()
	s.ctr = c
	now := m.clk.Now()
	s.StartedAt = now
	if s.stopReason != "" {
		// Stopped while being created: go straight to ending.
		m.beginEndLocked(s, s.stopReason)
		m.mu.Unlock()
		m.persist(s)
		m.teardown(s)
		return
	}
	s.State = StateRunning
	s.idleWindow = minutes(s.Limits.IdleMinutes)
	if m.closed {
		// labd is shutting down: leave the lab running, unarmed; the next labd adopts it.
		m.mu.Unlock()
		m.persist(s)
		return
	}
	m.armIdleLocked(s)
	s.hardDeadline = s.AdmittedAt.Add(minutes(s.Limits.TTLMinutes))
	id := s.ID
	s.hard = m.clk.AfterFunc(s.hardDeadline.Sub(now), func() { m.expire(id, ReasonHardTTL) })
	latency := now.Sub(s.AdmittedAt)
	m.mu.Unlock()

	go m.watchExit(s, c)
	m.persist(s)
	m.event(s, "lab_started", map[string]any{"start_latency_ms": latency.Milliseconds()})
	m.log.Info("lab started", "session_id", s.ID, "user_id", s.UserID, "challenge", s.ChallengeSlug,
		"start_latency_ms", latency.Milliseconds())
}

// runtimeForSpec maps the configured shim to the spec builder's process-limit scheme.
func (m *Manager) runtimeForSpec() string {
	if m.rtID == RuntimeRunc {
		return RuntimeRunc
	}
	return RuntimeRunsc
}

// pump copies the terminal's output into the session's scrollback until EOF. The output is
// always drained, so a lab never blocks on a full terminal while nobody is attached.
func (m *Manager) pump(s *Session, c Container) {
	_, stdout := c.IO()
	_, _ = io.Copy(s.out, stdout)
}

// watchExit ends the session when its task exits on its own.
func (m *Manager) watchExit(s *Session, c Container) {
	<-c.Done()
	m.expire(s.ID, ReasonTaskExited)
}

// expire ends a running session for a timer or a task exit. Stale timer callbacks (a Touch
// re-armed the idle timer after it fired) are ignored by checking the deadline.
func (m *Manager) expire(id, reason string) {
	m.mu.Lock()
	s := m.sessions[id]
	if m.closed || s == nil || s.State != StateRunning {
		m.mu.Unlock()
		return
	}
	if reason == ReasonIdle && m.clk.Now().Before(s.idleDeadline) {
		m.mu.Unlock()
		return
	}
	m.beginEndLocked(s, reason)
	m.mu.Unlock()
	m.persist(s)
	m.launchTeardown(s)
}

func (m *Manager) armIdleLocked(s *Session) {
	if m.closed {
		return
	}
	s.idleDeadline = m.clk.Now().Add(s.idleWindow)
	if s.idle == nil {
		id := s.ID
		s.idle = m.clk.AfterFunc(s.idleWindow, func() { m.expire(id, ReasonIdle) })
	} else {
		s.idle.Reset(s.idleWindow)
	}
}

// beginEndLocked moves a running (or just-created) session to ending. Its slot is released
// by teardown once the container is gone. The user may start a new session meanwhile.
func (m *Manager) beginEndLocked(s *Session, reason string) {
	s.State, s.EndReason = StateEnding, reason
	stopTimers(s)
	if m.byUser[s.UserID] == s {
		delete(m.byUser, s.UserID)
	}
}

// teardown kills and deletes the container, then frees the slot and admits the queue head.
func (m *Manager) teardown(s *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	m.mu.Lock()
	c := s.ctr
	m.mu.Unlock()
	if c != nil {
		if err := c.Kill(ctx); err != nil {
			m.log.Warn("kill failed", "session_id", s.ID, "err", err)
		}
		if err := c.Delete(ctx); err != nil {
			// The slot is released anyway: holding it would shrink capacity until a restart.
			// The container is left over (containers > active) until the reconciler removes it.
			m.log.Error("delete failed; the reconciler removes it on the next restart", "session_id", s.ID, "err", err)
		}
	}
	m.mu.Lock()
	s.State, s.EndedAt = StateEnded, m.clk.Now()
	dur := s.EndedAt.Sub(s.StartedAt)
	m.inUse--
	m.forgetLocked(s)
	admitted := m.dispatchLocked()
	reason, commands := s.EndReason, s.commands
	close(s.ended)
	m.mu.Unlock()

	m.persist(s)
	m.event(s, "lab_ended", map[string]any{"reason": reason, "duration_s": int(dur.Seconds()), "commands": commands})
	m.log.Info("lab ended", "session_id", s.ID, "reason", reason, "duration_s", int(dur.Seconds()))
	m.launchAdmitted(admitted)
}

// fail records a create failure, releases the slot and admits the queue head.
func (m *Manager) fail(s *Session, c Container, cause error) {
	if c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		_ = c.Kill(ctx)
		_ = c.Delete(ctx)
		cancel()
	}
	m.mu.Lock()
	s.State, s.EndReason, s.EndedAt = StateFailed, ReasonCreateFailed, m.clk.Now()
	m.inUse--
	m.forgetLocked(s)
	admitted := m.dispatchLocked()
	close(s.ended)
	m.mu.Unlock()

	m.persist(s)
	m.event(s, "lab_ended", map[string]any{"reason": ReasonCreateFailed, "duration_s": 0, "error": cause.Error()})
	m.log.Error("lab failed to start", "session_id", s.ID, "challenge", s.ChallengeSlug, "err", cause)
	m.launchAdmitted(admitted)
}

func stopTimers(s *Session) {
	for _, t := range []clock.Timer{s.idle, s.hard, s.abandon, s.grace} {
		if t != nil {
			t.Stop()
		}
	}
}

// persist writes the session's current row. Writes for one session are serialised and each
// takes a fresh snapshot, so the newest state is always the last one written.
func (m *Manager) persist(s *Session) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	m.mu.Lock()
	rec := s.record()
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.st.UpsertSession(ctx, rec); err != nil {
		m.log.Error("write session row", "session_id", s.ID, "err", err)
	}
}

func (m *Manager) event(s *Session, typ string, data map[string]any) {
	m.mu.Lock()
	ev := store.Event{TS: m.clk.Now(), Type: typ, UserID: s.UserID, SessionID: s.ID,
		ChallengeSlug: s.ChallengeSlug, Data: data}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.st.InsertEvents(ctx, ev); err != nil {
		m.log.Error("write event", "session_id", s.ID, "type", typ, "err", err)
	}
}

// mergeLimits fills zero fields of l from d.
func mergeLimits(l, d config.Limits) config.Limits {
	pick := func(a, b int) int {
		if a > 0 {
			return a
		}
		return b
	}
	return config.Limits{
		MemoryMB: pick(l.MemoryMB, d.MemoryMB), CPUMillicores: pick(l.CPUMillicores, d.CPUMillicores),
		Pids: pick(l.Pids, d.Pids), TTLMinutes: pick(l.TTLMinutes, d.TTLMinutes),
		IdleMinutes: pick(l.IdleMinutes, d.IdleMinutes), ExtendMinutes: pick(l.ExtendMinutes, d.ExtendMinutes),
	}
}

func minutes(n int) time.Duration { return time.Duration(n) * time.Minute }
