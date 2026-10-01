package orch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
)

// cleanupTimeout bounds each containerd call made while tearing a container down.
const cleanupTimeout = 30 * time.Second

// ContainerdRuntime is the Runtime backed by the containerd daemon.
type ContainerdRuntime struct {
	client  *containerd.Client
	runtime string // io.containerd.runsc.v1, or runc on dev hosts
	fifoDir string // terminal FIFOs; "" uses containerd's default (/run/containerd/fifo)
	closing chan struct{}
	once    sync.Once
}

// NewContainerdRuntime connects to the containerd socket. runtime is the shim name from
// labd.yaml. fifoDir, if set, is created (0700) and holds the terminal FIFOs.
func NewContainerdRuntime(socket, runtime, fifoDir string) (*ContainerdRuntime, error) {
	if fifoDir != "" {
		if err := os.MkdirAll(fifoDir, 0o700); err != nil {
			return nil, fmt.Errorf("fifo dir: %w", err)
		}
	}
	c, err := containerd.New(socket, containerd.WithDefaultNamespace(Namespace))
	if err != nil {
		return nil, fmt.Errorf("connect containerd %s: %w", socket, err)
	}
	return &ContainerdRuntime{client: c, runtime: runtime, fifoDir: fifoDir, closing: make(chan struct{})}, nil
}

// Close releases the client connection. Containers keep running.
func (r *ContainerdRuntime) Close() error {
	r.once.Do(func() { close(r.closing) })
	return r.client.Close()
}

// nsctx puts a context in namespace labs.
func (r *ContainerdRuntime) nsctx(ctx context.Context) context.Context {
	return namespaces.WithNamespace(ctx, Namespace)
}

func (r *ContainerdRuntime) ioOpts(stdin io.Reader, stdout io.Writer) []cio.Opt {
	o := []cio.Opt{cio.WithStreams(stdin, stdout, nil), cio.WithTerminal}
	if r.fifoDir != "" {
		o = append(o, cio.WithFIFODir(r.fifoDir))
	}
	return o
}

func (r *ContainerdRuntime) Create(ctx context.Context, o CreateOpts) (_ Container, err error) {
	ctx = r.nsctx(ctx)
	img, err := r.client.GetImage(ctx, o.Image)
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", o.Image, err)
	}
	if ok, _ := img.IsUnpacked(ctx, snapshotter); !ok {
		if err := img.Unpack(ctx, snapshotter); err != nil {
			return nil, fmt.Errorf("unpack %s: %w", o.Image, err)
		}
	}
	ctr, err := r.client.NewContainer(ctx, o.ID,
		containerd.WithImage(img),
		containerd.WithSnapshotter(snapshotter),
		// A read-only view: the root is read-only anyway (S3), so no writable layer is needed.
		containerd.WithNewSnapshotView(o.ID, img),
		containerd.WithRuntime(r.runtime, nil),
		containerd.WithSpec(o.Spec),
		containerd.WithContainerLabels(o.Labels),
	)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}
	defer func() {
		if err != nil {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			defer cancel()
			_ = ctr.Delete(cctx, containerd.WithSnapshotCleanup)
		}
	}()

	c := newCtrdContainer(ctr, "/sys/fs/cgroup"+o.Spec.Linux.CgroupsPath, r.closing)
	task, err := ctr.NewTask(ctx, cio.NewCreator(r.ioOpts(c.stdinR, c.stdoutW)...))
	if err != nil {
		c.closePipes()
		return nil, fmt.Errorf("create task: %w", err)
	}
	if err := c.watch(ctx, task); err != nil {
		_, _ = task.Delete(context.WithoutCancel(ctx), containerd.WithProcessKill)
		c.closePipes()
		return nil, err
	}
	return c, nil
}

func (r *ContainerdRuntime) ImageWorkingDir(ctx context.Context, image string) (string, error) {
	ctx = r.nsctx(ctx)
	img, err := r.client.GetImage(ctx, image)
	if err != nil {
		return "", fmt.Errorf("image %s: %w", image, err)
	}
	return imageWorkingDir(ctx, img)
}

