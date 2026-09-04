// Command vsfleet-testbed runs the production TUI against a local,
// authenticated govmomi estate. It is a safe integration lab, not a live
// vCenter client: all services bind to loopback and all state is isolated.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/testbed"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
	"github.com/easonliuuuuu/vsfleet/internal/version"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func main() {
	rootDefault := os.Getenv("VSFLEET_TESTBED_ROOT")
	if rootDefault == "" {
		rootDefault = ".vsfleet-testbed"
	}
	root := flag.String("root", rootDefault, "isolated testbed state directory")
	portBase := flag.Int("port-base", 18443, "first loopback port for simulator endpoints")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lab, err := testbed.Start(ctx, testbed.Options{Root: *root, PortBase: *portBase})
	if err != nil {
		fatal(err)
	}
	defer lab.Close(context.Background())

	coord := tui.NewPromptCoordinator()
	keyring := credentials.NewStatic(credentials.SchemeKeyring, map[string]credentials.Credential{})
	resolver := credentials.NewResolver(keyring, coord)
	manager := session.New(resolver)
	manager.ConnectOptions.UserAgent = version.UserAgent()
	defer manager.Close(context.Background())

	cfg, err := config.Load(lab.ConfigPath)
	if err != nil {
		fatal(err)
	}
	store, err := assessment.Open(lab.HistoryPath)
	if err != nil {
		fatal(err)
	}
	defer store.Close()
	service := &assessment.Service{Store: store, Collector: &assessment.Collector{Store: store, Manager: manager}}

	fmt.Fprintf(os.Stderr, "VSFleet connected testbed\n")
	fmt.Fprintf(os.Stderr, "State: %s\nEndpoint catalog: %s\n", lab.Root, lab.ManifestPath)
	fmt.Fprintf(os.Stderr, "Fixture login: %s / %s\nProxy login: %s / %s\n", testbed.FixtureUsername, testbed.FixturePassword, testbed.FixtureProxyUser, testbed.FixtureProxyPassword)
	if manifest, readErr := os.ReadFile(lab.ManifestPath); readErr == nil {
		fmt.Fprintf(os.Stderr, "%s", manifest)
	}
	fmt.Fprintln(os.Stderr, "All listeners are loopback-only; press c to add a catalog context and H for assessment history.")

	backend := tui.NewBackend(cfg, resolver, manager, vsphere.ConnectOptions{Resolver: resolver, UserAgent: version.UserAgent()})
	if _, err := tui.Run(ctx, backend, tui.Options{Current: "prod-vc", Credentials: coord, Assessment: service}); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "vsfleet-testbed: %v\n", err)
	os.Exit(1)
}
