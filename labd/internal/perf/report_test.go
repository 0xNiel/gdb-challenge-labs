package perf

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// specSchema returns the perf-report.json block from the spec, the source of truth.
func specSchema(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../../docs/spec/mvp-spec.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(b), "**Outputs (`perf-report.json`)**")
	if !ok {
		t.Fatal("spec has no perf-report.json section")
	}
	_, rest, _ = strings.Cut(rest, "```json\n")
	block, _, _ := strings.Cut(rest, "```")
	var m map[string]any
	if err := json.Unmarshal([]byte(block), &m); err != nil {
		t.Fatalf("spec JSON: %v", err)
	}
	return m
}

// keys lists every key path, e.g. "per_lab.rss_mb.p50".
func keys(prefix string, v any, out *[]string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, sub := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		*out = append(*out, p)
		keys(p, sub, out)
	}
}

func TestReport_KeySetMatchesSpec(t *testing.T) {
	t.Parallel()
	var want, got []string
	keys("", specSchema(t), &want)
	b, err := json.Marshal(Report{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	keys("", m, &got)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("report keys differ from the spec\n got: %v\nwant: %v", got, want)
	}
}
