package orch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/store"
)

var epoch = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

type harness struct {
	m   *Manager
	rt  *fakeRuntime
	st  *store.Memory
	clk *clock.Fake
}

func newHarness(t *testing.T, maxSessions, maxQueue int) *harness {
	t.Helper()
	h := &harness{rt: newFakeRuntime(), st: store.NewMemory(), clk: clock.NewFake(epoch)}
	h.m = NewManager(ManagerConfig{
		BaseSpec: loadBase(t), MaxSessions: maxSessions, MaxQueue: maxQueue,
		DefaultLimits: config.Default().DefaultLimits,
		Challenges: []Challenge{
			{Slug: "perf", Image: "docker.io/gdblabs/perf:dev", Enabled: true},
			{Slug: "broken", Image: "missing", Enabled: true},
			{Slug: "off", Image: "x", Enabled: false},
		},
	}, h.rt, h.st, h.clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(h.m.Close)
	return h
}

func (h *harness) start(t *testing.T, user int64) SessionInfo {
	t.Helper()
	in, err := h.m.Start(context.Background(), StartReq{UserID: user, ChallengeSlug: "perf"})
	if err != nil {
		t.Fatalf("start user %d: %v", user, err)
	}
	return in
}

func (h *harness) state(t *testing.T, id string) State {
	t.Helper()
	if in, err := h.m.Get(id); err == nil {
		return in.State
	}
	row, ok := h.st.Session(id)
	if !ok {
		t.Fatalf("session %s unknown to manager and store", id)
	}
	return State(row.State)
}

func TestManager_CapAdmitsTwoQueuesThird(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 2, 10)
	a, b, c := h.start(t, 1), h.start(t, 2), h.start(t, 3)
	h.m.Settle()
	if a.State != StateCreating || b.State != StateCreating {
		t.Fatalf("first two: %s, %s; want creating", a.State, b.State)
	}
	if c.State != StateQueued || c.QueuePosition != 1 {
		t.Fatalf("third: %s at %d; want queued at 1", c.State, c.QueuePosition)
	}
	if h.state(t, a.ID) != StateRunning || h.state(t, b.ID) != StateRunning {
		t.Fatal("admitted sessions did not reach running")
	}
	if got := len(h.rt.ids()); got != 2 {
		t.Fatalf("%d containers, want 2", got)
	}
	st := h.m.Stats()
	if st.Active != 2 || st.Running != 2 || st.Queued != 1 || st.SlotsFree != 0 {
		t.Fatalf("stats %+v", st)
	}
}

// The session manager starts the lab in its image's WORKDIR (ADR 0014).
func TestManager_CwdFromImage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ workdir, want string }{
		{"/opt/lab", "/opt/lab"},
		{"", "/home/lab"},
	} {
		h := newHarness(t, 1, 1)
		if tc.workdir != "" {
			h.rt.workdirs["docker.io/gdblabs/perf:dev"] = tc.workdir
		}
		in := h.start(t, 1)
		h.m.Settle()
		if got := h.state(t, in.ID); got != StateRunning {
			t.Fatalf("WORKDIR %q: session %s", tc.workdir, got)
		}
		ids := h.rt.ids()
		if len(ids) != 1 {
			t.Fatalf("%d containers", len(ids))
		}
		if got := h.rt.spec(ids[0]).Process.Cwd; got != tc.want {
			t.Errorf("WORKDIR %q: cwd %q, want %q", tc.workdir, got, tc.want)
		}
	}
}

func TestManager_SameUserGetsSameSession(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 2, 10)
	a := h.start(t, 7)
	h.m.Settle()
	b := h.start(t, 7)
	if a.ID != b.ID {
		t.Fatalf("second start made a new session: %s != %s", a.ID, b.ID)
	}
	if b.State != StateRunning {
		t.Fatalf("second start state %s, want running", b.State)
	}
	if got := len(h.rt.ids()); got != 1 {
		t.Fatalf("%d containers, want 1", got)
	}
}

