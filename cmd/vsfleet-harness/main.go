// Command vsfleet-harness exposes the repository-owned deterministic TUI
// scenario catalogue. It is development tooling, not an end-user command.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/testbed/scenarios"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "list":
		list()
	case "test":
		test(os.Args[2:])
	case "sandbox":
		sandbox(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: vsfleet-harness list|test|sandbox")
	os.Exit(2)
}

func list() {
	for _, definition := range scenarios.Definitions() {
		fmt.Printf("%-24s %-13s %s\n", definition.Name, definition.Profile, definition.Purpose)
	}
}

func test(args []string) {
	// Goldens are byte-compared, and History and the datastore browser print
	// timestamps through time.Local. Without this the bytes depend on the
	// timezone of whoever ran --update-goldens, so a golden written outside
	// UTC never matches the one CI checks.
	time.Local = time.UTC

	flags := flag.NewFlagSet("test", flag.ExitOnError)
	profile := flags.String("profile", "", "presentation or connected")
	results := flags.String("results-dir", "", "directory for failure diagnostics")
	update := flags.Bool("update-goldens", false, "rewrite render goldens")
	scenario := flags.String("scenario", "", "one scenario; empty runs all")
	// Accept scenario names before or after options. The standard flag package
	// stops at the first positional argument, which made `test overview
	// --update-goldens` surprisingly different from the documented spelling.
	var flagArgs, names []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--profile", "--results-dir", "--scenario":
			flagArgs = append(flagArgs, arg)
			if i+1 >= len(args) {
				flagArgs = append(flagArgs, "")
			} else {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		case "--update-goldens":
			flagArgs = append(flagArgs, arg)
		default:
			names = append(names, arg)
		}
	}
	_ = flags.Parse(flagArgs)

	names = append(names, flags.Args()...)
	if *scenario != "" {
		names = append(names, *scenario)
	}
	ctx, cancel := context.WithTimeout(context.Background(), scenarios.ScenarioTimeout)
	defer cancel()
	if err := scenarios.RunAll(ctx, names, scenarios.RunOptions{Profile: *profile, ResultsDir: *results, UpdateGoldens: *update, SkipGoldens: runtime.GOOS != "linux"}, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "vsfleet-harness: %v\n", err)
		os.Exit(1)
	}
}

func sandbox(args []string) {
	flags := flag.NewFlagSet("sandbox", flag.ExitOnError)
	profile := flags.String("profile", "presentation", "presentation or connected")
	name := flags.String("scenario", "", "scenario name")
	_ = flags.Parse(args)
	if *name == "" && flags.NArg() > 0 {
		*name = flags.Arg(0)
	}
	definition, err := scenarios.Lookup(*name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("Sandbox prepared for %s (%s): %s\n", definition.Name, *profile, definition.Purpose)
	fmt.Println("Use scripts/testbed launch to take control of the real TUI.")
	// Keep this command non-interactive and safe for CI. The shell wrapper uses
	// the same scenario name to launch the selected presentation/connected
	// binary in a real terminal.
}
