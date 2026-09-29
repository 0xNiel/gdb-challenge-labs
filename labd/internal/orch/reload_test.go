package orch

import (
	"context"
	"testing"
)

// TestReload_LowerCapWithholdsSlots is the plan's case: cap 3 with 3 running, lower to 1,
// stop two, and nothing is admitted until the third stops; then admission resumes at cap 1.
func TestReload_LowerCapWithholdsSlots(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 3, 10)
	r := []SessionInfo{h.running(t, 1), h.running(t, 2), h.running(t, 3)}
	q1, q2 := h.start(t, 4), h.start(t, 5)

	h.m.SetCap(1)
	for _, s := range r {
		if got := h.state(t, s.ID); got != StateRunning {
			t.Fatalf("lowering the cap touched a running session: %s", got)
		}
	}
	for _, s := range r[:2] {
		h.m.Stop(context.Background(), s.ID, ReasonAdminKill)
		h.m.Settle()
		if got := h.state(t, q1.ID); got != StateQueued {
			t.Fatalf("admitted over the new cap: queue head is %s", got)
		}
	}
	if st := h.m.Stats(); st.Active != 1 || st.SlotsFree != 0 || st.MaxSessions != 1 {
		t.Fatalf("stats after two stops: %+v", st)
	}
	h.m.Stop(context.Background(), r[2].ID, ReasonAdminKill)
	h.m.Settle()
	if got := h.state(t, q1.ID); got != StateRunning {
		t.Fatalf("queue head is %s once below the cap", got)
	}
	if got := h.state(t, q2.ID); got != StateQueued {
		t.Fatalf("second queued is %s; cap 1 allows one", got)
	}
}

func TestReload_RaiseCapAdmitsAtOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 10)
	h.running(t, 1)
	q1, q2, q3 := h.start(t, 2), h.start(t, 3), h.start(t, 4)
	h.m.SetCap(3)
	h.m.Settle()
	for _, s := range []SessionInfo{q1, q2} {
		if got := h.state(t, s.ID); got != StateRunning {
			t.Fatalf("raising the cap left %s", got)
		}
	}
	if in, _ := h.m.Get(q3.ID); in.State != StateQueued || in.QueuePosition != 1 {
		t.Fatalf("third queued: %+v", in)
	}
}

func TestReload_ChallengesAndDefaults(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 2, 2)
	a := h.running(t, 1)
	h.m.SetChallenges([]Challenge{{Slug: "new", Image: "img2", Enabled: true}})
	lim := h.m.defaults
	lim.MemoryMB = 256
	h.m.SetDefaultLimits(lim)
	if got := h.state(t, a.ID); got != StateRunning {
		t.Fatalf("reload touched a running session: %s", got)
	}
	if _, err := h.m.Start(context.Background(), StartReq{UserID: 2, ChallengeSlug: "perf"}); err == nil {
		t.Fatal("removed challenge still starts")
	}
	b, err := h.m.Start(context.Background(), StartReq{UserID: 2, ChallengeSlug: "new"})
	if err != nil {
		t.Fatal(err)
	}
	h.m.Settle()
	h.m.mu.Lock()
	mem := h.m.sessions[b.ID].Limits.MemoryMB
	h.m.mu.Unlock()
	if mem != 256 {
		t.Fatalf("new session memory %d MiB, want the reloaded default 256", mem)
	}
}
