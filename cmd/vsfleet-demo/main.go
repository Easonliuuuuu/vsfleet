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

	backend, opts, cleanup, err := setupDemo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vsfleet-demo: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	_, err = tui.Run(ctx, backend, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vsfleet-demo: %v\n", err)
		os.Exit(1)
	}
}

// setupDemo builds the synthetic backend, the seeded in-memory assessment
// service, and the TUI options for the presentation binary. It mirrors the
// "vsfleet demo" wiring in internal/cli/demo.go so both launch paths expose
// the same History capability. The returned cleanup closes the store and
// must run even when tui.Run fails.
func setupDemo() (*demo.Backend, tui.Options, func(), error) {
	backend := demo.NewBackend()
	service, closeHistory, err := backend.AssessmentService()
	if err != nil {
		return nil, tui.Options{}, nil, err
	}
	opts := tui.Options{Current: "prod-vc", Demo: true, Assessment: service}
	return backend, opts, closeHistory, nil
}
