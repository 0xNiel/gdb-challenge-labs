//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"gdblabs/labd/internal/store"
)

var postgresDSN = env("LABD_TEST_POSTGRES_DSN", "postgres://labd:labd@127.0.0.1:5432/labs")

// freshSchema returns a DSN whose search_path is a new, empty schema, dropped at cleanup.
func freshSchema(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	admin, err := store.OpenPostgres(ctx, postgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("it_%d", time.Now().UnixNano())
	if err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	u, err := url.Parse(postgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestStore_MigrateTwiceAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	p, err := store.OpenPostgres(ctx, freshSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	applied, err := p.Migrate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 || applied[0] != "0001_init" {
		t.Fatalf("first migrate applied %v", applied)
	}
	again, err := p.Migrate(ctx)
	if err != nil || len(again) != 0 {
		t.Fatalf("second migrate: applied %v, err %v", again, err)
	}

	t0 := time.Now().UTC().Truncate(time.Millisecond)
	s := store.Session{ID: "0b6a3f0e-1c1e-4a55-9a51-6d2b8b1c0001", UserID: 7, ChallengeSlug: "perf",
		Image: "docker.io/gdblabs/perf:dev", State: "creating", CreatedAt: t0}
	if err := p.UpsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.State, s.StartedAt, s.ContainerID = "running", t0.Add(time.Second), "lab-x"
	if err := p.UpsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	open, err := p.OpenSessions(ctx)
	if err != nil || len(open) != 1 || open[0].State != "running" || !open[0].StartedAt.Equal(s.StartedAt) {
		t.Fatalf("open sessions %+v, err %v", open, err)
	}
	s.State, s.EndedAt, s.EndReason, s.Extended = "ended", t0.Add(time.Minute), "user_stop", true
	if err := p.UpsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	if open, _ := p.OpenSessions(ctx); len(open) != 0 {
		t.Fatalf("ended session still open: %+v", open)
	}
	got, err := p.Session(ctx, s.ID)
	if err != nil || got.EndReason != "user_stop" || !got.Extended || got.UserID != 7 {
		t.Fatalf("row %+v, err %v", got, err)
	}

	bad := s
	bad.ID, bad.State = "0b6a3f0e-1c1e-4a55-9a51-6d2b8b1c0002", "sleeping"
	if err := p.UpsertSession(ctx, bad); err == nil {
		t.Fatal("state CHECK constraint accepted an unknown state")
	}

	if err := p.InsertEvents(ctx,
		store.Event{TS: t0, Type: "lab_requested", UserID: 7, SessionID: s.ID, ChallengeSlug: "perf"},
		store.Event{TS: t0, Type: "lab_ended", SessionID: s.ID, Data: map[string]any{"reason": "user_stop", "duration_s": 60}},
	); err != nil {
		t.Fatal(err)
	}
}
