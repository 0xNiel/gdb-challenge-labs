package perf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func loadRealLearner(t *testing.T) map[string]Episode {
	t.Helper()
	eps, err := LoadLearner("../../perf/learner.txt")
	if err != nil {
		t.Fatal(err)
	}
	return eps
}

// The episodes cover the five tier-1 labs, with their real slugs and binary names, and each
// starts and ends at the shell.
func TestLearner_RealFileMatchesTheLabs(t *testing.T) {
	t.Parallel()
	eps := loadRealLearner(t)
	manifests, _ := filepath.Glob("../../../challenges/tier1-c-fundamentals/*/manifest.yaml")
	if len(manifests) != 5 || len(eps) != 5 {
		t.Fatalf("%d manifests, %d episodes; want 5 and 5", len(manifests), len(eps))
	}
	slugRE, entryRE := regexp.MustCompile(`(?m)^slug: *(\S+)`), regexp.MustCompile(`(?m)^ *entry: *(\S+)`)
	for _, m := range manifests {
		b, _ := os.ReadFile(m)
		slug, entry := slugRE.FindSubmatch(b)[1], entryRE.FindSubmatch(b)[1]
		ep, ok := eps[string(slug)]
		if !ok {
			t.Errorf("no episode for %s", slug)
			continue
		}
		if ep.Entry != string(entry) {
			t.Errorf("%s: entry %q, manifest says %q", slug, ep.Entry, entry)
		}
		first, last := ep.Steps[0], ep.Steps[len(ep.Steps)-1]
		if !first.AtShell || first.GDB || first.Cmd != "./"+string(entry) {
			t.Errorf("%s: first step %+v, want a plain run at the shell", slug, first)
		}
		if last.Cmd != "quit" || last.GDB {
			t.Errorf("%s: last step %+v, want quit", slug, last)
		}
		verbs := map[string]bool{}
		for _, st := range ep.Steps {
			verbs[st.Verb()] = true
		}
		for _, v := range []string{"shell_run", "gdb_start", "run", "gdb_quit"} {
			if !verbs[v] {
				t.Errorf("%s: no %s step", slug, v)
			}
		}
	}
	if got := LearnerSlugs(eps); got[0] != "tier1-01-off-by-one" || len(got) != 5 {
		t.Errorf("slugs %v", got)
	}
}

