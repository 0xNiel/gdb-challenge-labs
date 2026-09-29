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
