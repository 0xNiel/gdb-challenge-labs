package orch

import (
	"context"
	"errors"
	"fmt"
	"slices"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"
)

// LabelKeep marks images labd must not prune (spec: `labd pull` labels them).
const LabelKeep = "lab.keep"

// PullResult is the outcome for one image.
type PullResult struct {
	Image   string `json:"image"`
	Present bool   `json:"present"` // already in the content store; nothing downloaded
	Err     string `json:"error,omitempty"`
}

// Pull makes every image available in namespace labs: images already present are left alone,
// missing ones are pulled and unpacked. Every image ends up labelled lab.keep=true. Nothing
// is pulled at request time in production (S20); this runs from `labd pull` and on reload.
func (r *ContainerdRuntime) Pull(ctx context.Context, images []string) ([]PullResult, error) {
	ctx = r.nsctx(ctx)
	var out []PullResult
	var errs []error
	for _, ref := range images {
		res := PullResult{Image: ref}
		_, err := r.client.GetImage(ctx, ref)
		switch {
		case err == nil:
			res.Present = true
		case errdefs.IsNotFound(err):
			_, err = r.client.Pull(ctx, ref, containerd.WithPullUnpack, containerd.WithPullSnapshotter(snapshotter))
		}
		if err == nil {
			err = r.keep(ctx, ref)
		}
		if err != nil {
			res.Err = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", ref, err))
		}
		out = append(out, res)
	}
	return out, errors.Join(errs...)
}

func (r *ContainerdRuntime) keep(ctx context.Context, ref string) error {
	is := r.client.ImageService()
	img, err := is.Get(ctx, ref)
	if err != nil {
		return err
	}
	if img.Labels[LabelKeep] == "true" {
		return nil
	}
	if img.Labels == nil {
		img.Labels = map[string]string{}
	}
	img.Labels[LabelKeep] = "true"
	_, err = is.Update(ctx, img, "labels."+LabelKeep)
	return err
}

// Unreferenced lists images in namespace labs that no challenge in images refers to. Phase 5
// adds the retention rule (older than 14 days) and the deletion; for now `labd prune` lists.
func (r *ContainerdRuntime) Unreferenced(ctx context.Context, images []string) ([]string, error) {
	ctx = r.nsctx(ctx)
	all, err := r.client.ImageService().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	var out []string
	for _, img := range all {
		if !slices.Contains(images, img.Name) {
			out = append(out, img.Name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// ChallengeImages returns the distinct images of the enabled challenges.
func ChallengeImages(cs []Challenge) []string {
	var out []string
	for _, c := range cs {
		if c.Enabled && !slices.Contains(out, c.Image) {
			out = append(out, c.Image)
		}
	}
	slices.Sort(out)
	return out
}
