//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/metrics"
	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/store"
	"gdblabs/labd/internal/term"
	"gdblabs/labd/internal/term/client"
)

// TestMetrics_SamplesFromTwoLabs is task 7.1: with 2 sessions running and typing, the sampler
// writes a row for every metric name (spec "Sample schema" plus session.sentry_rss_mb) into
// Postgres, and the peak memory reaches the session row.
func TestMetrics_SamplesFromTwoLabs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.OpenPostgres(ctx, freshSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rt := newRuntime(t)
	m := orch.NewManager(orch.ManagerConfig{
		BaseSpec: baseSpec(t), Runtime: runtimeID, MaxSessions: 2, MaxQueue: 2,
		DefaultLimits: config.Default().DefaultLimits,
		Challenges:    []orch.Challenge{{Slug: "perf", Image: image, Enabled: true}},
	}, rt, db, clock.Real{}, log)
	defer m.Close()
	tokens := term.NewTokens([]byte("integration"))
	gw := term.New(term.Options{Sessions: m, Tokens: tokens, Log: log, AllowNoOrigin: true})
	ts := httptest.NewServer(gw.Handler())
	defer ts.Close()

	var ids []string
	for uid := int64(1); uid <= 2; uid++ {
		in, err := m.Start(ctx, orch.StartReq{UserID: uid, ChallengeSlug: "perf"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, in.ID)
		t.Cleanup(func() { _ = rt.Remove(context.Background(), "lab-"+in.ID) })
		c, err := client.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/term/"+in.ID,
			tokens.Mint(in.ID, uid, time.Now()), client.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if st, err := c.WaitControl("state", 60*time.Second); err != nil || st["state"] != "running" {
			t.Fatalf("state %v, %v", st, err)
		}
		if err := c.Send("echo metrics-$((6*7))"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Expect(regexp.MustCompile(`metrics-42`), 30*time.Second); err != nil {
			t.Fatal(err)
		}
	}

	s := &metrics.Sampler{
		Root: "/", CgroupDir: "sys/fs/cgroup" + orch.CgroupParent, DiskPath: "/var/lib/containerd",
		Src: m, WSBytes: gw.Bytes, Sink: db, OnRSS: m.ObserveRSS, Log: log,
	}
	sctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.Run(sctx, 2*time.Second); close(done) }()
	time.Sleep(13 * time.Second)
	stop()
	<-done

	want := []string{
		metrics.HostMemUsedMB, metrics.HostCPUPct, metrics.HostDiskUsedGB, metrics.HostCPUPressure,
		metrics.HostMemPressure, metrics.LabdActive, metrics.LabdQueued, metrics.SessionRSSMB,
		metrics.SessionSentryMB, metrics.SessionCPUMS, metrics.WSBytesIn, metrics.WSBytesOut,
	}
	for _, name := range want {
		if name == metrics.SessionSentryMB && runtimeID != orch.RuntimeRunsc {
			continue // no Sentry under runc
		}
		n, err := db.QueryInt(ctx, `SELECT count(*) FROM samples WHERE metric = $1`, name)
		if err != nil || n == 0 {
			t.Errorf("no %s samples (%d, %v)", name, n, err)
		}
	}
	n, err := db.QueryInt(ctx, `SELECT count(DISTINCT metric) FROM samples`)
	if err != nil || n < 10 {
		t.Errorf("%d distinct metrics (%v), the gate wants at least 10", n, err)
	}
	for _, id := range ids {
		n, err := db.QueryInt(ctx, `SELECT count(DISTINCT metric) FROM samples WHERE session_id = $1`, id)
		if err != nil || n < 4 {
			t.Errorf("session %s: %d per-session metrics (%v)", id, n, err)
		}
		if _, err := m.Stop(ctx, id, orch.ReasonUserStop); err != nil {
			t.Fatal(err)
		}
	}
	m.Settle()
	for _, id := range ids {
		if row, err := db.Session(ctx, id); err != nil || row.PeakRSSMB <= 0 {
			t.Errorf("session %s: peak_rss_mb %v (%v)", id, row.PeakRSSMB, err)
		}
	}
}
