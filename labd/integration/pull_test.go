//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPull_PresentIsNoOpAndBogusFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rt := newRuntime(t)

	res, err := rt.Pull(ctx, []string{image})
	if err != nil || len(res) != 1 || !res[0].Present {
		t.Fatalf("pull of an imported image: %+v, %v", res, err)
	}

	bogus := "docker.io/gdblabs/does-not-exist@sha256:" + strings.Repeat("0", 64)
	res, err = rt.Pull(ctx, []string{image, bogus})
	if err == nil {
		t.Fatal("pull of a bogus digest succeeded")
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Fatalf("error does not name the image: %v", err)
	}
	if len(res) != 2 || !res[0].Present || res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("results %+v", res)
	}
	t.Logf("bogus digest error: %s", res[1].Err)
}

// Prune against real containerd, as a dry run 30 days ahead: the referenced image is never
// a candidate, nothing is removed, and images without lab.keep are not even listed.
func TestPrune_DryRunRemovesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rt := newRuntime(t)
	before, err := rt.Prune(ctx, nil, time.Now().Add(30*24*time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rt.Prune(ctx, []string{image}, time.Now().Add(30*24*time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Image == image || r.Action != "would remove" {
			t.Errorf("dry run result %+v", r)
		}
	}
	if len(res) != len(before)-1 && len(res) != len(before) {
		t.Errorf("referencing %s changed the candidates from %d to %d", image, len(before), len(res))
	}
	if pr, err := rt.Pull(ctx, []string{image}); err != nil || !pr[0].Present {
		t.Fatalf("the image is gone after a dry run: %+v, %v", pr, err)
	}
}
