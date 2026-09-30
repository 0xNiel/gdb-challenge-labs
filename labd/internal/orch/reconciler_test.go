package orch

import (
	"context"
	"testing"
	"time"

	"gdblabs/labd/internal/store"
)

// labels as an earlier labd would have set them for a container created at `created`.
func labelsFor(sid string, created time.Time) map[string]string {
	return map[string]string{
		LabelSessionID: sid, LabelUserID: "9", LabelChallenge: "perf",
		LabelCreatedAt: created.UTC().Format(time.RFC3339Nano), LabelTTLMinutes: "60", LabelIdleMinutes: "15",
	}
}

func (h *harness) openRow(t *testing.T, sid, state string, user int64, created time.Time) {
	t.Helper()
	row := store.Session{ID: sid, UserID: user, ChallengeSlug: "perf", Image: "img", State: state,
		CreatedAt: created, ContainerID: "lab-" + sid}
	if state == "running" {
		row.StartedAt = created.Add(time.Second)
	}
	if err := h.st.UpsertSession(context.Background(), row); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) reconcile(t *testing.T) ReconcileReport {
	t.Helper()
	rep, err := h.m.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestReconcile_OrphanRemoved(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 5, 5)
	h.rt.addOrphan("lab-orphan", labelsFor("orphan", epoch), true)
	h.rt.addOrphan("p0-runsc-123", nil, true) // unlabelled: a leftover from a script
	rep := h.reconcile(t)
	if rep.Removed != 2 || rep.Adopted != 0 {
		t.Fatalf("report %+v", rep)
	}
	if ids := h.rt.ids(); len(ids) != 0 {
		t.Fatalf("containers left: %v", ids)
	}
	if st := h.m.Stats(); st.Active != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestReconcile_LiveTaskAdopted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 5, 5)
	created := epoch.Add(-20 * time.Minute)
	h.openRow(t, "live", "running", 9, created)
	h.rt.addOrphan("lab-live", labelsFor("live", created), true)
	rep := h.reconcile(t)
	if rep.Adopted != 1 || rep.Removed != 0 || rep.ClosedRows != 0 {
		t.Fatalf("report %+v", rep)
	}
	list := h.m.List()
	if len(list) != 1 || list[0].ID != "live" || list[0].State != StateRunning || list[0].UserID != 9 {
		t.Fatalf("list %+v", list)
	}
	in := list[0]
	if !in.HardDeadline.Equal(created.Add(60 * time.Minute)) {
		t.Fatalf("hard deadline %v, want the original %v", in.HardDeadline, created.Add(60*time.Minute))
	}
	if !in.IdleDeadline.Equal(epoch.Add(15 * time.Minute)) {
		t.Fatalf("idle deadline %v, want now + 15 min", in.IdleDeadline)
	}
	if st := h.m.Stats(); st.Active != 1 || st.Running != 1 {
		t.Fatalf("stats %+v", st)
	}
	// The user gets the adopted session back, and its terminal works.
	if again := h.start(t, 9); again.ID != "live" {
		t.Fatalf("start after restart made %s", again.ID)
	}
	out, _ := h.m.Output("live")
	if err := h.m.WriteInput("live", []byte("still here\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return string(out.Snapshot()) == "still here\n" })
	// The browser reconnects within the grace; timers were rebuilt: the hard TTL fires 40
	// minutes after the restart.
	if _, err := h.m.ClientAttached("live"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		h.clk.Advance(13 * time.Minute)
		h.m.Touch("live")
	}
	h.clk.Advance(time.Minute)
	h.endedWith(t, "live", ReasonHardTTL)
}

func TestReconcile_DeadTaskClosed(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 5, 5)
	h.openRow(t, "dead", "running", 9, epoch.Add(-5*time.Minute))
	h.rt.addOrphan("lab-dead", labelsFor("dead", epoch.Add(-5*time.Minute)), false)
	rep := h.reconcile(t)
	if rep.Removed != 1 || rep.ClosedRows != 1 || rep.Adopted != 0 {
		t.Fatalf("report %+v", rep)
	}
	row, _ := h.st.Session("dead")
	if row.State != "ended" || row.EndReason != ReasonReconciled {
		t.Fatalf("row %+v", row)
	}
	if ids := h.rt.ids(); len(ids) != 0 {
		t.Fatalf("containers left: %v", ids)
	}
}

func TestReconcile_ExpiredKilledDespiteRow(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 5, 5)
	created := epoch.Add(-61 * time.Minute)
	h.openRow(t, "old", "running", 9, created)
	h.rt.addOrphan("lab-old", labelsFor("old", created), true)
	rep := h.reconcile(t)
	if rep.Removed != 1 || rep.Adopted != 0 {
		t.Fatalf("report %+v", rep)
	}
	row, _ := h.st.Session("old")
	if row.State != "ended" || row.EndReason != ReasonReconciled {
		t.Fatalf("row %+v", row)
	}
}

func TestReconcile_RowsWithoutContainers(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 5, 5)
	h.openRow(t, "q", "queued", 1, epoch)
	h.openRow(t, "c", "creating", 2, epoch)
	h.openRow(t, "e", "ending", 3, epoch)
	h.openRow(t, "e2", "ending", 4, epoch)
	h.rt.addOrphan("lab-e2", labelsFor("e2", epoch), true) // was being torn down
	rep := h.reconcile(t)
	if rep.ClosedRows != 4 || rep.Removed != 1 || rep.Adopted != 0 {
		t.Fatalf("report %+v", rep)
	}
	for id, want := range map[string]string{"q": "abandoned", "c": "ended", "e": "ended", "e2": "ended"} {
		row, _ := h.st.Session(id)
		if row.State != want || row.EndReason != ReasonReconciled {
			t.Errorf("%s: %s/%s, want %s/reconciled", id, row.State, row.EndReason, want)
		}
	}
	if open, _ := h.st.OpenSessions(context.Background()); len(open) != 0 {
		t.Fatalf("rows still open: %+v", open)
	}
}

func TestReconcile_AdoptedCountsAgainstCap(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, 5)
	h.openRow(t, "live", "running", 9, epoch)
	h.rt.addOrphan("lab-live", labelsFor("live", epoch), true)
	h.reconcile(t)
	if s := h.start(t, 1); s.State != StateQueued {
		t.Fatalf("new session is %s with the only slot adopted; want queued", s.State)
	}
}

func TestReconcile_AdoptedWithoutReconnectEndsAfterGrace(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 5, 5)
	h.openRow(t, "live", "running", 9, epoch)
	h.rt.addOrphan("lab-live", labelsFor("live", epoch), true)
	h.reconcile(t)
	h.clk.Advance(59 * time.Second)
	if got := h.state(t, "live"); got != StateRunning {
		t.Fatalf("ended inside the grace: %s", got)
	}
	h.clk.Advance(time.Second)
	h.endedWith(t, "live", ReasonWSClosed)
}
