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
	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/store"
	"gdblabs/labd/internal/term"
	"gdblabs/labd/internal/term/client"
)

// TestTerm_RealGdbOverWebSocket is task 3.9: labd's manager and gateway in-process against
// the real containerd, gdb driven through the WebSocket.
func TestTerm_RealGdbOverWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := newRuntime(t)
	st := store.NewMemory()
	m := orch.NewManager(orch.ManagerConfig{
		BaseSpec: baseSpec(t), Runtime: runtimeID, MaxSessions: 2, MaxQueue: 2,
		DefaultLimits: config.Default().DefaultLimits,
		Challenges:    []orch.Challenge{{Slug: "perf", Image: image, Enabled: true}},
	}, rt, st, clock.Real{}, log)
	defer m.Close()
	tokens := term.NewTokens([]byte("integration"))
	rec := term.NewRecorder(st, clock.Real{}, log)
	defer rec.Close()
	gw := term.New(term.Options{Sessions: m, Tokens: tokens, Recorder: rec, Log: log, AllowNoOrigin: true})
	ts := httptest.NewServer(gw.Handler())
	defer ts.Close()

	in, err := m.Start(ctx, orch.StartReq{UserID: 1, ChallengeSlug: "perf"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Remove(context.Background(), "lab-"+in.ID) })
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/term/" + in.ID
	c, err := client.Dial(ctx, url, tokens.Mint(in.ID, 1, time.Now()), client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if st, err := c.WaitControl("state", 60*time.Second); err != nil || st["state"] != "running" {
		t.Fatalf("state %v, %v", st, err)
	}
	if err := c.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	step := func(send, want string, timeout time.Duration) {
		t.Helper()
		if err := c.Send(send); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Expect(regexp.MustCompile(want), timeout); err != nil {
			t.Fatalf("after %q: %v", send, err)
		}
	}
	step("gdb -q /opt/perf/perf", `\(gdb\) `, 60*time.Second)
	step("break main", `Breakpoint 1 at`, 30*time.Second)
	// gdb colours its output (TERM=xterm-256color): allow SGR codes around names.
	step("run", `Breakpoint 1, (?:\x1b\[[0-9;]*m)*main`, 60*time.Second)
	step("print sizeof(struct account)", `= 16`, 30*time.Second)
	step("quit", `lab\$ |\$ `, 30*time.Second)
	t.Logf("echo latencies: %v", c.Latencies())

	if _, err := m.Stop(ctx, in.ID, orch.ReasonUserStop); err != nil {
		t.Fatal(err)
	}
	if code := waitClose(c, 30*time.Second); code != 1000 {
		t.Fatalf("close status %d, want 1000 after stop", code)
	}
	m.Settle()
	infos, err := rt.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range infos {
		if i.ID == "lab-"+in.ID {
			t.Fatal("container still exists after stop")
		}
	}
	rec.Close()
	var lines []string
	for _, e := range st.Events() {
		if e.Type == "command_entered" {
			lines = append(lines, e.Data["line"].(string))
		}
	}
	if !strings.Contains(strings.Join(lines, "|"), "break main|run|print sizeof(struct account)|quit") {
		t.Fatalf("captured commands %q", lines)
	}
}

func waitClose(c *client.Client, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if code := c.CloseStatus(); code != -1 {
			return int(code)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return -1
}
