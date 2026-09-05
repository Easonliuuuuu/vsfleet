// Command vsfleet-demo runs the TUI against deterministic sample data for
// screenshots and presentations. The released vsfleet binary reaches the same
// fixtures through "vsfleet demo"; this command exists so a recording can be
// driven without building the whole command tree.
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	_, err := tui.Run(ctx, demo.NewBackend(), tui.Options{Current: "prod-vc", Demo: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "vsfleet-demo: %v\n", err)
		os.Exit(1)
	}
}
