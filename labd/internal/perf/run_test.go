package perf

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAssignProfiles_KeepsTheMixInEveryPrefix(t *testing.T) {
	t.Parallel()
	got := assignProfiles(map[string]int{Reader: 60, Stepper: 30, Abuser: 10}, 100)
	count := map[string]int{}
	for i, p := range got {
		count[p]++
		if n := i + 1; n%10 == 0 && (count[Abuser] != n/10 || count[Stepper] != 3*n/10) {
			t.Fatalf("after %d users: %v", n, count)
		}
	}
	if count[Reader] != 60 || count[Stepper] != 30 || count[Abuser] != 10 {
		t.Fatalf("%v", count)
	}
	for _, p := range assignProfiles(nil, 3) {
		if p != Reader {
			t.Fatalf("no mix gave %q", p)
		}
	}
}

// A small P2 end to end against the fake labd: every user runs, the run file is written
// with its summary and criteria, and nothing is left.
func TestRun_SteadyAgainstFakeLabd(t *testing.T) {
	t.Parallel()
	f := newFakeLabd(t)
	ps, _ := Profiles(loadRealScript(t))
	out := t.TempDir()
	cfg := RunConfig{
		Scenario: "P2", N: 3, Ramp: 50, Hold: 2 * time.Second, Mix: map[string]int{Reader: 1},
		API: f.srv.URL, WS: "ws" + strings.TrimPrefix(f.srv.URL, "http"), Secret: "s", Challenge: "perf",
		Profiles: ps, Host: "test", Out: out, Root: t.TempDir(), CgroupDir: "labs", Settle: time.Millisecond,
		Log: io.Discard,
	}
	path, rf, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, out+"/run-P2-") || !strings.HasSuffix(path, "-test.json") {
		t.Errorf("path %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back RunFile
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	s := back.Summary
	if s.Sessions != 3 || s.VUserErrors != 0 || s.StartToPromptMS.N != 3 || s.EchoMS.N == 0 {
		t.Errorf("summary %+v", s)
	}
	if len(back.Criteria) == 0 || len(back.Timeline) < 3 || back.Meta.Mix[Reader] != 1 {
		t.Errorf("criteria %v timeline %v meta %+v", back.Criteria, back.Timeline, back.Meta)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.live) != 0 || len(f.deleted) != 3 {
		t.Errorf("live %v deleted %v", f.live, f.deleted)
	}
	_ = rf
}
