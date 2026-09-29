//go:build integration

// Integration tests against the real containerd on the lab host (./run.sh test --integration).
// They run as root (go test -exec sudo) because containerd's snapshots and FIFOs need it.
package integration

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	socket    = env("LABD_TEST_SOCKET", "/run/containerd/containerd.sock")
	runtimeID = env("LABD_TEST_RUNTIME", orch.RuntimeRunsc)
	image     = env("LABD_TEST_IMAGE", "docker.io/gdblabs/perf:dev")
)

func baseSpec(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../sandbox/sandbox-base.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newRuntime(t *testing.T) *orch.ContainerdRuntime {
	t.Helper()
	rt, err := orch.NewContainerdRuntime(socket, runtimeID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close() })
	return rt
}

func TestRuntime_CreateStartKillDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rt := newRuntime(t)
	id := fmt.Sprintf("it-runtime-%d", time.Now().UnixNano())
	spec, err := orch.BuildSpec(baseSpec(t), orch.SpecParams{
		ID: id, Limits: config.Default().DefaultLimits, Runtime: runtimeID,
		Annotations: map[string]string{"lab.session_id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	c, err := rt.Create(ctx, orch.CreateOpts{ID: id, Image: image, Spec: spec, Labels: map[string]string{"lab.session_id": id}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Remove(context.Background(), id) })
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("create+start %v", time.Since(t0))

	// The terminal works both ways: a command typed in comes back with its output.
	stdin, stdout := c.IO()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- strings.TrimRight(sc.Text(), "\r")
		}
		close(lines)
	}()
	if _, err := fmt.Fprintf(stdin, "echo P2-$((40+2)); id -u\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(20 * time.Second)
	for found := false; !found; {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("terminal closed before the echo arrived")
			}
			found = strings.HasPrefix(l, "P2-42")
		case <-deadline:
			t.Fatal("no echo from the terminal in 20 s")
		}
	}

	infos, err := rt.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, in := range infos {
		if in.ID == id && in.Labels["lab.session_id"] == id {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("List does not show %s with its label", id)
	}
	if _, err := os.Stat(c.CgroupPath() + "/memory.current"); err != nil {
		t.Fatalf("cgroup %s: %v", c.CgroupPath(), err)
	}

	if err := c.Kill(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("Done not closed after Kill")
	}
	if err := c.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	infos, err = rt.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range infos {
		if in.ID == id {
			t.Fatalf("%s still listed after Delete", id)
		}
	}
}