func TestLearner_ParseRejects(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"gdb line at the shell": "== a x\n$ ./x\nrun\n",
		"shell line in gdb":     "== a x\n$ gdb -q ./x\n$ ls\nquit\n",
		"ends inside gdb":       "== a x\n$ gdb -q ./x\nrun\n",
		"line before a lab":     "$ ./x\n",
		"bad header":            "== a\n$ ./x\n",
		"duplicate lab":         "== a x\n$ ./x\n== a x\n$ ./x\n",
		"empty":                 "# nothing\n",
		"empty episode":         "== a x\n== b y\n$ ./y\n",
	} {
		if _, err := ParseLearner(strings.NewReader(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLearner_Verbs(t *testing.T) {
	t.Parallel()
	for st, want := range map[Step]string{
		{Cmd: "./scores", AtShell: true}:                   "shell_run",
		{Cmd: "gdb -q ./scores", AtShell: true, GDB: true}: "gdb_start",
		{Cmd: "quit"}:                        "gdb_quit",
		{Cmd: "watch f.checksum", GDB: true}: "watch",
		{Cmd: "run", GDB: true}:              "run",
	} {
		if got := st.Verb(); got != want {
			t.Errorf("%+v: %s, want %s", st, got, want)
		}
	}
}

// A learner works its lab from the shell: plain run, gdb, commands, quit, and again; it never
// leaves gdb running at the end.
func TestVUser_Learner(t *testing.T) {
	t.Parallel()
	f := newFakeLabd(t)
	p := LearnerProfile(loadRealLearner(t))
	cfg := f.cfg(p, 11, 10*time.Minute)
	cfg.Challenge = "tier1-01-off-by-one"
	res := RunVUser(context.Background(), cfg)
	if res.Err != "" {
		t.Fatalf("learner failed: %s", res.Err)
	}
	// 6 a minute with 20 % idle over 10 minutes: about 48 lines, more than one episode.
	if res.Commands < 35 || res.Commands > 60 {
		t.Errorf("%d commands in 10 minutes, want about 48", res.Commands)
	}
	if res.Errors != 0 || res.StartToPromptMS <= 0 {
		t.Errorf("result %+v", res)
	}
	for _, v := range []string{"shell_run", "gdb_start", "run", "next", "gdb_quit"} {
		if len(res.CmdMS[v]) == 0 {
			t.Errorf("no %s timings: %v", v, res.CmdMS)
		}
	}
	if len(res.CmdMS["gdb_start"]) < 2 {
		t.Errorf("gdb started %d times; the episode should repeat", len(res.CmdMS["gdb_start"]))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lines[0] != "./scores" || f.lines[1] != "gdb -q ./scores" {
		t.Errorf("first lines %q", f.lines[:2])
	}
	if f.gdb["s11"] {
		t.Error("left gdb running at the end")
	}
	if len(f.deleted) != 1 {
		t.Errorf("deleted %v", f.deleted)
	}
}

func TestVUser_LearnerUnknownLab(t *testing.T) {
	t.Parallel()
	f := newFakeLabd(t)
	cfg := f.cfg(LearnerProfile(loadRealLearner(t)), 12, time.Minute)
	cfg.Challenge = "perf"
	if res := RunVUser(context.Background(), cfg); !strings.Contains(res.Err, "no learner episode") {
		t.Errorf("err %q", res.Err)
	}
}

// P10's mix spreads learners round-robin over the labs and keeps abusers on the perf image.
func TestP10_ProfilesAndLabs(t *testing.T) {
	t.Parallel()
	got := assignProfiles(map[string]int{Learner: 90, Abuser: 10}, 20)
	n := map[string]int{}
	for _, p := range got {
		n[p]++
	}
	if n[Learner] != 18 || n[Abuser] != 2 {
		t.Fatalf("mix %v", n)
	}
	r := &runner{cfg: RunConfig{Challenge: "perf", Profiles: map[string]Profile{
		Learner: LearnerProfile(loadRealLearner(t)), Abuser: {Name: Abuser, Abuse: true},
	}}}
	labs := map[string]int{}
	for _, p := range got {
		labs[r.vcfg(p, time.Minute).Challenge]++
	}
	if labs["perf"] != 2 || len(labs) != 6 {
		t.Errorf("labs %v, want perf twice and the five tier-1 labs", labs)
	}
	for slug, c := range labs {
		if slug != "perf" && (c < 3 || c > 4) {
			t.Errorf("%s got %d learners of 18", slug, c)
		}
	}
}

func p10Run(n int, pass bool, date string) RunFile {
	rf := RunFile{
		Meta: RunMeta{Scenario: "P10", Date: date, Arch: "x86_64", CPUs: 8, HostMemMB: 15000,
			Runtime: "io.containerd.runsc.v1", N: n, HoldS: 480, Mix: map[string]int{Learner: 90, Abuser: 10}},
		Summary: RunSummary{
			StartByProfile:     map[string]Pctl{Learner: {N: n, P95: 900}},
			EchoMSByProfile:    map[string]Pctl{Learner: {N: 100, P95: 4}},
			CmdMS:              map[string]Pctl{"next": {N: 10, P95: 30}, "gdb_start": {N: 5, P95: 700}},
			LabCPUPctByProfile: map[string]float64{Learner: 2.5, Abuser: 49},
			HostCPUPct:         P50P95Max{P95: float64(n) / 2},
		},
		Criteria: []Criterion{{Criterion: "learners' lab start p95 < 2 s", Measured: "x", Pass: true}},
	}
	if !pass {
		rf.Criteria = append(rf.Criteria, Criterion{Criterion: "`next` p95 < 250 ms", Measured: "p95 400 ms", Pass: false})
	}
	return rf
}

func TestCapacitySearch(t *testing.T) {
	t.Parallel()
	runs := map[int]RunFile{
		60: p10Run(60, true, "2026-10-02T10:00:00Z"), 90: p10Run(90, true, "2026-10-02T10:20:00Z"),
		120: p10Run(120, false, "2026-10-02T10:40:00Z"), 150: p10Run(150, true, "2026-10-02T11:00:00Z"),
	}
	files := map[int]string{60: "a", 90: "b", 120: "c", 150: "d"}
	cs := BuildCapacitySearch("linux-laptop", runs, files)
	// A pass above the first miss does not count: the knee is the first miss.
	if cs.MaxPassing != 90 || cs.FirstFail != 120 || cs.CPUs != 8 || cs.Date != "2026-10-02" || len(cs.Steps) != 4 {
		t.Fatalf("search %+v", cs)
	}
	if cs.Steps[0].LearnerCPU != 2.5 || cs.Steps[0].GDBStartMS.P95 != 700 {
		t.Errorf("step %+v", cs.Steps[0])
	}
	md := RenderCapacitySearch(cs)
	for _, want := range []string{"Largest count that met every criterion: 90.", "The first to miss was 120.",
		"**8 CPUs online**", "| 120 | **miss** |", "- 120 labs: `next` p95 < 250 ms: p95 400 ms", "learner 90, abuser 10"} {
		if !strings.Contains(md, want) {
			t.Errorf("render lacks %q:\n%s", want, md)
		}
	}
}

func TestCapacitySearch_LoadsNewestPerCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, rf RunFile) {
		b, _ := jsonMarshal(rf)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("run-P10-2026-10-02-linux-laptop-n60.json", p10Run(60, false, "2026-10-02T09:00:00Z"))
	write("run-P10-2026-10-03-linux-laptop-n60.json", p10Run(60, true, "2026-10-03T09:00:00Z"))
	write("run-P10-2026-10-03-dev-vm-n60.json", p10Run(60, false, "2026-10-03T09:00:00Z"))
	write("run-P2-2026-10-03-linux-laptop.json", p10Run(100, false, "2026-10-03T09:00:00Z"))
	cs, err := LoadCapacitySearch(dir, "linux-laptop")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Steps) != 1 || !cs.Steps[0].Pass || cs.Steps[0].File != "run-P10-2026-10-03-linux-laptop-n60.json" {
		t.Errorf("steps %+v", cs.Steps)
	}
	if _, err := LoadCapacitySearch(dir, "hostinger"); err == nil {
		t.Error("no runs for a host should be an error")
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
