package orch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/errdefs"
)

// LabelKeep marks images labd manages (spec: `labd pull` labels them). containerd's garbage
// collector keeps their content; only Prune removes them.
const LabelKeep = "lab.keep"

// PruneAfter is how long an image no enabled challenge uses is kept, so a rollback never
// needs a network pull (spec "Registry").
const PruneAfter = 14 * 24 * time.Hour

// ImageInfo is one image record in namespace labs.
type ImageInfo struct {
	Name      string
	Labels    map[string]string
	CreatedAt time.Time
}

// ImageStore is the part of containerd's image service that pull and prune use; tests fake it.
type ImageStore interface {
	Get(ctx context.Context, ref string) (ImageInfo, error) // errdefs.ErrNotFound when absent
	Pull(ctx context.Context, ref string) error             // fetch and unpack
	SetLabel(ctx context.Context, ref, key, value string) error
	List(ctx context.Context) ([]ImageInfo, error)
	Delete(ctx context.Context, ref string) error
}

// PullResult is the outcome for one image.
type PullResult struct {
	Image   string `json:"image"`
	Present bool   `json:"present"` // already in the content store; nothing downloaded
	Err     string `json:"error,omitempty"`
}

// PullImages makes every image available: images already present are left alone, missing
// ones are pulled and unpacked. Every image ends up labelled lab.keep=true. Nothing is
// pulled at request time in production (S20); this runs from `labd pull` and on reload.
func PullImages(ctx context.Context, s ImageStore, refs []string) ([]PullResult, error) {
	var out []PullResult
	var errs []error
	for _, ref := range refs {
		res := PullResult{Image: ref}
		img, err := s.Get(ctx, ref)
		switch {
		case err == nil:
			res.Present = true
		case errdefs.IsNotFound(err):
			err = s.Pull(ctx, ref)
		}
		if err == nil && img.Labels[LabelKeep] != "true" {
			err = s.SetLabel(ctx, ref, LabelKeep, "true")
		}
		if err != nil {
			res.Err = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", ref, err))
		}
		out = append(out, res)
	}
	return out, errors.Join(errs...)
}

// PruneResult is one image prune looked at.
type PruneResult struct {
	Image   string    `json:"image"`
	Created time.Time `json:"created"`
	Action  string    `json:"action"` // "removed", "kept: younger than 14 days", "would remove" (dry run), "failed: ..."
}

// Prune removes images labd manages (lab.keep=true) that no enabled challenge references
// and that are older than PruneAfter. Images without the label (labbase, a developer's
// imports) are never touched. dryRun reports without removing.
func Prune(ctx context.Context, s ImageStore, referenced []string, now time.Time, dryRun bool) ([]PruneResult, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	slices.SortFunc(all, func(a, b ImageInfo) int { return compareStrings(a.Name, b.Name) })
	var out []PruneResult
	var errs []error
	for _, img := range all {
		if img.Labels[LabelKeep] != "true" || slices.Contains(referenced, img.Name) {
			continue
		}
		r := PruneResult{Image: img.Name, Created: img.CreatedAt}
		switch {
		case now.Sub(img.CreatedAt) < PruneAfter:
			r.Action = "kept: younger than 14 days"
		case dryRun:
			r.Action = "would remove"
		default:
			if err := s.Delete(ctx, img.Name); err != nil {
				r.Action = "failed: " + err.Error()
				errs = append(errs, fmt.Errorf("%s: %w", img.Name, err))
			} else {
				r.Action = "removed"
			}
		}
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ChallengeImages returns the distinct images of the enabled challenges.
func ChallengeImages(cs []Challenge) []string {
	var out []string
	for _, c := range cs {
		if c.Enabled && c.Image != "" && !slices.Contains(out, c.Image) {
			out = append(out, c.Image)
		}
	}
	slices.Sort(out)
	return out
}

// Pull is PullImages against this runtime's containerd.
func (r *ContainerdRuntime) Pull(ctx context.Context, refs []string) ([]PullResult, error) {
	return PullImages(r.nsctx(ctx), containerdImages{r}, refs)
}

// Prune is Prune against this runtime's containerd.
func (r *ContainerdRuntime) Prune(ctx context.Context, referenced []string, now time.Time, dryRun bool) ([]PruneResult, error) {
	return Prune(r.nsctx(ctx), containerdImages{r}, referenced, now, dryRun)
}

// containerdImages adapts containerd's client to ImageStore (namespace from the context).
type containerdImages struct{ r *ContainerdRuntime }

func (c containerdImages) Get(ctx context.Context, ref string) (ImageInfo, error) {
	img, err := c.r.client.ImageService().Get(ctx, ref)
	if err != nil {
		return ImageInfo{}, err
	}
	return ImageInfo{Name: img.Name, Labels: img.Labels, CreatedAt: img.CreatedAt}, nil
}

func (c containerdImages) Pull(ctx context.Context, ref string) error {
	_, err := c.r.client.Pull(ctx, ref, containerd.WithPullUnpack, containerd.WithPullSnapshotter(snapshotter))
	return err
}

func (c containerdImages) SetLabel(ctx context.Context, ref, key, value string) error {
	is := c.r.client.ImageService()
	img, err := is.Get(ctx, ref)
	if err != nil {
		return err
	}
	if img.Labels == nil {
		img.Labels = map[string]string{}
	}
	img.Labels[key] = value
	_, err = is.Update(ctx, img, "labels."+key)
	return err
}

func (c containerdImages) List(ctx context.Context) ([]ImageInfo, error) {
	imgs, err := c.r.client.ImageService().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ImageInfo, 0, len(imgs))
	for _, img := range imgs {
		out = append(out, ImageInfo{Name: img.Name, Labels: img.Labels, CreatedAt: img.CreatedAt})
	}
	return out, nil
}

// Delete removes the image record synchronously; containerd's garbage collector then frees
// content and snapshots nothing else references.
func (c containerdImages) Delete(ctx context.Context, ref string) error {
	return c.r.client.ImageService().Delete(ctx, ref, images.SynchronousDelete())
}