// imageWorkingDir reads WORKDIR from the image's config; "" if it sets none (ADR 0014).
// labd's sessions and RunOnce (specrun, the challenge oracle) both use it, so a lab starts in
// the same directory either way.
func imageWorkingDir(ctx context.Context, img containerd.Image) (string, error) {
	cfg, err := img.Spec(ctx)
	if err != nil {
		return "", fmt.Errorf("config of %s: %w", img.Name(), err)
	}
	if cfg.Config.WorkingDir == "" {
		return "", nil
	}
	return path.Clean(cfg.Config.WorkingDir), nil
}

func (r *ContainerdRuntime) List(ctx context.Context) ([]ContainerInfo, error) {
	ctx = r.nsctx(ctx)
	cs, err := r.client.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]ContainerInfo, 0, len(cs))
	for _, c := range cs {
		labels, err := c.Labels(ctx)
		if err != nil && !errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("labels of %s: %w", c.ID(), err)
		}
		out = append(out, ContainerInfo{ID: c.ID(), Labels: labels})
	}
	return out, nil
}

func (r *ContainerdRuntime) Attach(ctx context.Context, id string) (Container, error) {
	ctx = r.nsctx(ctx)
	ctr, err := r.client.LoadContainer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load container %s: %w", id, err)
	}
	spec, err := ctr.Spec(ctx)
	if err != nil {
		return nil, fmt.Errorf("spec of %s: %w", id, err)
	}
	cg := ""
	if spec.Linux != nil {
		cg = "/sys/fs/cgroup" + spec.Linux.CgroupsPath
	}
	c := newCtrdContainer(ctr, cg, r.closing)
	task, err := ctr.Task(ctx, cio.NewAttach(r.ioOpts(c.stdinR, c.stdoutW)...))
	if err != nil {
		c.closePipes()
		if errdefs.IsNotFound(err) {
			return nil, ErrNoTask
		}
		return nil, fmt.Errorf("attach %s: %w", id, err)
	}
	// From here on, a failure must also release the attached terminal copy.
	release := func() {
		if tio := task.IO(); tio != nil {
			tio.Cancel()
			tio.Close()
		}
		c.closePipes()
	}
	st, err := task.Status(ctx)
	if err != nil {
		release()
		return nil, fmt.Errorf("status of %s: %w", id, err)
	}
	if st.Status != containerd.Running {
		release()
		return nil, ErrNoTask
	}
	if err := c.watch(ctx, task); err != nil {
		release()
		return nil, err
	}
	return c, nil
}

func (r *ContainerdRuntime) Remove(ctx context.Context, id string) error {
	ctx = r.nsctx(ctx)
	ctr, err := r.client.LoadContainer(ctx, id)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load container %s: %w", id, err)
	}
	if task, err := ctr.Task(ctx, nil); err == nil {
		if exitCh, err := task.Wait(ctx); err == nil {
			_ = task.Kill(ctx, syscall.SIGKILL)
			select {
			case <-exitCh:
			case <-time.After(killWait):
			}
		}
		if _, err := task.Delete(ctx, containerd.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("delete task %s: %w", id, err)
		}
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("task of %s: %w", id, err)
	}
	if err := ctr.Delete(ctx, containerd.WithSnapshotCleanup); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("delete container %s: %w", id, err)
	}
	return nil
}

// ctrdContainer is one container and its task. The terminal is bridged through two pipes:
// keystrokes written to stdinW reach the task, and the task's output is read from stdoutR.
type ctrdContainer struct {
	ctr     containerd.Container
	task    containerd.Task
	cgroup  string
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	done    chan struct{}
	closing <-chan struct{} // closed by ContainerdRuntime.Close
	delOnce sync.Once
	delErr  error
}

