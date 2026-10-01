package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const schemaPath = "../../../challenges/schema/manifest.schema.json"

const valid = `slug: tier1-01-off-by-one
title: Off by one
tier: 1
order: 1
difficulty: 1
estimated_minutes: 15
flag_format: "LAB{...}"
image: ""
limits: {memory_mb: 128, cpu_millicores: 500, pids: 32, ttl_minutes: 60, idle_minutes: 15, extend_minutes: 15}
hints:
  - {cost: 0, text: "Start with break main and run."}
  - {cost: 1, text: "Watch i with display i."}
tags: [c, loops]
key_expr: total
key_value: 345
build: {flags: "-O0 -g -fno-stack-protector", entry: score}
`

// write puts content at <tmp>/<dir>/manifest.yaml and returns the path.
func write(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), dir, "manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLint_Valid(t *testing.T) {
	t.Parallel()
	m, errs := Lint(write(t, "tier1-c-fundamentals/01-off-by-one", valid), schemaPath, false)
	if len(errs) != 0 {
		t.Fatalf("valid manifest rejected: %v", errs)
	}
	if m.KeyValue != 345 || m.Build.Entry != "score" || m.Limits.Pids != 32 || len(m.Hints) != 2 {
		t.Errorf("parsed %+v", m)
	}
}

func TestLint_Rejects(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	cases := []struct {
		name, dir, content, want string
		allowLocal               bool
	}{
		{"missing slug", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, "slug: tier1-01-off-by-one\n", "", 1), `missing "slug"`, false},
		{"slug not the directory", "tier1-c-fundamentals/02-null-deref", valid, `the directory says "tier1-02-null-deref"`, false},
		{"image by tag", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, `image: ""`, `image: ghcr.io/org/lab-tier1-01-off-by-one:latest`, 1), "does not match", false},
		{"memory over 1024", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, "memory_mb: 128", "memory_mb: 2048", 1), "2048 is above 1024", false},
		{"hint cost 4", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, "{cost: 1,", "{cost: 4,", 1), "4 is above 3", false},
		{"hint cost negative", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, "{cost: 0,", "{cost: -1,", 1), "-1 is below 0", false},
		{"unknown key", "tier1-c-fundamentals/01-off-by-one", valid + "secret_flag: x\n", `unknown key "secret_flag"`, false},
		{"pids over the sandbox limit", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, "pids: 32", "pids: 64", 1), "64 is above 32", false},
		{"boss with hints", "tier1-c-fundamentals/01-off-by-one", valid + "boss: true\n", "a boss challenge has no hints", false},
		{"tier not the directory", "tier2-optimized-c/01-off-by-one", valid, "the directory says", false},
		{"local image without allow-local", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, `image: ""`, "image: local/lab-tier1-01-off-by-one@sha256:"+digest, 1), "does not match", false},
		{"key_value over 32 bits", "tier1-c-fundamentals/01-off-by-one", strings.Replace(valid, "key_value: 345", "key_value: 4294967296", 1), "above 4294967295", false},
	}
	for _, c := range cases {
		_, errs := Lint(write(t, c.dir, c.content), schemaPath, c.allowLocal)
		if !strings.Contains(strings.Join(errs, "\n"), c.want) {
			t.Errorf("%s: errors %v, want one containing %q", c.name, errs, c.want)
		}
	}
	// A GHCR digest is fine, and a local one with allow-local.
	ok := strings.Replace(valid, `image: ""`, "image: ghcr.io/org/lab-tier1-01-off-by-one@sha256:"+digest, 1)
	if _, errs := Lint(write(t, "tier1-c-fundamentals/01-off-by-one", ok), schemaPath, false); len(errs) != 0 {
		t.Errorf("GHCR digest rejected: %v", errs)
	}
	local := strings.Replace(valid, `image: ""`, "image: local/lab-tier1-01-off-by-one@sha256:"+digest, 1)
	if _, errs := Lint(write(t, "tier1-c-fundamentals/01-off-by-one", local), schemaPath, true); len(errs) != 0 {
		t.Errorf("local image rejected with allow-local: %v", errs)
	}
}

// Every manifest in the repository lints (dev builds may have written local/ images).
func TestLint_RepositoryManifests(t *testing.T) {
	t.Parallel()
	paths, _ := filepath.Glob("../../../challenges/tier*/*/manifest.yaml")
	tmpl, _ := filepath.Glob("../../../challenges/TEMPLATE/manifest.yaml")
	for _, p := range append(paths, tmpl...) {
		if _, errs := Lint(p, schemaPath, true); len(errs) != 0 {
			t.Errorf("%s: %v", p, errs)
		}
	}
}
