package testbed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestStartCreatesAuthenticatedRoutesAndHistory(t *testing.T) {
	ctx := context.Background()
	lab, err := Start(ctx, Options{Root: filepath.Join(t.TempDir(), "state"), PortBase: 28443})
	if err != nil {
		t.Fatal(err)
	}
	defer lab.Close(ctx)

	if len(lab.contexts) != 4 {
		t.Fatalf("configured contexts = %d, want 4", len(lab.contexts))
	}
	manifest, err := os.ReadFile(lab.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), FixturePassword) || strings.Contains(string(manifest), FixtureProxyPassword) {
		t.Fatal("endpoint manifest persisted a fixture password")
	}
	store, err := assessment.Open(lab.HistoryPath)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := store.Runs(ctx)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 4 {
		t.Fatalf("seeded runs = %d, want 4", len(runs))
	}

	for _, name := range []string{"prod-vc", "edge-vc", "branch-vc"} {
		cc, err := config.Load(lab.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		contextConfig, err := cc.Context(name)
		if err != nil {
			t.Fatal(err)
		}
		opts := vsphere.ConnectOptions{Credential: &credentials.Credential{Username: FixtureUsername, Password: FixturePassword}}
		if name != "prod-vc" {
			opts.ProxyCredential = &credentials.Credential{Username: FixtureProxyUser, Password: FixtureProxyPassword}
		}
		client, err := vsphere.Connect(ctx, contextConfig, opts)
		if err != nil {
			t.Fatalf("connect %s: %v", name, err)
		}
		idx, err := client.NewIndex(ctx)
		if err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
		part := client.FetchGroup(ctx, idx, vsphere.GroupVMs)
		_ = client.Close(ctx)
		if len(part.VMs) == 0 {
			t.Fatalf("%s returned no VMs", name)
		}
	}
}

func TestAuthenticationRejectsWrongFixtureCredentials(t *testing.T) {
	ctx := context.Background()
	lab, err := Start(ctx, Options{Root: filepath.Join(t.TempDir(), "state"), PortBase: 29443})
	if err != nil {
		t.Fatal(err)
	}
	defer lab.Close(ctx)

	cfg, err := config.Load(lab.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	prod, err := cfg.Context("prod-vc")
	if err != nil {
		t.Fatal(err)
	}
	_, err = vsphere.Connect(ctx, prod, vsphere.ConnectOptions{Credential: &credentials.Credential{Username: FixtureUsername, Password: "wrong-password"}})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "authenticate") {
		t.Fatalf("wrong vCenter password error = %v, want authentication failure", err)
	}

	edge, err := cfg.Context("edge-vc")
	if err != nil {
		t.Fatal(err)
	}
	_, err = vsphere.Connect(ctx, edge, vsphere.ConnectOptions{
		Credential:      &credentials.Credential{Username: FixtureUsername, Password: FixturePassword},
		ProxyCredential: &credentials.Credential{Username: FixtureProxyUser, Password: "wrong-proxy"},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "proxy") {
		t.Fatalf("wrong proxy password error = %v, want proxy failure", err)
	}
}
