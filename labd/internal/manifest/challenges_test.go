package manifest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/orch"
)

const root = "../../.."

// The repository's challenges.json is what the generator produces: five tier-1
// challenges in order, valid against the schema, and labd can load it.
func TestBuildDoc_Repository(t *testing.T) {
	t.Parallel()
	local := map[string]LocalImage{"tier1-02-null-deref": {Image: "local/lab-tier1-02-null-deref@sha256:" + strings.Repeat("b", 64), Arch: "amd64"}}
	for _, l := range []map[string]LocalImage{nil, local} {
		doc, err := BuildDoc(root, l, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, c := range doc.Challenges {
			slugs = append(slugs, c.Slug)
			if c.Enabled != (c.Image != "") {
				t.Errorf("%s: enabled %v with image %q", c.Slug, c.Enabled, c.Image)
			}
			if !strings.HasSuffix(c.LessonPath, "/lesson.md") || !strings.HasPrefix(c.LessonPath, "challenges/tier") {
				t.Errorf("%s: lesson_path %q", c.Slug, c.LessonPath)
			}
		}
		want := "tier1-01-off-by-one tier1-02-null-deref tier1-03-uninitialized tier1-04-unterminated tier1-05-stack-overwrite"
		if got := strings.Join(slugs, " "); got != want {
			t.Fatalf("slugs %s", got)
		}
		raw, _ := json.Marshal(doc)
		if errs := ValidateDoc(raw, root+"/challenges/schema/challenges.schema.json"); len(errs) > 0 {
			t.Fatalf("schema: %v", errs)
		}
		cs, err := orch.ParseChallenges(raw)
		if err != nil || len(cs) != 5 {
			t.Fatalf("labd cannot load it: %d, %v", len(cs), err)
		}
		if l != nil && (!doc.Challenges[1].Enabled || doc.Challenges[1].Image != local["tier1-02-null-deref"].Image) {
			t.Errorf("local image not used: %+v", doc.Challenges[1])
		}
		if !doc.Challenges[4].Boss || len(doc.Challenges[4].Hints) != 0 || len(doc.Challenges[0].Hints) != 3 {
			t.Errorf("boss and hints: %+v / %+v", doc.Challenges[4], doc.Challenges[0])
		}
	}
}

func TestValidateDoc_Rejects(t *testing.T) {
	t.Parallel()
	bad := `{"version":1,"generated_at":"2026-10-01T00:00:00Z","challenges":[{"slug":"tier1-01-x","image":"docker.io/x:latest"}]}`
	errs := strings.Join(ValidateDoc([]byte(bad), root+"/challenges/schema/challenges.schema.json"), "\n")
	for _, want := range []string{`missing "title"`, "does not match"} {
		if !strings.Contains(errs, want) {
			t.Errorf("errors %q lack %q", errs, want)
		}
	}
}