func TestManager_StopAdmitsQueuedInFIFOOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 10)
	first := h.start(t, 1)
	q1, q2 := h.start(t, 2), h.start(t, 3)
	h.m.Settle()
	if q1.QueuePosition != 1 || q2.QueuePosition != 2 {
		t.Fatalf("positions %d, %d", q1.QueuePosition, q2.QueuePosition)
	}

	if _, err := h.m.Stop(context.Background(), first.ID, ReasonUserStop); err != nil {
		t.Fatal(err)
	}
	h.m.Settle()
	if got := h.state(t, first.ID); got != StateEnded {
		t.Fatalf("stopped session is %s", got)
	}
	if got := h.state(t, q1.ID); got != StateRunning {
		t.Fatalf("queue head is %s, want running", got)
	}
	in, _ := h.m.Get(q2.ID)
	if in.State != StateQueued || in.QueuePosition != 1 {
		t.Fatalf("second in queue: %s at %d; want queued at 1", in.State, in.QueuePosition)
	}
	if ids := h.rt.ids(); len(ids) != 1 || ids[0] != "lab-"+q1.ID {
		t.Fatalf("containers %v, want only the queue head's", ids)
	}
	row, _ := h.st.Session(first.ID)
	if row.State != "ended" || row.EndReason != ReasonUserStop || row.EndedAt.IsZero() {
		t.Fatalf("row %+v", row)
	}
}

func TestManager_StopUnknown(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	if _, err := h.m.Stop(context.Background(), "nope", ReasonUserStop); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v, want ErrNotFound", err)
	}
}

