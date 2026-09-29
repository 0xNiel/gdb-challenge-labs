package orch

import (
	"context"
	"errors"
	"testing"
	"time"
)

// running starts a session for user and waits until it runs.
func (h *harness) running(t *testing.T, user int64) SessionInfo {
	t.Helper()
	s := h.start(t, user)
	h.m.Settle()
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("session is %s, want running", got)
	}
	return s
}

func (h *harness) endedWith(t *testing.T, id, reason string) {
	t.Helper()
	h.m.Settle()
	row, _ := h.st.Session(id)
	if row.State != string(StateEnded) || row.EndReason != reason {
		t.Fatalf("row state %s reason %q, want ended/%s", row.State, row.EndReason, reason)
	}
	if len(h.rt.ids()) != 0 {
		t.Fatalf("containers left: %v", h.rt.ids())
	}
}

func TestTimers_IdleFires(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	h.clk.Advance(15*time.Minute - time.Second)
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("ended early: %s", got)
	}
	h.clk.Advance(time.Second)
	h.endedWith(t, s.ID, ReasonIdle)
}

func TestTimers_TouchResetsIdle(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	for range 4 { // 40 minutes of activity every 10 minutes; 40 + 15 stays under the 60 min TTL
		h.clk.Advance(10 * time.Minute)
		h.m.Touch(s.ID)
	}
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("active session ended: %s", got)
	}
	h.clk.Advance(15 * time.Minute)
	h.endedWith(t, s.ID, ReasonIdle)
}

func TestTimers_HardTTLFires(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	for range 5 { // keep it active: only the hard TTL can end it
		h.clk.Advance(11 * time.Minute)
		h.m.Touch(s.ID)
	}
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("state %s at 55 min", got)
	}
	h.clk.Advance(5 * time.Minute)
	h.endedWith(t, s.ID, ReasonHardTTL)
}

func TestTimers_ExtendOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	h.clk.Advance(14 * time.Minute)
	in, err := h.m.Extend(s.ID)
	if err != nil || !in.Extended {
		t.Fatalf("extend: %+v, %v", in, err)
	}
	if want := h.clk.Now().Add(30 * time.Minute); !in.IdleDeadline.Equal(want) {
		t.Fatalf("idle deadline %v, want %v (15 + 15 min)", in.IdleDeadline, want)
	}
	if _, err := h.m.Extend(s.ID); !errors.Is(err, ErrAlreadyExtended) {
		t.Fatalf("second extend: %v, want ErrAlreadyExtended", err)
	}
	h.clk.Advance(29 * time.Minute)
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("extended session ended early: %s", got)
	}
	row, _ := h.st.Session(s.ID)
	if !row.Extended {
		t.Fatal("row does not record the extension")
	}
	h.clk.Advance(time.Minute)
	h.endedWith(t, s.ID, ReasonIdle)
}

func TestTimers_ExtendNotRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	h.start(t, 1)
	q := h.start(t, 2)
	if _, err := h.m.Extend(q.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("extend queued: %v", err)
	}
	if _, err := h.m.Extend("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("extend unknown: %v", err)
	}
}

func TestTimers_TouchAfterEndingIgnored(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	gate := make(chan struct{})
	c := h.rt.get("lab-" + s.ID)
	// Hold the teardown in Kill so the session stays in ending while we touch it.
	blocking := &blockingKill{fakeContainer: c, gate: gate}
	h.m.mu.Lock()
	h.m.sessions[s.ID].ctr = blocking
	h.m.mu.Unlock()

	if _, err := h.m.Stop(context.Background(), s.ID, ReasonUserStop); err != nil {
		t.Fatal(err)
	}
	h.m.Touch(s.ID)
	if _, err := h.m.Extend(s.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("extend while ending: %v", err)
	}
	if in, _ := h.m.Get(s.ID); in.State != StateEnding || !in.IdleDeadline.Equal(epoch.Add(15*time.Minute)) {
		t.Fatalf("touch changed an ending session: %+v", in)
	}
	if h.clk.Pending() != 0 {
		t.Fatalf("%d timers still armed on an ending session", h.clk.Pending())
	}
	close(gate)
	h.endedWith(t, s.ID, ReasonUserStop)
}

type blockingKill struct {
	*fakeContainer
	gate chan struct{}
}

func (b *blockingKill) Kill(ctx context.Context) error {
	<-b.gate
	return b.fakeContainer.Kill(ctx)
}
