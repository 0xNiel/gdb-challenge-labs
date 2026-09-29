package orch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// fakeRuntime is an in-memory Runtime. Containers are created stopped, Start makes them
// running, Kill or exit() stops them, Delete or Remove forgets them.
type fakeRuntime struct {
	mu         sync.Mutex
	ctrs       map[string]*fakeContainer
	failCreate map[string]error // by challenge slug (label lab.challenge)
	gate       chan struct{}    // if set, Create blocks until it is closed or receives
	creates    int
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{ctrs: map[string]*fakeContainer{}, failCreate: map[string]error{}}
}

// FailCreateFor makes every Create for slug fail.
func (f *fakeRuntime) FailCreateFor(slug string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCreate[slug] = errors.New("injected create failure for " + slug)
}

func (f *fakeRuntime) Create(ctx context.Context, o CreateOpts) (Container, error) {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if err := f.failCreate[o.Labels[LabelChallenge]]; err != nil {
		return nil, err
	}
	if _, dup := f.ctrs[o.ID]; dup {
		return nil, fmt.Errorf("container %s already exists", o.ID)
	}
	c := newFakeContainer(f, o.ID, o.Labels)
	f.ctrs[o.ID] = c
	return c, nil
}

func (f *fakeRuntime) List(context.Context) ([]ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ContainerInfo
	for id, c := range f.ctrs {
		out = append(out, ContainerInfo{ID: id, Labels: c.labels})
	}
	slices.SortFunc(out, func(a, b ContainerInfo) int {
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return out, nil
}

func (f *fakeRuntime) Attach(_ context.Context, id string) (Container, error) {
	f.mu.Lock()
	old := f.ctrs[id]
	f.mu.Unlock()
	if old == nil {
		return nil, fmt.Errorf("container %s not found", id)
	}
	if !old.isRunning() {
		return nil, ErrNoTask
	}
	// A new handle on the same task, with fresh terminal pipes (as after a restart).
	c := newFakeContainer(f, id, old.labels)
	c.running = true
	f.mu.Lock()
	f.ctrs[id] = c
	f.mu.Unlock()
	old.detach()
	return c, nil
}

func (f *fakeRuntime) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	c := f.ctrs[id]
	delete(f.ctrs, id)
	f.mu.Unlock()
	if c != nil {
		c.stop()
	}
	return nil
}

// ids returns the ids of every container that exists.
func (f *fakeRuntime) ids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for id := range f.ctrs {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func (f *fakeRuntime) get(id string) *fakeContainer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctrs[id]
}

// addOrphan puts a container into the runtime as if an earlier labd had made it.
func (f *fakeRuntime) addOrphan(id string, labels map[string]string, running bool) *fakeContainer {
	c := newFakeContainer(f, id, labels)
	c.running = running
	if !running {
		c.stop()
	}
	f.mu.Lock()
	f.ctrs[id] = c
	f.mu.Unlock()
	return c
}

type fakeContainer struct {
	rt      *fakeRuntime
	id      string
	labels  map[string]string
	mu      sync.Mutex
	running bool
	done    chan struct{}
	once    sync.Once
	inR     *io.PipeReader
	inW     *io.PipeWriter
	outR    *io.PipeReader
	outW    *io.PipeWriter
}

func newFakeContainer(rt *fakeRuntime, id string, labels map[string]string) *fakeContainer {
	c := &fakeContainer{rt: rt, id: id, labels: labels, done: make(chan struct{})}
	c.inR, c.inW = io.Pipe()
	c.outR, c.outW = io.Pipe()
	// Echo keystrokes back, like a terminal in cooked mode.
	go func() { _, _ = io.Copy(c.outW, c.inR) }()
	return c
}

func (c *fakeContainer) ID() string { return c.id }

func (c *fakeContainer) Start(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = true
	return nil
}

func (c *fakeContainer) Kill(context.Context) error { c.stop(); return nil }

func (c *fakeContainer) Delete(context.Context) error {
	c.stop()
	c.rt.mu.Lock()
	if c.rt.ctrs[c.id] == c {
		delete(c.rt.ctrs, c.id)
	}
	c.rt.mu.Unlock()
	return nil
}

func (c *fakeContainer) Done() <-chan struct{} { return c.done }

func (c *fakeContainer) Resize(context.Context, uint32, uint32) error { return nil }

func (c *fakeContainer) IO() (io.WriteCloser, io.Reader) { return c.inW, c.outR }

func (c *fakeContainer) CgroupPath() string { return "/nonexistent/labs/" + c.id }

func (c *fakeContainer) isRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// stop is the task exiting: Done closes, the terminal reaches EOF.
func (c *fakeContainer) stop() {
	c.once.Do(func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		close(c.done)
		_ = c.inW.Close()
		_ = c.outW.Close()
	})
}

// detach closes this handle's pipes without stopping the task (another handle took over).
func (c *fakeContainer) detach() {
	_ = c.inW.Close()
	_ = c.outW.Close()
}
