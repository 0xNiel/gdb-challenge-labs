// Command specrun runs one container under the lab sandbox spec and exits with its status.
// It is a developer and test tool (P0 checks, single-lab measurements), not part of labd.
//
//	sudo specrun --image docker.io/gdblabs/perf:dev [--runtime runc] [--timeout 60s] \
//	    [--mark '\(gdb\) '] [--memory-mb 128] -- gdb -batch -x /opt/perf/checks/x.gdb /opt/perf/perf
//
// Stdout: the container's PTY output. Stderr: one line `specrun-stats: {json}` with timings,
// the cgroup path and the host PID. Exit code: the container's, or 124 on timeout (like
// timeout(1)), or 125 when specrun itself failed. Needs root (containerd FIFOs and snapshots).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
)

func main() { os.Exit(run()) }

func run() int {
	def := config.Default()
	var (
		image   = flag.String("image", "", "image reference in containerd namespace labs (required)")
		spec    = flag.String("spec", "sandbox/sandbox-base.json", "base OCI spec")
		runtime = flag.String("runtime", "runsc", "runsc or runc")
		socket  = flag.String("socket", def.ContainerdSocket, "containerd socket")
		id      = flag.String("id", fmt.Sprintf("specrun-%d", os.Getpid()), "container id")
		timeout = flag.Duration("timeout", 60*time.Second, "kill the container after this long (0 = never)")
		mark    = flag.String("mark", "", "regexp; report time from start to its first match in the output")
		quiet   = flag.Bool("quiet", false, "do not copy PTY output to stdout")
		stdinF  = flag.Bool("stdin", false, "forward this process's stdin as keystrokes")
		memMB   = flag.Int("memory-mb", def.DefaultLimits.MemoryMB, "memory limit")
		cpuM    = flag.Int("cpu-millicores", def.DefaultLimits.CPUMillicores, "CPU quota")
		pids    = flag.Int("pids", def.DefaultLimits.Pids, "pids limit")
	)
	flag.Parse()
	if *image == "" {
		fmt.Fprintln(os.Stderr, "specrun: --image is required")
		return 125
	}
	rt := orch.RuntimeRunsc
	switch *runtime {
	case "runsc":
	case "runc":
		rt = orch.RuntimeRunc
	default:
		fmt.Fprintf(os.Stderr, "specrun: --runtime must be runsc or runc, got %q\n", *runtime)
		return 125
	}
	base, err := os.ReadFile(*spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "specrun:", err)
		return 125
	}
	var markRE *regexp.Regexp
	if *mark != "" {
		if markRE, err = regexp.Compile(*mark); err != nil {
			fmt.Fprintln(os.Stderr, "specrun: --mark:", err)
			return 125
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var out io.Writer = os.Stdout
	if *quiet {
		out = nil
	}
	var in io.Reader
	if *stdinF {
		in = os.Stdin
	}
	stats := map[string]any{"id": *id, "runtime": *runtime, "image": *image}
	opts := orch.RunOnceOpts{
		Socket: *socket, Image: *image, Runtime: rt, BaseSpec: base, Timeout: *timeout,
		Stdin: in, Output: out, Mark: markRE,
		Params: orch.SpecParams{
			ID:          *id,
			Args:        flag.Args(),
			Limits:      config.Limits{MemoryMB: *memMB, CPUMillicores: *cpuM, Pids: *pids, TTLMinutes: 1, IdleMinutes: 1, ExtendMinutes: 1},
			Annotations: map[string]string{"lab.tool": "specrun"},
		},
		OnStart: func(pid uint32, cg string) {
			stats["pid"], stats["cgroup"] = pid, cg
			// Early line so a caller can start sampling while the container runs.
			fmt.Fprintf(os.Stderr, "specrun-started: {\"pid\":%d,\"cgroup\":%q}\n", pid, cg)
		},
	}
	if len(opts.Params.Args) == 0 {
		opts.Params.Args = nil // keep the base spec's /bin/sh
	}

	res, err := orch.RunOnce(ctx, opts)
	stats["create_ms"] = res.CreateTime.Milliseconds()
	stats["start_ms"] = res.StartTime.Milliseconds()
	stats["exit_code"] = res.ExitCode
	stats["timed_out"] = res.TimedOut
	if markRE != nil {
		stats["mark_ms"] = res.MarkAfter.Milliseconds()
		stats["mark_found"] = res.MarkAfter > 0
	}
	if err != nil {
		stats["error"] = err.Error()
	}
	b, _ := json.Marshal(stats)
	fmt.Fprintf(os.Stderr, "specrun-stats: %s\n", b)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "specrun:", strings.TrimSpace(err.Error()))
		return 125
	case res.TimedOut:
		return 124
	default:
		return res.ExitCode
	}
}
