// Command vsfleet operates several vCenters from one terminal.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/easonliuuuuu/vsfleet/internal/cli"
)

func main() {
	// Bubble Tea owns Ctrl-C while the TUI is active. During an SSH handoff
	// the child must own it, so do not cancel the application's context on
	// SIGINT; SIGTERM remains the process-level shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Execute(ctx))
}
