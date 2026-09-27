// Command labd-perf is the load driver for the perf suite (spec "Local performance test
// suite"). Phase 0 ships only --version; the run, report and profiles subcommands arrive
// in Phase 4.
package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fs := flag.NewFlagSet("labd-perf", flag.ExitOnError)
	showVersion := fs.Bool("version", false, "print version and exit")
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("labd-perf", version)
		return
	}
	fmt.Fprintln(os.Stderr, "labd-perf: no subcommands yet (Phase 4). Use --version.")
	os.Exit(2)
}
