package orch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

// Runtime names accepted by RunOnce. runc is registered only on dev hosts (provision.sh) and
// is used for the gVisor overhead reference runs; production has runsc only (S1).
const (
	RuntimeRunsc = "io.containerd.runsc.v1"
	RuntimeRunc  = "io.containerd.runc.v2"
	Namespace    = "labs"
	snapshotter  = "overlayfs"
)

// RunOnceOpts describes one short-lived container. Used by the P0 checks and measurements;
// Phase 2's session manager reuses the same building blocks.
type RunOnceOpts struct {
	Socket   string     // containerd socket
	Image    string     // image reference already present in namespace labs
	Runtime  string     // RuntimeRunsc or RuntimeRunc
	Params   SpecParams // ID, args, limits, annotations
	BaseSpec []byte     // contents of labd/sandbox/sandbox-base.json
	Timeout  time.Duration
	Cols     uint32 // PTY size; 0 means 200x50
	Rows     uint32
	Stdin    io.Reader // keystrokes; nil keeps stdin open and silent
	Output   io.Writer // PTY output is copied here as it arrives (may be nil)
	// Mark, if set, is matched against the output; RunResult.MarkAfter is the time from
	// task start to the first match (e.g. the gdb prompt, for start-to-prompt latency).
	Mark *regexp.Regexp
	// OnStart is called once the task is running, with its host PID and cgroup path, so a
	// caller can sample memory and CPU while the container runs.
	OnStart func(pid uint32, cgroupPath string)
}

// RunResult reports what happened. ExitCode is -1 when the container was killed on timeout.
type RunResult struct {
	ExitCode   int
	TimedOut   bool
	CreateTime time.Duration // NewContainer + NewTask (snapshot view, sandbox boot)
	StartTime  time.Duration // task.Start
	MarkAfter  time.Duration // since task start; 0 if Mark never matched
	Output     []byte
	// Cgroup is sampled every 200 ms while the task runs (peaks and the last counters).
	Cgroup CgroupStats
	// RunTime is task start to exit, for CPU rates (Cgroup.CPUUsageUsec / RunTime).
	RunTime time.Duration
}