func TestManager_CreateFailureReleasesSlot(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	h.rt.FailCreateFor("broken")
	bad, err := h.m.Start(context.Background(), StartReq{UserID: 1, ChallengeSlug: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	next := h.start(t, 2) // queued behind the failing one
	h.m.Settle()
	if got := h.state(t, bad.ID); got != StateFailed {
		t.Fatalf("failing session is %s, want failed", got)
	}
	row, _ := h.st.Session(bad.ID)
	if row.EndReason != ReasonCreateFailed {
		t.Fatalf("end reason %q", row.EndReason)
	}
	if got := h.state(t, next.ID); got != StateRunning {
		t.Fatalf("queued session is %s after the failure; want running", got)
	}
	if st := h.m.Stats(); st.Active != 1 {
		t.Fatalf("active %d, want 1", st.Active)
	}
	// The user whose lab failed can try again.
	again, err := h.m.Start(context.Background(), StartReq{UserID: 1, ChallengeSlug: "perf"})
	if err != nil || again.ID == bad.ID {
		t.Fatalf("retry after failure: %+v, %v", again, err)
	}
}

func TestManager_UnknownOrDisabledChallenge(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	for _, slug := range []string{"nope", "off"} {
		if _, err := h.m.Start(context.Background(), StartReq{UserID: 1, ChallengeSlug: slug}); !errors.Is(err, ErrUnknownChallenge) {
			t.Fatalf("%s: err %v", slug, err)
		}
	}
}

func TestManager_StopWhileCreating(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	gate := make(chan struct{})
	h.rt.gate = gate
	s := h.start(t, 1)
	in, err := h.m.Stop(context.Background(), s.ID, ReasonUserStop)
	if err != nil || in.State != StateCreating {
		t.Fatalf("stop during create: %+v, %v", in, err)
	}
	close(gate)
	h.m.Settle()
	if got := h.state(t, s.ID); got != StateEnded {
		t.Fatalf("state %s, want ended", got)
	}
	if ids := h.rt.ids(); len(ids) != 0 {
		t.Fatalf("containers left: %v", ids)
	}
	if st := h.m.Stats(); st.Active != 0 {
		t.Fatalf("active %d", st.Active)
	}
}

func TestManager_TaskExitEndsSession(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.start(t, 1)
	h.m.Settle()
	h.rt.get("lab-" + s.ID).stop()
	waitFor(t, func() bool { return h.m.Stats().Active == 0 })
	row, _ := h.st.Session(s.ID)
	if row.State != "ended" || row.EndReason != ReasonTaskExited {
		t.Fatalf("row %+v", row)
	}
}

func TestManager_TerminalIO(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.start(t, 1)
	h.m.Settle()
	out, err := h.m.Output(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.WriteInput(s.ID, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return string(out.Snapshot()) == "hello\n" })
}

func TestManager_EventsAndLabels(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.start(t, 42)
	h.m.Settle()
	c := h.rt.get("lab-" + s.ID)
	for k, want := range map[string]string{
		LabelSessionID: s.ID, LabelUserID: "42", LabelChallenge: "perf",
		LabelCreatedAt: epoch.Format(time.RFC3339Nano), LabelTTLMinutes: "60", LabelIdleMinutes: "15",
	} {
		if c.labels[k] != want {
			t.Errorf("label %s = %q, want %q", k, c.labels[k], want)
		}
	}
	var types []string
	for _, e := range h.st.Events() {
		types = append(types, e.Type)
	}
	if len(types) != 2 || types[0] != "lab_requested" || types[1] != "lab_started" {
		t.Fatalf("events %v", types)
	}
}

// waitFor polls cond for up to 2 s (for effects of real goroutines, not timers).
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManager_StopWhileCreatingFreesTheUser(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	gate := make(chan struct{})
	h.rt.gate = gate
	old := h.start(t, 1)
	if _, err := h.m.Stop(context.Background(), old.ID, ReasonUserStop); err != nil {
		t.Fatal(err)
	}
	fresh := h.start(t, 1)
	if fresh.ID == old.ID {
		t.Fatal("start after stopping a creating lab returned the doomed session")
	}
	close(gate)
	h.m.Settle()
	if got := h.state(t, fresh.ID); got != StateRunning {
		t.Fatalf("fresh session is %s, want running once the old slot freed", got)
	}
}

// After Close (labd shutting down) nothing may end or start a lab: they must survive for
// the next labd to adopt.
func TestManager_CloseLeavesLabsAlone(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 2, 5)
	a, b := h.running(t, 1), h.running(t, 2)
	h.m.Close()
	h.clk.Advance(2 * time.Hour)      // idle and hard timers would have fired
	h.rt.get("lab-" + b.ID).stop()    // a task exit (or a failed Wait) after Close
	h.m.Touch(a.ID)                   // must not re-arm anything
	time.Sleep(50 * time.Millisecond) // give a wrongly launched teardown time to run
	if h.clk.Pending() != 0 {
		t.Fatalf("%d timers armed after Close", h.clk.Pending())
	}
	if h.rt.get("lab-"+a.ID) == nil {
		t.Fatal("a running lab was torn down after Close")
	}
	for _, id := range []string{a.ID, b.ID} {
		if row, _ := h.st.Session(id); row.State != "running" {
			t.Fatalf("row %s is %s after Close; the next labd would not adopt it", id, row.State)
		}
	}
	if _, err := h.m.Start(context.Background(), StartReq{UserID: 3, ChallengeSlug: "perf"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after Close: %v", err)
	}
}

// The metrics sampler's view (Phase 7): running sessions, the counts, and the peak memory
// written to the session's row.
func TestManager_SamplerHooks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	a := h.start(t, 1)
	q := h.start(t, 2)
	h.m.Settle()
	if got := h.m.Running(); len(got) != 1 || got[0] != a.ID {
		t.Fatalf("running %v, want [%s]", got, a.ID)
	}
	if active, queued := h.m.Counts(); active != 1 || queued != 1 {
		t.Fatalf("counts %d active, %d queued", active, queued)
	}
	h.m.ObserveRSS(a.ID, 25)
	h.m.ObserveRSS(a.ID, 31.5)
	h.m.ObserveRSS(a.ID, 28) // lower: the peak stays
	h.m.ObserveRSS("no-such-session", 99)
	if _, err := h.m.Stop(context.Background(), a.ID, ReasonUserStop); err != nil {
		t.Fatal(err)
	}
	h.m.Settle()
	row, ok := h.st.Session(a.ID)
	if !ok || row.PeakRSSMB != 31.5 || row.State != "ended" {
		t.Fatalf("row %+v", row)
	}
	_ = q
}

// Task 7.2: each lifecycle writes its events with the spec's data keys; a queued session
// adds lab_queued.
func TestManager_EventLifecycle(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	a := h.start(t, 1)
	b := h.start(t, 2) // queued behind a
	h.m.Settle()
	h.m.AddCommands(a.ID, 3)
	h.clk.Advance(90 * time.Second) // under the 2 min queue timeout: b stays queued
	for _, id := range []string{a.ID, b.ID} {
		h.m.Settle()
		if _, err := h.m.Stop(context.Background(), id, ReasonUserStop); err != nil {
			t.Fatal(err)
		}
		h.m.Settle()
	}
	byUser := map[int64][]store.Event{}
	for _, e := range h.st.Events() {
		byUser[e.UserID] = append(byUser[e.UserID], e)
	}
	types := func(evs []store.Event) []string {
		var out []string
		for _, e := range evs {
			out = append(out, e.Type)
		}
		return out
	}
	if got := strings.Join(types(byUser[1]), ","); got != "lab_requested,lab_started,lab_ended" {
		t.Fatalf("user 1 events %s", got)
	}
	if got := strings.Join(types(byUser[2]), ","); got != "lab_requested,lab_queued,lab_started,lab_ended" {
		t.Fatalf("user 2 events %s", got)
	}
	for _, e := range append(byUser[1], byUser[2]...) {
		if e.SessionID == "" || e.ChallengeSlug != "perf" {
			t.Errorf("%s lacks session or challenge: %+v", e.Type, e)
		}
		need := map[string][]string{
			"lab_queued":  {"position"},
			"lab_started": {"start_latency_ms"},
			"lab_ended":   {"reason", "duration_s", "commands"},
		}[e.Type]
		for _, k := range need {
			if _, ok := e.Data[k]; !ok {
				t.Errorf("%s lacks data key %q: %v", e.Type, k, e.Data)
			}
		}
	}
	end := byUser[1][2].Data
	if end["reason"] != ReasonUserStop || end["commands"] != 3 || end["duration_s"] != 90 {
		t.Errorf("user 1 lab_ended data %v", end)
	}
	if byUser[2][1].Data["position"] != 1 {
		t.Errorf("lab_queued data %v", byUser[2][1].Data)
	}
}

// ADR 0018: draining admits nothing and leaves running labs alone; a config reload (SetCap)
// while draining keeps the drain; resuming admits the queue.
func TestManager_Drain(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 2, 5)
	a := h.start(t, 1)
	h.m.Settle()
	h.m.SetDrain(true)
	if st := h.m.Stats(); !st.Draining || st.MaxSessions != 0 || st.ConfiguredMaxSessions != 2 {
		t.Fatalf("draining stats %+v", st)
	}
	b := h.start(t, 2)
	h.m.Settle()
	if b.State != StateQueued || h.state(t, a.ID) != StateRunning {
		t.Fatalf("while draining: new %s, running one %s", b.State, h.state(t, a.ID))
	}
	h.m.SetCap(3) // a reload while draining
	if st := h.m.Stats(); !st.Draining || st.MaxSessions != 0 || st.ConfiguredMaxSessions != 3 {
		t.Fatalf("reload ended the drain: %+v", st)
	}
	h.m.SetDrain(false)
	h.m.Settle()
	if st := h.m.Stats(); st.Draining || st.MaxSessions != 3 {
		t.Fatalf("resumed stats %+v", st)
	}
	if h.state(t, b.ID) != StateRunning {
		t.Fatalf("queued session is %s after resume, want running", h.state(t, b.ID))
	}
}
