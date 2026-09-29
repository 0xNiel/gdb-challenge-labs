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
		{"slug":"b","image":"x","enabled":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Limits.MemoryMB != 256 || !cs[0].Enabled || cs[1].Enabled {
		t.Fatalf("%+v", cs)
	}
	for name, doc := range map[string]string{
		"empty slug":   `{"challenges":[{"slug":"","image":"x"}]}`,
		"no image":     `{"challenges":[{"slug":"a"}]}`,
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
