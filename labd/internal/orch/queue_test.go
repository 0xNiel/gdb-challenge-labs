package orch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueue_PositionsShiftWhenHeadAdmitted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	run := h.start(t, 1)
	q := []SessionInfo{h.start(t, 2), h.start(t, 3), h.start(t, 4)}
	h.m.Settle()
	for i, s := range q {
		if s.QueuePosition != i+1 {
			t.Fatalf("user %d at %d, want %d", s.UserID, s.QueuePosition, i+1)
		}
	}
	h.m.Stop(context.Background(), run.ID, ReasonUserStop)
	h.m.Settle()
	for i, s := range q[1:] {
		in, _ := h.m.Get(s.ID)
		if in.QueuePosition != i+1 {
			t.Fatalf("user %d at %d after the head left, want %d", s.UserID, in.QueuePosition, i+1)
		}
	}
	// A repeated start by a queued user reports the current position.
	if again := h.start(t, 4); again.ID != q[2].ID || again.QueuePosition != 2 {
		t.Fatalf("repeat start: %+v", again)
	}
}

func TestQueue_AbandonedAfterTimeout(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	h.start(t, 1)
	early := h.start(t, 2)
	h.clk.Advance(time.Minute)
	late := h.start(t, 3)
	h.m.Settle()

	h.clk.Advance(time.Minute) // early has waited 2 min, late 1 min
	if got := h.state(t, early.ID); got != StateAbandoned {
		t.Fatalf("early is %s, want abandoned", got)
	}
	row, _ := h.st.Session(early.ID)
	if row.EndReason != ReasonQueueTimeout {
		t.Fatalf("reason %q", row.EndReason)
	}
	if in, _ := h.m.Get(late.ID); in.State != StateQueued || in.QueuePosition != 1 {
		t.Fatalf("late: %+v", in)
	}
	h.clk.Advance(time.Minute)
	if got := h.state(t, late.ID); got != StateAbandoned {
		t.Fatalf("late is %s after 2 min, want abandoned", got)
	}
	if st := h.m.Stats(); st.Queued != 0 || st.Running != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestQueue_AdmittedSessionIsNotAbandoned(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	first := h.start(t, 1)
	q := h.start(t, 2)
	h.m.Settle()
	h.m.Stop(context.Background(), first.ID, ReasonUserStop)
	h.m.Settle()
	h.clk.Advance(5 * time.Minute)
	if got := h.state(t, q.ID); got != StateRunning {
		t.Fatalf("admitted session is %s after the queue timeout passed", got)
	}
}

func TestQueue_FullReturnsErrQueueFull(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 2)
	h.start(t, 1)
	h.start(t, 2)
	h.start(t, 3)
	_, err := h.m.Start(context.Background(), StartReq{UserID: 4, ChallengeSlug: "perf"})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err %v, want ErrQueueFull", err)
	}
	// No queue at all: at the cap, a start is refused at once.
	h0 := newHarness(t, 1, 0)
	h0.start(t, 1)
	if _, err := h0.m.Start(context.Background(), StartReq{UserID: 2, ChallengeSlug: "perf"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("max_queue 0: err %v", err)
	}
}

// TestQueue_150Into100 mirrors P6: 150 requests against cap 100 and queue 50.
func TestQueue_150Into100(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 100, 50)
	var admitted, queued []SessionInfo
	for u := int64(1); u <= 150; u++ {
		in := h.start(t, u)
		if in.State == StateQueued {
			queued = append(queued, in)
		} else {
			admitted = append(admitted, in)
		}
	}
	if _, err := h.m.Start(context.Background(), StartReq{UserID: 151, ChallengeSlug: "perf"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("151st: %v, want ErrQueueFull", err)
	}
	h.m.Settle()
	if len(admitted) != 100 || len(queued) != 50 {
		t.Fatalf("admitted %d, queued %d", len(admitted), len(queued))
	}
	for i, q := range queued {
		if q.QueuePosition != i+1 {
			t.Fatalf("queued #%d reported position %d", i+1, q.QueuePosition)
		}
	}
	if n := len(h.rt.ids()); n != 100 {
		t.Fatalf("%d containers over a cap of 100", n)
	}

	// Drain: each stop admits exactly the queue head, in request order.
	for i, a := range admitted[:50] {
		h.m.Stop(context.Background(), a.ID, ReasonUserStop)
		h.m.Settle()
		if got := h.state(t, queued[i].ID); got != StateRunning {
			t.Fatalf("after %d stops, queued #%d is %s", i+1, i+1, got)
		}
		if i+1 < len(queued) {
			if in, _ := h.m.Get(queued[i+1].ID); in.State != StateQueued || in.QueuePosition != 1 {
				t.Fatalf("after %d stops, next in queue: %+v", i+1, in)
			}
		}
		if st := h.m.Stats(); st.Active > 100 || len(h.rt.ids()) > 100 {
			t.Fatalf("over cap: %+v", st)
		}
	}
	if st := h.m.Stats(); st.Queued != 0 || st.Running != 100 {
		t.Fatalf("after drain: %+v", st)
	}
}
