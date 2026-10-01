//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
)

// labImage is a challenge image on this host: $LABD_TEST_LAB_IMAGE, else the first entry in
// .scratch/local-images.json (written by scripts/challenge-build.sh). "" if there is none.
func labImage() string {
	if v := os.Getenv("LABD_TEST_LAB_IMAGE"); v != "" {
		return v
	}
	b, err := os.ReadFile("../../.scratch/local-images.json")
	if err != nil {
		return ""
	}
	var m map[string]struct{ Image string }
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	for slug, e := range m {
		if strings.HasPrefix(slug, "tier1-") && e.Image != "" {
			return e.Image
		}
	}
	return ""
}

// TestCwd_FromImageWorkdir starts a lab the way the session manager does and checks where its
// shell starts (ADR 0014): the image's WORKDIR, /opt/lab for a lab and /home/lab for perf.
func TestCwd_FromImageWorkdir(t *testing.T) {
	cases := []struct{ name, image, want string }{
		{"perf", image, "/home/lab"},
		{"lab", labImage(), "/opt/lab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.image == "" {
				t.Skip("no lab image on this host: build one with scripts/challenge-build.sh or set LABD_TEST_LAB_IMAGE")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			rt := newRuntime(t)
			cwd, err := rt.ImageWorkingDir(ctx, tc.image)
			if err != nil {
				t.Fatal(err)
			}
			if cwd != tc.want {
				t.Fatalf("ImageWorkingDir(%s) = %q, want %q", tc.image, cwd, tc.want)
			}
			id := fmt.Sprintf("it-cwd-%s-%d", tc.name, time.Now().UnixNano())
			spec, err := orch.BuildSpec(baseSpec(t), orch.SpecParams{
				ID: id, Cwd: cwd, Limits: config.Default().DefaultLimits, Runtime: runtimeID,
			})
			if err != nil {
				t.Fatal(err)
			}
			c, err := rt.Create(ctx, orch.CreateOpts{ID: id, Image: tc.image, Spec: spec, Labels: map[string]string{"lab.session_id": id}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = rt.Remove(context.Background(), id) })
			if err := c.Start(ctx); err != nil {
				t.Fatal(err)
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
			// The marker is built by the shell so the echoed command line never matches.
			if _, err := fmt.Fprintf(stdin, "echo CWD-$((1+1)) \"$(pwd)\" \"$HOME\"\n"); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(20 * time.Second)
			for {
				select {
				case l, ok := <-lines:
					if !ok {
						t.Fatal("terminal closed before pwd arrived")
					}
					if !strings.HasPrefix(l, "CWD-2 ") {
						continue
					}
					if got, want := l, "CWD-2 "+tc.want+" /home/lab"; got != want {
						t.Fatalf("got %q, want %q (cwd, then HOME)", got, want)
					}
					return
				case <-deadline:
					t.Fatal("no pwd from the terminal in 20 s")
				}
			}
		})
	}
}
