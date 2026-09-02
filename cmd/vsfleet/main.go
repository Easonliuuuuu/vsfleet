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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Execute(ctx))
}
