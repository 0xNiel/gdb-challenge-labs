// Command manifestlint checks challenge manifests against challenges/schema/
// manifest.schema.json and the directory layout (Phase 5, task 5.3).
//
//	manifestlint [--schema PATH] [--allow-local] challenges/tier1-*/*/manifest.yaml
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"gdblabs/labd/internal/manifest"
)

func main() {
	schema := flag.String("schema", "challenges/schema/manifest.schema.json", "the manifest schema")
	allowLocal := flag.Bool("allow-local", os.Getenv("ALLOW_LOCAL_IMAGES") == "1", "accept local/ image references (dev builds)")
	asJSON := flag.Bool("json", false, "for one valid manifest, print the fields the build uses as JSON")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: manifestlint [--schema PATH] [--allow-local] MANIFEST...")
		os.Exit(2)
	}
	bad := 0
	for _, p := range flag.Args() {
		m, errs := manifest.Lint(p, *schema, *allowLocal)
		if *asJSON && len(errs) == 0 {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"slug": m.Slug, "image": m.Image, "entry": m.Build.Entry, "flags": m.Build.Flags,
				"key_value": m.KeyValue, "tier": m.Tier, "order": m.Order,
			})
			continue
		}
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "%s: %s\n", p, e)
		}
		if len(errs) > 0 {
			bad++
		} else {
			fmt.Printf("ok  %s\n", p)
		}
	}
	if bad > 0 {
		os.Exit(1)
	}
}
