// Command labd-perf is the load driver for the perf suite (spec "Local performance test
// suite", Phase 4). It drives a running labd through its internal API and WebSocket.
//
//	labd-perf run --scenario P2 --n 100 --ramp 5 --hold 20m --out docs/metrics
//	labd-perf report --in docs/metrics --host linux-laptop
//	labd-perf capacity --in docs/metrics --host linux-laptop   (P10 runs, ADR 0017)
//	labd-perf profiles
//
// labd/perf/scenario.sh starts a private labd, runs this, and does the host actions some
// scenarios need (P5 kills labd, P7 flushes the image, P9 samples disk). Run it with the
// containerd group (scripts/with-containerd-group.sh) so it can count containers.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/perf"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "report":
		err = report(os.Args[2:])
	case "capacity":
		err = capacity(os.Args[2:])
	case "profiles":
		err = profiles(os.Args[2:])
	case "--version", "-version", "version":
		fmt.Println("labd-perf", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "labd-perf:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: labd-perf run|report|capacity|profiles [flags] (labd-perf <cmd> -h for flags)")
}

// defaults per scenario (spec table): N, profile mix, hold.
var scenarioDefaults = map[string]struct {
	n    int
	hold time.Duration
	mix  map[string]int
}{
	"P1": {1, 10 * time.Minute, map[string]int{perf.Stepper: 1}},
	"P2": {100, 20 * time.Minute, map[string]int{perf.Reader: 1}},
	"P3": {100, 20 * time.Minute, map[string]int{perf.Reader: 60, perf.Stepper: 30, perf.Abuser: 10}},
	"P4": {100, 30 * time.Minute, map[string]int{perf.Reader: 1}},
	"P5": {100, time.Minute, map[string]int{perf.Reader: 1}},
	"P6": {150, 0, nil},
	"P7": {10, 0, map[string]int{perf.Reader: 1}},
	"P8": {100, 10 * time.Minute, map[string]int{perf.Stepper: 1}},
	"P9": {100, 2 * time.Hour, map[string]int{perf.Reader: 1}},
	// P10: capacity search, one count per run (labd/perf/capacity.sh steps through them).
	"P10": {60, 8 * time.Minute, map[string]int{perf.Learner: 90, perf.Abuser: 10}},
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	scenario := fs.String("scenario", "", "P1..P10")
	n := fs.Int("n", 0, "sessions (P6: requests); default from the spec table")
	ramp := fs.Float64("ramp", 5, "sessions started per second")
	hold := fs.Duration("hold", 0, "hold (P4/P9: churn time); default from the spec table")
	api := fs.String("api", "http://127.0.0.1:18081", "labd internal API")
	ws := fs.String("ws", "ws://127.0.0.1:18082", "labd WebSocket gateway")
	script := fs.String("script", "images/perf/session.gdb", "session.gdb")
	challenge := fs.String("challenge", "perf", "challenge slug (learners use the labs in --learner)")
	learner := fs.String("learner", "labd/perf/learner.txt", "learner episodes (P10)")
	mixFlag := fs.String("mix", "", "profile mix, e.g. learner=90,abuser=10; default from the scenario")
	out := fs.String("out", "docs/metrics", "directory for run-*.json")
	host := fs.String("host", "", "host label for the file name (e.g. linux-laptop)")
	rt := fs.String("runtime", "io.containerd.runsc.v1", "the runtime labd was started with (meta only)")
	platform := fs.String("platform", "", "gVisor platform (meta only: systrap, kvm)")
	label := fs.String("label", "", "suffix for the file name (runc, kvm, ...)")
	partial := fs.String("partial", "", "why N is below the spec's, if it is")
	socket := fs.String("containerd", "/run/containerd/containerd.sock", "containerd socket (container counts)")
	fifoDir := fs.String("fifo-dir", filepath.Join(os.TempDir(), fmt.Sprintf("labd-%d", os.Getuid()), "fifo"), "labd's FIFO directory")
	churn := fs.Duration("churn", 5*time.Second, "P4/P9: one session out and one in this often")
	diskCmd := fs.String("disk-cmd", "", "command printing disk usage as JSON")
	diskEvery := fs.Duration("disk-every", 5*time.Minute, "how often to run --disk-cmd")
	ready := fs.String("ready-file", "", "P5: written when the sessions are up (the wrapper then kills labd)")
	pull := fs.String("pull-cmd", "", "P7: brings the removed image back")
	_ = fs.Parse(args)

	d, ok := scenarioDefaults[*scenario]
	if !ok {
		return fmt.Errorf("--scenario must be P1..P10, got %q", *scenario)
	}
	if *n == 0 {
		*n = d.n
	}
	if *hold == 0 {
		*hold = d.hold
	}
	if *host == "" {
		*host, _ = os.Hostname()
	}
	secret := os.Getenv("LABD_INTERNAL_SECRET")
	if secret == "" {
		return fmt.Errorf("LABD_INTERNAL_SECRET is not set")
	}
	sc, err := perf.LoadScript(*script)
	if err != nil {
		return err
	}
	ps, err := perf.Profiles(sc)
	if err != nil {
		return err
	}
	mix := d.mix
	if *mixFlag != "" {
		if mix, err = parseMix(*mixFlag); err != nil {
			return err
		}
	}
	if mix[perf.Learner] > 0 {
		eps, err := perf.LoadLearner(*learner)
		if err != nil {
			return err
		}
		ps[perf.Learner] = perf.LearnerProfile(eps)
	}
	cfg := perf.RunConfig{
		Scenario: *scenario, N: *n, Ramp: *ramp, Hold: *hold, Mix: mix,
		API: *api, WS: *ws, Secret: secret, Challenge: *challenge, Profiles: ps,
		Host: *host, Runtime: *rt, Platform: *platform, Label: *label, Partial: *partial, Out: *out,
		Command: "labd-perf run " + strings.Join(args, " "),
		Root:    "/", CgroupDir: "sys/fs/cgroup/labs", FIFODir: *fifoDir,
		Churn: *churn, DiskCmd: *diskCmd, DiskEvery: *diskEvery, ReadyFile: *ready, PullCmd: *pull,
	}
	if rtc, err := orch.NewContainerdRuntime(*socket, *rt, ""); err == nil {
		defer rtc.Close()
		cfg.Containers = func(ctx context.Context) (int, error) {
			cs, err := rtc.List(ctx)
			return len(cs), err
		}
	} else {
		fmt.Fprintf(os.Stderr, "labd-perf: no containerd access (%v); container counts are skipped\n", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	path, rf, err := perf.Run(ctx, cfg)
	if path != "" {
		fmt.Fprintf(os.Stderr, "labd-perf: wrote %s\n", path)
	}
	failed := 0
	for _, c := range rf.Criteria {
		v := "PASS"
		if !c.Pass {
			v, failed = "MISS", failed+1
		}
		fmt.Fprintf(os.Stderr, "  %s  %s: %s\n", v, c.Criterion, c.Measured)
	}
	if err != nil {
		return err
	}
	if failed > 0 {
		// A missed perf criterion is a finding, not a broken run (ADR 0006): the run file
		// has it, and capacity.md needs a decision line.
		fmt.Fprintf(os.Stderr, "labd-perf: %d criteria missed; record a decision in docs/metrics/capacity.md\n", failed)
	}
	return nil
}

// parseMix reads "learner=90,abuser=10".
func parseMix(s string) (map[string]int, error) {
	mix := map[string]int{}
	for _, part := range strings.Split(s, ",") {
		name, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); !ok || err != nil || n < 0 {
			return nil, fmt.Errorf("--mix: want name=count[,name=count], got %q", part)
		}
		switch name {
		case perf.Reader, perf.Stepper, perf.Abuser, perf.Learner:
			mix[name] = n
		default:
			return nil, fmt.Errorf("--mix: unknown profile %q", name)
		}
	}
	return mix, nil
}

// capacity summarises a capacity search: the newest P10 run at each count for --host, as
// docs/metrics/capacity-search-<date>-<host>.{json,md} (Phase 7, task 7.11).
func capacity(args []string) error {
	fs := flag.NewFlagSet("capacity", flag.ExitOnError)
	in := fs.String("in", "docs/metrics", "directory with run-P10-*.json")
	host := fs.String("host", "", "host label of the runs (e.g. linux-laptop)")
	_ = fs.Parse(args)
	if *host == "" {
		return fmt.Errorf("--host is required")
	}
	cs, err := perf.LoadCapacitySearch(*in, *host)
	if err != nil {
		return err
	}
	base := filepath.Join(*in, fmt.Sprintf("capacity-search-%s-%s", cs.Date, *host))
	b, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(base+".md", []byte(perf.RenderCapacitySearch(cs)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "labd-perf: wrote %s.{json,md}: %d counts, largest passing %d\n", base, len(cs.Steps), cs.MaxPassing)
	return nil
}

func profiles(args []string) error {
	fs := flag.NewFlagSet("profiles", flag.ExitOnError)
	script := fs.String("script", "images/perf/session.gdb", "session.gdb")
	_ = fs.Parse(args)
	sc, err := perf.LoadScript(*script)
	if err != nil {
		return err
	}
	ps, err := perf.Profiles(sc)
	if err != nil {
		return err
	}
	for _, name := range []string{perf.Reader, perf.Stepper, perf.Abuser} {
		p := ps[name]
		fmt.Printf("%s: %.0f commands/min, %.0f %% idle\n  setup: %s\n", p.Name, p.PerMin, p.IdleFrac*100, strings.Join(p.Setup, " | "))
		if p.Abuse {
			fmt.Println("  then: cpu loop, probe fork, probe fill, 100 KB paste; then one print a minute")
		} else {
			fmt.Printf("  loop (%d): %s\n", len(p.Loop), strings.Join(p.Loop, " | "))
		}
	}
	return nil
}

// report merges the newest run of each scenario for --host into perf-report-<date>-<host>.json
// (the spec's schema) and rewrites the generated block of capacity.md.
func report(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	in := fs.String("in", "docs/metrics", "directory with run-*.json")
	host := fs.String("host", "", "host label of the runs (e.g. linux-laptop)")
	out := fs.String("out", "", "report path (default <in>/perf-report-<date>-<host>.json)")
	md := fs.String("md", "", "capacity.md to update (default <in>/capacity.md; \"-\" skips it)")
	_ = fs.Parse(args)
	if *host == "" {
		return fmt.Errorf("--host is required")
	}
	runs, err := perf.LoadRuns(*in, *host)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("no run-P*-*-%s*.json in %s", *host, *in)
	}
	rep, missing := perf.BuildReport(runs)
	date := time.Now().UTC().Format("2006-01-02")
	if len(rep.Meta.Date) >= 10 {
		date = rep.Meta.Date[:10]
	}
	if *out == "" {
		*out = filepath.Join(*in, fmt.Sprintf("perf-report-%s-%s.json", date, *host))
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "labd-perf: wrote %s from %d runs\n", *out, len(runs))
	for _, m := range missing {
		fmt.Fprintf(os.Stderr, "  not measured: %s\n", m)
	}
	if *md == "-" {
		return nil
	}
	if *md == "" {
		*md = filepath.Join(*in, "capacity.md")
	}
	cur, err := os.ReadFile(*md)
	if err != nil {
		return err
	}
	next, err := perf.ReplaceBlock(string(cur), perf.RenderCapacity(rep, runs, missing))
	if err != nil {
		return fmt.Errorf("%s: %w", *md, err)
	}
	if err := os.WriteFile(*md, []byte(next), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "labd-perf: updated %s\n", *md)
	return nil
}
