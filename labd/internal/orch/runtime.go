package orch

import (
	"context"
	"errors"
	"io"
	"time"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Runtime is everything the session manager and the reconciler need from containerd. The
// containerd implementation is in runtime_containerd.go; tests use a fake. Nothing outside
// this package sees containerd types.
type Runtime interface {
	// Create makes the container and its task with a terminal (ADR 0007). The task is created
	// but not started.
	Create(ctx context.Context, o CreateOpts) (Container, error)
	// List returns every container in namespace labs, labelled or not.
	List(ctx context.Context) ([]ContainerInfo, error)
	// Attach loads an existing container and re-opens its terminal. ErrNoTask means the
	// container exists but has no live task.
	Attach(ctx context.Context, id string) (Container, error)
	// Remove kills and deletes a container by id, whatever state it is in. Used by the
	// reconciler for containers it will not adopt.
	Remove(ctx context.Context, id string) error
	// ImageWorkingDir is the image's WORKDIR, or "" if it sets none. The lab starts there
	// (ADR 0014).
	ImageWorkingDir(ctx context.Context, image string) (string, error)
}

// CreateOpts describes one lab container.
type CreateOpts struct {
	ID     string
	Image  string // reference present in the content store (a digest in production, S20)
	Spec   *specs.Spec
	Labels map[string]string
}

// ContainerInfo is what List reports.
type ContainerInfo struct {
	ID     string
	Labels map[string]string
}

// Container is one lab's container and task.
type Container interface {
	ID() string
	Start(ctx context.Context) error
	// Kill sends SIGKILL and waits up to killWait for the task to exit.
	Kill(ctx context.Context) error
	// Delete removes the task, the container and its snapshot. Safe to call after Kill or on
	// a task that already exited.
	Delete(ctx context.Context) error
	// Done is closed when the task exits.
	Done() <-chan struct{}
	Resize(ctx context.Context, cols, rows uint32) error
	// IO is the terminal: keystrokes in, output out. The output must be read continuously or
	// the lab's program blocks on a full terminal.
	IO() (stdin io.WriteCloser, stdout io.Reader)
	// CgroupPath is the cgroup v2 directory on the host (/sys/fs/cgroup/labs/<id>).
	CgroupPath() string
}

// ErrNoTask is returned by Attach when the container has no running task.
var ErrNoTask = errors.New("container has no running task")

// killWait bounds how long Kill waits for the task to exit after SIGKILL (plan task 2.3).
const killWait = 5 * time.Second
