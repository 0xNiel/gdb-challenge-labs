// Package manifest reads and lints challenge manifests (challenges/<tier>/<NN-slug>/
// manifest.yaml) against challenges/schema/manifest.schema.json, plus the rules a schema
// cannot state: the slug, tier and order match the directory, and a boss has no hints.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Manifest is the typed view the build tooling uses.
type Manifest struct {
	Slug             string   `yaml:"slug" json:"slug"`
	Title            string   `yaml:"title" json:"title"`
	Tier             int      `yaml:"tier" json:"tier"`
	Order            int      `yaml:"order" json:"order"`
	Difficulty       int      `yaml:"difficulty" json:"difficulty"`
	EstimatedMinutes int      `yaml:"estimated_minutes" json:"estimated_minutes"`
	Image            string   `yaml:"image" json:"image"`
	Limits           Limits   `yaml:"limits" json:"limits"`
	Hints            []Hint   `yaml:"hints" json:"hints"`
	Tags             []string `yaml:"tags" json:"tags"`
	Boss             bool     `yaml:"boss" json:"boss"`
	KeyExpr          string   `yaml:"key_expr" json:"-"`
	KeyValue         uint32   `yaml:"key_value" json:"-"`
	Build            struct {
		Flags string `yaml:"flags"`
		Entry string `yaml:"entry"`
	} `yaml:"build" json:"-"`
}

type Limits struct {
	MemoryMB      int `yaml:"memory_mb" json:"memory_mb"`
	CPUMillicores int `yaml:"cpu_millicores" json:"cpu_millicores"`
	Pids          int `yaml:"pids" json:"pids"`
	TTLMinutes    int `yaml:"ttl_minutes" json:"ttl_minutes"`
	IdleMinutes   int `yaml:"idle_minutes" json:"idle_minutes"`
	ExtendMinutes int `yaml:"extend_minutes" json:"extend_minutes"`
}

type Hint struct {
	Cost int    `yaml:"cost" json:"cost"`
	Text string `yaml:"text" json:"text"`
}

// LocalImage is a dev build's image reference (scripts/challenge-build.sh without --push).
var LocalImage = regexp.MustCompile(`^local/lab-[a-z0-9-]+@sha256:[a-f0-9]{64}$`)

var challengeDir = regexp.MustCompile(`^tier([1-9])-[a-z0-9-]+/([0-9]{2})-([a-z0-9-]+)$`)

// Lint reads the manifest at path and returns every problem (none: valid). allowLocal
// accepts a local/ image reference (dev builds). TEMPLATE directories skip the directory
// rule.
func Lint(path, schemaPath string, allowLocal bool) (Manifest, []string) {
	var m Manifest
	raw, err := os.ReadFile(path)
	if err != nil {
		return m, []string{err.Error()}
	}
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		return m, []string{err.Error()}
	}
	var schema map[string]any
	if err := json.Unmarshal(sb, &schema); err != nil {
		return m, []string{fmt.Sprintf("%s: %v", schemaPath, err)}
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return m, []string{fmt.Sprintf("yaml: %v", err)}
	}
	doc = normalise(doc)
	obj, _ := doc.(map[string]any)
	var errs []string
	if img, _ := obj["image"].(string); allowLocal && LocalImage.MatchString(img) {
		obj["image"] = "" // the schema allows only GHCR digests; a dev build may use local/
	}
	errs = append(errs, validate(schema, doc, "manifest")...)
	if err := yaml.Unmarshal(raw, &m); err != nil {
		errs = append(errs, fmt.Sprintf("yaml: %v", err))
		return m, errs
	}
	if m.Boss && len(m.Hints) > 0 {
		errs = append(errs, "manifest.hints: a boss challenge has no hints")
	}
	if !m.Boss && len(m.Hints) == 0 {
		errs = append(errs, "manifest.hints: only a boss has no hints")
	}
	for i := 1; i < len(m.Hints); i++ {
		if m.Hints[i].Cost < m.Hints[i-1].Cost {
			errs = append(errs, "manifest.hints: costs must not decrease (hints escalate)")
			break
		}
	}
	dir := filepath.Base(filepath.Dir(path))
	if dir != "TEMPLATE" {
		rel := filepath.Base(filepath.Dir(filepath.Dir(path))) + "/" + dir
		d := challengeDir.FindStringSubmatch(rel)
		switch {
		case d == nil:
			errs = append(errs, fmt.Sprintf("directory %s is not tierN-<name>/NN-<slug>", rel))
		default:
			want := "tier" + d[1] + "-" + d[2] + "-" + d[3]
			if m.Slug != want {
				errs = append(errs, fmt.Sprintf("manifest.slug: %q, the directory says %q", m.Slug, want))
			}
			if t, _ := strconv.Atoi(d[1]); m.Tier != t {
				errs = append(errs, fmt.Sprintf("manifest.tier: %d, the directory says %d", m.Tier, t))
			}
			if o, _ := strconv.Atoi(d[2]); m.Order != o {
				errs = append(errs, fmt.Sprintf("manifest.order: %d, the directory says %d", m.Order, o))
			}
		}
	}
	return m, errs
}

// normalise turns yaml.v3's generic values into JSON-like ones (map[string]any, []any).
func normalise(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalise(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalise(e)
		}
		return x
	}
	return v
}
