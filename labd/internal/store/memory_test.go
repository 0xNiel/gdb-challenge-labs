package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemory_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := NewMemory()
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	rows := []Session{
		{ID: "b", UserID: 2, State: "running", CreatedAt: t0.Add(time.Second)},
		{ID: "a", UserID: 1, State: "queued", CreatedAt: t0},
		{ID: "c", UserID: 3, State: "ended", CreatedAt: t0, EndReason: "user_stop"},
	}
	for _, r := range rows {
		if err := m.UpsertSession(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	upd := rows[0]
	upd.State, upd.EndReason = "ended", "idle_timeout"
	if err := m.UpsertSession(ctx, upd); err != nil {
		t.Fatal(err)
	}
	open, err := m.OpenSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != "a" {
		t.Fatalf("open %+v, want only a", open)
	}
	if got, _ := m.Session("b"); got.EndReason != "idle_timeout" {
		t.Fatalf("update lost: %+v", got)
	}

	data := map[string]any{"reason": "user_stop"}
	if err := m.InsertEvents(ctx, Event{Type: "lab_ended", SessionID: "c", Data: data}); err != nil {
		t.Fatal(err)
	}
	data["reason"] = "mutated"
	if evs := m.Events(); len(evs) != 1 || evs[0].Data["reason"] != "user_stop" {
		t.Fatalf("events %+v", evs)
	}

	m.FailWrites(errors.New("down"))
	if err := m.UpsertSession(ctx, rows[1]); err == nil {
		t.Fatal("FailWrites ignored")
	}
}
