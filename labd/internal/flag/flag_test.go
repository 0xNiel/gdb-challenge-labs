package flag

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"
)

func TestDerive_SharedVectors(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../../challenges/schema/flag_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Vectors []struct{ Slug, Secret, Flag string }
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Vectors) != 10 {
		t.Fatalf("%d vectors, want 10", len(doc.Vectors))
	}
	for _, v := range doc.Vectors {
		if got := Derive(v.Secret, v.Slug); got != v.Flag {
			t.Errorf("Derive(%q, %q) = %s, want %s", v.Secret, v.Slug, got, v.Flag)
		}
	}
}

var shape = regexp.MustCompile(`^LAB\{[A-Z2-7]{24}\}$`)

func TestDerive_AlwaysWellFormed(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 2000; i++ {
		secret, slug := randString(rng), randString(rng)
		f := Derive(secret, slug)
		if !shape.MatchString(f) || len(f) != Len {
			t.Fatalf("Derive(%q, %q) = %q", secret, slug, f)
		}
	}
}

func TestSeedMix_StableAndKeyed(t *testing.T) {
	t.Parallel()
	a := SeedMix("s", "tier1-01-off-by-one")
	if a != SeedMix("s", "tier1-01-off-by-one") {
		t.Fatal("SeedMix not deterministic")
	}
	if a == SeedMix("other", "tier1-01-off-by-one") || a == SeedMix("s", "tier1-02-null-deref") {
		t.Fatal("SeedMix ignores the secret or the slug")
	}
}

func randString(rng *rand.Rand) string {
	b := make([]rune, rng.IntN(40))
	for i := range b {
		b[i] = rune(rng.IntN(0x2000))
	}
	return string(b)
}
