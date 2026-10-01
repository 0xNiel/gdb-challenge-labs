package orch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// LoadChallenges reads challenges.json: {"challenges": [{slug, image, limits, enabled}]}.
// Phase 5 generates it; unknown fields (title, tier, hints, ...) are for web and ignored. A
// disabled challenge may have no image yet (not pushed): labd never starts it.
func LoadChallenges(path string) ([]Challenge, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read challenges: %w", err)
	}
	return ParseChallenges(raw)
}

// ParseChallenges is LoadChallenges without the file read.
func ParseChallenges(raw []byte) ([]Challenge, error) {
	var doc struct {
		Challenges []Challenge `json:"challenges"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse challenges: %w", err)
	}
	var errs []error
	seen := map[string]bool{}
	for i, c := range doc.Challenges {
		switch {
		case c.Slug == "":
			errs = append(errs, fmt.Errorf("challenges[%d]: slug is empty", i))
		case seen[c.Slug]:
			errs = append(errs, fmt.Errorf("challenges[%d]: duplicate slug %q", i, c.Slug))
		case c.Image == "" && c.Enabled:
			errs = append(errs, fmt.Errorf("challenge %q: image is empty", c.Slug))
		}
		l := c.Limits
		if l.MemoryMB < 0 || l.CPUMillicores < 0 || l.Pids < 0 || l.TTLMinutes < 0 || l.IdleMinutes < 0 || l.ExtendMinutes < 0 {
			errs = append(errs, fmt.Errorf("challenge %q: negative limit", c.Slug))
		}
		seen[c.Slug] = true
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return doc.Challenges, nil
}
