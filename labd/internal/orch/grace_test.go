package orch

import (
	"context"
	"testing"
	"time"
)

func TestGrace_DetachEndsAfterGrace(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	gen, _ := h.m.ClientAttached(s.ID)
	h.m.ClientDetached(s.ID, gen)
	h.clk.Advance(59 * time.Second)
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("ended inside the grace: %s", got)
	}
	h.clk.Advance(2 * time.Second)
	h.endedWith(t, s.ID, ReasonWSClosed)
}

func TestGrace_ReconnectCancels(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	gen, _ := h.m.ClientAttached(s.ID)
	h.m.ClientDetached(s.ID, gen)
	h.clk.Advance(30 * time.Second)
	if _, err := h.m.ClientAttached(s.ID); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(5 * time.Minute)
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("reconnected session ended: %s", got)
	}
}

// A replaced connection's close must not start the grace for the connection that replaced it.
func TestGrace_StaleDetachIgnored(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	old, _ := h.m.ClientAttached(s.ID)
	if _, err := h.m.ClientAttached(s.ID); err != nil {
		t.Fatal(err)
	}
	h.m.ClientDetached(s.ID, old)
	h.clk.Advance(2 * time.Minute)
	if got := h.state(t, s.ID); got != StateRunning {
		t.Fatalf("stale detach ended the session: %s", got)
	}
	if in, _ := h.m.Get(s.ID); !in.Attached {
		t.Fatal("session not shown as attached")
	}
}

func TestEnded_SignalsWithFinalState(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 1)
	s := h.running(t, 1)
	done, final, err := h.m.Ended(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.m.AddCommands(s.ID, 3)
	h.m.Stop(context.Background(), s.ID, ReasonAdminKill)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Ended channel not closed")
	}
	if in := final(); in.State != StateEnded || in.EndReason != ReasonAdminKill || in.Commands != 3 {
		t.Fatalf("final %+v", in)
	}
	h.m.Settle()
	if row, _ := h.st.Session(s.ID); row.Commands != 3 {
		t.Fatalf("row commands %d", row.Commands)
	}
}

func TestOutput_SubscribeIsGaplessAndBackpressured(t *testing.T) {
	t.Parallel()
	o := newOutput()
	o.Write([]byte("before "))
	snap, ch, cancel := o.Subscribe(1)
	if string(snap) != "before " {
		t.Fatalf("scrollback %q", snap)
	}
	o.Write([]byte("a"))
	blocked := make(chan struct{})
	go func() { o.Write([]byte("b")); close(blocked) }()
	select {
	case <-blocked:
		t.Fatal("write did not wait for a full subscriber")
	case <-time.After(50 * time.Millisecond):
	}
	if got := string(<-ch) + string(<-ch); got != "ab" {
		t.Fatalf("got %q", got)
	}
	<-blocked
	o.Write([]byte("c"))
	cancel()
	done := make(chan struct{})
	go func() { o.Write([]byte("d")); o.Write([]byte("e")); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("write blocked on a cancelled subscriber")
	}
	if string(o.Snapshot()) != "before abcde" {
		t.Fatalf("scrollback %q", o.Snapshot())
	}
}