// RunOnce creates a container from BaseSpec+Params, runs it to completion with a terminal
// (ADR 0007), and deletes it and its snapshot whatever happens.
func RunOnce(ctx context.Context, o RunOnceOpts) (RunResult, error) {
	var res RunResult
	if o.Runtime == "" {
		o.Runtime = RuntimeRunsc
	}
	if o.Cols == 0 || o.Rows == 0 {
		o.Cols, o.Rows = 200, 50
	}
	o.Params.Runtime = o.Runtime

	client, err := containerd.New(o.Socket)
	if err != nil {
		return res, fmt.Errorf("connect containerd: %w", err)
	}
	defer client.Close()
	ctx = namespaces.WithNamespace(ctx, Namespace)

	img, err := client.GetImage(ctx, o.Image)
	if err != nil {
		return res, fmt.Errorf("image %s: %w", o.Image, err)
	}
	if ok, _ := img.IsUnpacked(ctx, snapshotter); !ok {
		if err := img.Unpack(ctx, snapshotter); err != nil {
			return res, fmt.Errorf("unpack %s: %w", o.Image, err)
		}
	}
	if o.Params.Cwd == "" {
		if o.Params.Cwd, err = imageWorkingDir(ctx, img); err != nil {
			return res, err
		}
	}
	spec, err := BuildSpec(o.BaseSpec, o.Params)
	if err != nil {
		return res, err
	}

	id := o.Params.ID
	t0 := time.Now()
	ctr, err := client.NewContainer(ctx, id,
		containerd.WithImage(img),
		containerd.WithSnapshotter(snapshotter),
		// A read-only view: the root is read-only anyway (S3), so no writable layer is needed.
		containerd.WithNewSnapshotView(id, img),
		containerd.WithRuntime(o.Runtime, nil),
		containerd.WithSpec(spec),
		containerd.WithContainerLabels(o.Params.Annotations),
	)
	if err != nil {
		return res, fmt.Errorf("create container: %w", err)
	}
	// Cleanup uses a fresh context so it runs even after ctx is cancelled.
	defer func() {
		cctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), Namespace), 30*time.Second)
		defer cancel()
		_ = ctr.Delete(cctx, containerd.WithSnapshotCleanup)
	}()

	stdin := o.Stdin
	if stdin == nil {
		pr, pw := io.Pipe() // keep stdin open and silent until the task exits
		defer pw.Close()
		stdin = pr
	}
	out := &markWriter{w: o.Output, re: o.Mark}
	task, err := ctr.NewTask(ctx, cio.NewCreator(cio.WithStreams(stdin, out, nil), cio.WithTerminal))
	if err != nil {
		return res, fmt.Errorf("create task: %w", err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), Namespace), 30*time.Second)
		defer cancel()
		_, _ = task.Delete(cctx, containerd.WithProcessKill)
	}()
	res.CreateTime = time.Since(t0)

	exitCh, err := task.Wait(ctx)
	if err != nil {
		return res, fmt.Errorf("wait: %w", err)
	}
	t1 := time.Now()
	if err := task.Start(ctx); err != nil {
		return res, fmt.Errorf("start: %w", err)
	}
	res.StartTime = time.Since(t1)
	out.setStart(t1)
	_ = task.Resize(ctx, o.Cols, o.Rows)
	cgDir := "/sys/fs/cgroup" + spec.Linux.CgroupsPath
	if o.OnStart != nil {
		o.OnStart(task.Pid(), cgDir)
	}

	// Sample the cgroup until the task exits; the runtime may remove it right after exit.
	var (
		cgMu    sync.Mutex
		cgStats CgroupStats
	)
	sampleDone := make(chan struct{})
	stopSampling := make(chan struct{})
	go func() {
		defer close(sampleDone)
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			if st, err := ReadCgroupStats(cgDir); err == nil {
				cgMu.Lock()
				cgStats.Merge(st)
				cgMu.Unlock()
			}
			select {
			case <-stopSampling:
				return
			case <-tick.C:
			}
		}
	}()
	finishSampling := func() {
		close(stopSampling)
		<-sampleDone
		if st, err := ReadCgroupStats(cgDir); err == nil { // last reading if still there
			cgStats.Merge(st)
		}
		res.Cgroup = cgStats
		res.RunTime = time.Since(t1)
	}

	var timeout <-chan time.Time
	if o.Timeout > 0 {
		timer := time.NewTimer(o.Timeout)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case st := <-exitCh:
		finishSampling()
		code, _, err := st.Result()
		if err != nil {
			return res, fmt.Errorf("exit status: %w", err)
		}
		res.ExitCode = int(code)
	case <-timeout:
		finishSampling()
		res.TimedOut, res.ExitCode = true, -1
		_ = task.Kill(ctx, syscall.SIGKILL)
		<-exitCh
	case <-ctx.Done():
		finishSampling()
		_ = task.Kill(context.WithoutCancel(ctx), syscall.SIGKILL)
		<-exitCh
		return res, ctx.Err()
	}
	// Let the IO copy drain the last bytes of the PTY.
	task.CloseIO(ctx, containerd.WithStdinCloser)
	if tio := task.IO(); tio != nil {
		tio.Wait()
	}
	res.Output, res.MarkAfter = out.result()
	return res, nil
}

// markWriter tees PTY output, keeps a copy, and timestamps the first Mark match.
type markWriter struct {
	mu    sync.Mutex
	w     io.Writer
	re    *regexp.Regexp
	buf   bytes.Buffer
	start time.Time
	hit   time.Duration
}

func (m *markWriter) setStart(t time.Time) { m.mu.Lock(); m.start = t; m.mu.Unlock() }

func (m *markWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf.Write(p)
	if m.re != nil && m.hit == 0 && !m.start.IsZero() && m.re.Match(m.buf.Bytes()) {
		m.hit = time.Since(m.start)
	}
	if m.w != nil {
		_, _ = m.w.Write(p) // a failing consumer must never stall the PTY
	}
	return len(p), nil
}

func (m *markWriter) result() ([]byte, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return bytes.Clone(m.buf.Bytes()), m.hit
}
