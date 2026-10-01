// Command challengesjson writes challenges.json from the challenge manifests (Phase 5, task
// 5.11); scripts/challenges-json.sh runs it.
//
//	challengesjson [--root .] [--local .scratch/local-images.json] > challenges.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gdblabs/labd/internal/manifest"
)

func main() {
	root := flag.String("root", ".", "repository root")
	local := flag.String("local", "", "dev images by slug (.scratch/local-images.json from challenge-build.sh)")
	flag.Parse()
	var imgs map[string]manifest.LocalImage
	if *local != "" {
		b, err := os.ReadFile(*local)
		if err != nil {
			fail(err)
		}
		if err := json.Unmarshal(b, &imgs); err != nil {
			fail(fmt.Errorf("%s: %w", *local, err))
		}
	}
	doc, err := manifest.BuildDoc(*root, imgs, time.Now())
	if err != nil {
		fail(err)
	}
	out, _ := json.MarshalIndent(doc, "", "  ")
	out = append(out, '\n')
	if errs := manifest.ValidateDoc(out, filepath.Join(*root, "challenges", "schema", "challenges.schema.json")); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "challengesjson:", e)
		}
		os.Exit(1)
	}
	os.Stdout.Write(out)
	n := 0
	for _, c := range doc.Challenges {
		if c.Enabled {
			n++
		}
	}
	fmt.Fprintf(os.Stderr, "challengesjson: %d challenges, %d enabled (an image is required to enable one)\n", len(doc.Challenges), n)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "challengesjson:", err)
	os.Exit(1)
}
