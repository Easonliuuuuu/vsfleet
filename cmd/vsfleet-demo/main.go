// Command vsfleet-demo runs the TUI against deterministic sample data. It is
// the launcher for the presentation profile of scripts/testbed. The released
// vsfleet binary reaches the same fixtures through "vsfleet demo".
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/easonliuuuuu/vsfleet/internal/demo"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
)

func main() {
	// Ctrl-C belongs to Bubble Tea or an interactive handoff child; only
	// SIGTERM should cancel the outer application context.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	backend, opts, cleanup, err := demo.Launch(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vsfleet-demo: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()
	if scenario := os.Getenv("VSFLEET_TESTBED_SCENARIO"); scenario != "" {
		fmt.Fprintf(os.Stderr, "Scenario: %s\n", scenario)
	}

	_, err = tui.Run(ctx, backend, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vsfleet-demo: %v\n", err)
		os.Exit(1)
	}
}
