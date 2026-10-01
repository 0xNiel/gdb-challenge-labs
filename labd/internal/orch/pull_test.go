package orch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// fakeImages is an in-memory ImageStore. pullable lists refs a registry would serve.
type fakeImages struct {
	mu       sync.Mutex
	imgs     map[string]ImageInfo
	pullable map[string]bool
	pulled   []string
	deleted  []string
	now      time.Time
}

func (f *fakeImages) Get(_ context.Context, ref string) (ImageInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	img, ok := f.imgs[ref]
	if !ok {
		return ImageInfo{}, fmt.Errorf("image %q: %w", ref, errdefs.ErrNotFound)
	}
	return img, nil
}

func (f *fakeImages) Pull(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.pullable[ref] {
		return errors.New("registry: not found")
	}
	f.pulled = append(f.pulled, ref)
	f.imgs[ref] = ImageInfo{Name: ref, Labels: map[string]string{}, CreatedAt: f.now}
	return nil
}

func (f *fakeImages) SetLabel(_ context.Context, ref, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	img := f.imgs[ref]
	labels := map[string]string{}
	for k, v := range img.Labels {
		labels[k] = v
	}
	labels[key] = value
	img.Labels = labels
	f.imgs[ref] = img
	return nil
}

func (f *fakeImages) List(context.Context) ([]ImageInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ImageInfo
	for _, img := range f.imgs {
		out = append(out, img)
	}
	return out, nil
}

func (f *fakeImages) Delete(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.imgs, ref)
	f.deleted = append(f.deleted, ref)
	return nil
}

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestPullImages_SkipsPresentPullsMissingLabelsAll(t *testing.T) {
	t.Parallel()
	f := &fakeImages{now: t0, imgs: map[string]ImageInfo{
		"present": {Name: "present", Labels: map[string]string{}, CreatedAt: t0},
		"kept":    {Name: "kept", Labels: map[string]string{LabelKeep: "true"}, CreatedAt: t0},
	}, pullable: map[string]bool{"missing": true}}
	res, err := PullImages(context.Background(), f, []string{"present", "kept", "missing", "nowhere"})
	if err == nil {
		t.Fatal("an unpullable image did not fail the pull")
	}
	got := map[string]PullResult{}
	for _, r := range res {
		got[r.Image] = r
	}
	if !got["present"].Present || !got["kept"].Present || got["missing"].Present || got["nowhere"].Err == "" {
		t.Errorf("results %+v", res)
	}
	if len(f.pulled) != 1 || f.pulled[0] != "missing" {
		t.Errorf("pulled %v, want only the missing one", f.pulled)
	}
	for _, ref := range []string{"present", "kept", "missing"} {
		if f.imgs[ref].Labels[LabelKeep] != "true" {
			t.Errorf("%s not labelled %s", ref, LabelKeep)
		}
	}
}

func TestPrune_RetentionRules(t *testing.T) {
	t.Parallel()
	keep := map[string]string{LabelKeep: "true"}
	old, young := t0.Add(-15*24*time.Hour), t0.Add(-13*24*time.Hour)
	newStore := func() *fakeImages {
		return &fakeImages{now: t0, imgs: map[string]ImageInfo{
			"referenced-old":   {Name: "referenced-old", Labels: keep, CreatedAt: old},
			"unused-young":     {Name: "unused-young", Labels: keep, CreatedAt: young},
			"unused-old":       {Name: "unused-old", Labels: keep, CreatedAt: old},
			"unmanaged-old":    {Name: "unmanaged-old", Labels: map[string]string{}, CreatedAt: old},
			"labbase-no-label": {Name: "labbase-no-label", CreatedAt: old},
		}}
	}
	f := newStore()
	res, err := Prune(context.Background(), f, []string{"referenced-old"}, t0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "unused-old" {
		t.Fatalf("deleted %v, want only unused-old", f.deleted)
	}
	actions := map[string]string{}
	for _, r := range res {
		actions[r.Image] = r.Action
	}
	if actions["unused-young"] != "kept: younger than 14 days" || actions["unused-old"] != "removed" || len(actions) != 2 {
		t.Errorf("actions %v", actions)
	}
	// Dry run reports the same and removes nothing.
	f = newStore()
	res, _ = Prune(context.Background(), f, []string{"referenced-old"}, t0, true)
	if len(f.deleted) != 0 || len(res) != 2 || res[0].Action != "would remove" {
		t.Errorf("dry run: deleted %v, results %+v", f.deleted, res)
	}
}

func TestChallengeImages_SkipsDisabledAndEmpty(t *testing.T) {
	t.Parallel()
	got := ChallengeImages([]Challenge{{Slug: "a", Image: "x", Enabled: true}, {Slug: "b", Enabled: false}, {Slug: "c", Image: "y"}})
	if len(got) != 1 || got[0] != "x" {
		t.Fatalf("%v", got)
	}
}

// ADR 0018: the admin banner counts enabled challenges whose image is not present.
func TestMissingImages(t *testing.T) {
	t.Parallel()
	f := &fakeImages{imgs: map[string]ImageInfo{"a@sha256:1": {Name: "a@sha256:1"}}}
	got := MissingImages(context.Background(), f, ChallengeImages([]Challenge{
		{Slug: "a", Image: "a@sha256:1", Enabled: true},
		{Slug: "b", Image: "b@sha256:2", Enabled: true},
		{Slug: "c", Image: "c@sha256:3", Enabled: false}, // disabled: not needed
	}))
	if len(got) != 1 || got[0] != "b@sha256:2" {
		t.Fatalf("missing %v, want [b@sha256:2]", got)
	}
}
