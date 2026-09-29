//go:build integration

package integration

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
)

// helperEnv makes this test binary act as a labd that creates one lab and dies without any
// cleanup, the way a kill -9 would leave it.
const helperEnv = "LABD_IT_CREATE_AND_DIE"

func TestMain(m *testing.M) {
	if id := os.Getenv(helperEnv); id != "" {
		os.Exit(createAndDie(id))
	}
	os.Exit(m.Run())
}

func createAndDie(id string) int {
	ctx := context.Background()
	rt, err := orch.NewContainerdRuntime(socket, runtimeID, os.Getenv("LABD_IT_FIFO_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	base, err := os.ReadFile("../sandbox/sandbox-base.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	spec, err := orch.BuildSpec(base, orch.SpecParams{ID: id, Limits: config.Default().DefaultLimits, Runtime: runtimeID})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	c, err := rt.Create(ctx, orch.CreateOpts{ID: id, Image: image, Spec: spec, Labels: map[string]string{"lab.session_id": id}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := c.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0 // exit with the terminal FIFOs still open in this process: they die with it
}

// TestRuntime_AttachAfterCreatorDied is what adoption needs: a new labd re-opens the
// terminal of a lab whose creator died, and keystrokes and output flow again.
func TestRuntime_AttachAfterCreatorDied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := fmt.Sprintf("it-attach-%d", time.Now().UnixNano())
	fifos := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"="+id, "LABD_IT_FIFO_DIR="+fifos)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	rt := newRuntime(t)
	t.Cleanup(func() { _ = rt.Remove(context.Background(), id) })

	c, err := rt.Attach(ctx, id)
	if err != nil {
		t.Fatalf("attach after the creator died: %v", err)
	}
	stdin, stdout := c.IO()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- strings.TrimRight(sc.Text(), "\r")
		}
		close(lines)
	}()
	if _, err := fmt.Fprintf(stdin, "echo P2-ATTACH-$((40+2))\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("terminal closed before the echo arrived")
			}
			if strings.HasPrefix(l, "P2-ATTACH-42") {
				if err := c.Kill(ctx); err != nil {
					t.Fatal(err)
				}
				if err := c.Delete(ctx); err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-deadline:
			t.Fatal("no output through the re-attached terminal in 20 s")
		}
	}
}
