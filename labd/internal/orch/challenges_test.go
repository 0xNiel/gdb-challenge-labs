package orch

import (
	"strings"
	"testing"
)

func TestParseChallenges(t *testing.T) {
	t.Parallel()
	cs, err := ParseChallenges([]byte(`{"version":1,"challenges":[
		{"slug":"perf","image":"docker.io/gdblabs/perf:dev","enabled":true,"title":"ignored",
		 "limits":{"memory_mb":256}},
		{"slug":"b","image":"x","enabled":false},
		{"slug":"c","enabled":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	// A disabled challenge may have no image yet (not pushed): labd never starts it.
	if len(cs) != 3 || cs[0].Limits.MemoryMB != 256 || !cs[0].Enabled || cs[1].Enabled || cs[2].Image != "" {
		t.Fatalf("%+v", cs)
	}
	for name, doc := range map[string]string{
		"empty slug":   `{"challenges":[{"slug":"","image":"x"}]}`,
		"no image":     `{"challenges":[{"slug":"a","enabled":true}]}`,
		"duplicate":    `{"challenges":[{"slug":"a","image":"x"},{"slug":"a","image":"y"}]}`,
		"negative":     `{"challenges":[{"slug":"a","image":"x","limits":{"pids":-1}}]}`,
		"not json":     `{`,
		"limits typed": `{"challenges":[{"slug":"a","image":"x","limits":{"pids":"many"}}]}`,
	} {
		if _, err := ParseChallenges([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("%s: empty error", name)
		}
	}
}

func TestChallengeImages(t *testing.T) {
	t.Parallel()
	got := ChallengeImages([]Challenge{
		{Slug: "a", Image: "img2", Enabled: true}, {Slug: "b", Image: "img1", Enabled: true},
		{Slug: "c", Image: "img2", Enabled: true}, {Slug: "d", Image: "off", Enabled: false},
	})
	if strings.Join(got, ",") != "img1,img2" {
		t.Fatalf("%v", got)
	}
}