// failedWait is an exit channel that reports err, so the watch loop retries.
func failedWait(err error) <-chan containerd.ExitStatus {
	ch := make(chan containerd.ExitStatus, 1)
	ch <- *containerd.NewExitStatus(containerd.UnknownExitStatus, time.Time{}, err)
	return ch
}

func newCtrdContainer(ctr containerd.Container, cgroup string, closing <-chan struct{}) *ctrdContainer {
	c := &ctrdContainer{ctr: ctr, cgroup: cgroup, done: make(chan struct{}), closing: closing}
	c.stdinR, c.stdinW = io.Pipe()
	c.stdoutR, c.stdoutW = io.Pipe()
	return c
}

// watch subscribes to the task's exit before anything can start it, and closes done on exit.
// The wait outlives ctx: a session's task lives much longer than the request that made it.
func (c *ctrdContainer) watch(ctx context.Context, task containerd.Task) error {
	wctx := context.WithoutCancel(ctx)
	exitCh, err := task.Wait(wctx)
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}
	c.task = task
	go func() {
		// A failed Wait RPC (containerd restarting, or this client closing) arrives as an
		// ExitStatus with an error. It is not an exit: re-subscribe until the task really
		// exits or the client is closed, so a containerd hiccup never tears labs down.
		backoff := 100 * time.Millisecond
		for {
			st := <-exitCh
			if st.Error() == nil {
				break
			}
			select {
			case <-c.closing:
				return // labd is shutting down; the lab keeps running for the next labd
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Second)
			if ch, err := task.Wait(wctx); err == nil {
				exitCh = ch
			} else {
				exitCh = failedWait(err)
			}
		}
		close(c.done)
		// Give the terminal copy a moment to deliver the last output, then signal EOF.
		if tio := task.IO(); tio != nil {
			waited := make(chan struct{})
			go func() { tio.Wait(); close(waited) }()
			select {
			case <-waited:
			case <-time.After(2 * time.Second):
			}
		}
		_ = c.stdoutW.Close()
	}()
	return nil
}

// closePipes ends both terminal pipes; used when the container will never be returned.
func (c *ctrdContainer) closePipes() {
	_ = c.stdinW.Close()
	_ = c.stdoutW.Close()
}

func (c *ctrdContainer) ID() string { return c.ctr.ID() }

func (c *ctrdContainer) Start(ctx context.Context) error {
	if err := c.task.Start(namespaces.WithNamespace(ctx, Namespace)); err != nil {
		return fmt.Errorf("start task: %w", err)
	}
	return nil
}

func (c *ctrdContainer) Kill(ctx context.Context) error {
	ctx = namespaces.WithNamespace(ctx, Namespace)
	select {
	case <-c.done:
		return nil
	default:
	}
	if err := c.task.Kill(ctx, syscall.SIGKILL); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("kill: %w", err)
	}
	select {
	case <-c.done:
		return nil
	case <-time.After(killWait):
		return fmt.Errorf("task %s still running %s after SIGKILL", c.ID(), killWait)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *ctrdContainer) Delete(ctx context.Context) error {
	c.delOnce.Do(func() {
		ctx := namespaces.WithNamespace(ctx, Namespace)
		_ = c.stdinW.Close()
		var errs []error
		if _, err := c.task.Delete(ctx, containerd.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete task: %w", err))
		}
		if err := c.ctr.Delete(ctx, containerd.WithSnapshotCleanup); err != nil && !errdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete container: %w", err))
		}
		c.delErr = errors.Join(errs...)
	})
	return c.delErr
}

func (c *ctrdContainer) Done() <-chan struct{} { return c.done }

func (c *ctrdContainer) Resize(ctx context.Context, cols, rows uint32) error {
	return c.task.Resize(namespaces.WithNamespace(ctx, Namespace), cols, rows)
}

func (c *ctrdContainer) IO() (io.WriteCloser, io.Reader) { return c.stdinW, c.stdoutR }

func (c *ctrdContainer) CgroupPath() string { return c.cgroup }
