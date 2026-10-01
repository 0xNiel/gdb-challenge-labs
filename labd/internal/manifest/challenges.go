package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one challenge in challenges.json.
type Entry struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	Tier             int      `json:"tier"`
	Order            int      `json:"order"`
	Difficulty       int      `json:"difficulty"`
	EstimatedMinutes int      `json:"estimated_minutes"`
	Image            string   `json:"image"`
	Limits           Limits   `json:"limits"`
	Hints            []Hint   `json:"hints"`
	Tags             []string `json:"tags"`
	Boss             bool     `json:"boss"`
	Enabled          bool     `json:"enabled"`
	LessonPath       string   `json:"lesson_path"`
	SolutionPath     string   `json:"solution_path"`
}

// Doc is challenges.json.
type Doc struct {
	Version     int     `json:"version"`
	GeneratedAt string  `json:"generated_at"`
	Challenges  []Entry `json:"challenges"`
}

// LocalImage is one entry of .scratch/local-images.json (scripts/challenge-build.sh).
type LocalImage struct {
	Image string `json:"image"`
	Arch  string `json:"arch"`
}

// BuildDoc reads every challenge manifest under root/challenges (not TEMPLATE) and returns
// challenges.json, sorted by tier then order. local, if not nil, supplies dev images by
// slug and wins over the manifest's. A challenge with no image is disabled: labd ignores it
// and web shows it as coming soon.
func BuildDoc(root string, local map[string]LocalImage, now time.Time) (Doc, error) {
	schema := filepath.Join(root, "challenges", "schema", "manifest.schema.json")
	paths, err := filepath.Glob(filepath.Join(root, "challenges", "tier*", "*", "manifest.yaml"))
	if err != nil {
		return Doc{}, err
	}
	doc := Doc{Version: 1, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05Z"), Challenges: []Entry{}}
	var problems []string
	for _, p := range paths {
		m, errs := Lint(p, schema, true)
		if len(errs) > 0 {
			problems = append(problems, fmt.Sprintf("%s: %s", p, strings.Join(errs, "; ")))
			continue
		}
		dir, _ := filepath.Rel(root, filepath.Dir(p))
		e := Entry{
			Slug: m.Slug, Title: m.Title, Tier: m.Tier, Order: m.Order, Difficulty: m.Difficulty,
			EstimatedMinutes: m.EstimatedMinutes, Image: m.Image, Limits: m.Limits, Hints: m.Hints,
			Tags: m.Tags, Boss: m.Boss, LessonPath: filepath.ToSlash(filepath.Join(dir, "lesson.md")),
			SolutionPath: filepath.ToSlash(filepath.Join(dir, "solution.md")),
		}
		if li := local[m.Slug]; li.Image != "" {
			e.Image = li.Image
		} else if strings.HasPrefix(e.Image, "local/") {
			e.Image = "" // a local reference in a manifest belongs to some other host
		}
		if e.Hints == nil {
			e.Hints = []Hint{}
		}
		if e.Tags == nil {
			e.Tags = []string{}
		}
		e.Enabled = e.Image != ""
		doc.Challenges = append(doc.Challenges, e)
	}
	if len(problems) > 0 {
		return Doc{}, fmt.Errorf("invalid manifests:\n%s", strings.Join(problems, "\n"))
	}
	sort.Slice(doc.Challenges, func(i, j int) bool {
		a, b := doc.Challenges[i], doc.Challenges[j]
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		return a.Order < b.Order
	})
	return doc, nil
}

// ValidateDoc checks raw challenges.json against challenges/schema/challenges.schema.json.
func ValidateDoc(raw []byte, schemaPath string) []string {
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		return []string{err.Error()}
	}
	var schema map[string]any
	if err := json.Unmarshal(sb, &schema); err != nil {
		return []string{err.Error()}
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []string{err.Error()}
	}
	return validate(schema, doc, "challenges.json")
}
