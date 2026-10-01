// Command flagblob writes a challenge's flag_blob.h (ADR 0005). The deploy secret comes from
// an environment variable, never from the command line, so it does not land in shell
// history or process listings.
//
//	DEPLOY_SECRET=... flagblob --slug tier1-01-off-by-one --key 345 --out build/flag_blob.h
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	lab "gdblabs/labd/internal/flag"
)

func main() {
	slug := flag.String("slug", "", "challenge slug (manifest slug)")
	key := flag.String("key", "", "the correct runtime key (manifest key_value), decimal or 0x hex, 32 bits")
	out := flag.String("out", "flag_blob.h", "header to write")
	env := flag.String("secret-env", "DEPLOY_SECRET", "environment variable holding the deploy secret")
	printFlag := flag.Bool("print-flag", false, "print the flag for --slug instead (the build's oracle needs it)")
	flag.Parse()
	secret := os.Getenv(*env)
	if *printFlag && *slug != "" && secret != "" {
		fmt.Println(lab.Derive(secret, *slug))
		return
	}
	if *slug == "" || *key == "" || secret == "" {
		fmt.Fprintf(os.Stderr, "flagblob: --slug, --key and $%s are required\n", *env)
		os.Exit(2)
	}
	k, err := strconv.ParseUint(*key, 0, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flagblob: --key: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(*out, []byte(lab.Header(*slug, secret, uint32(k))), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "flagblob:", err)
		os.Exit(1)
	}
}
