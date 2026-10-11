package uistate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadOfMissingFileReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	got := Load(path)
	if !reflect.DeepEqual(got, State{}) {
		t.Errorf("Load of a missing file returned %+v, want the zero value", got)
	}
}

func TestSaveOverwritesThePreviousState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	if err := Save(path, State{Context: "prod", Kind: "vm"}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := Save(path, State{Context: "lab", Kind: "datastore", Sort: "status"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	got := Load(path)
	want := State{Context: "lab", Kind: "datastore", Sort: "status"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}
}

func TestLoadOfCorruptFileReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got := Load(path)
	if !reflect.DeepEqual(got, State{}) {
		t.Errorf("Load of a corrupt file returned %+v, want the zero value — a bad state file must never be fatal", got)
	}
}

// TestSaveThenLoadRoundTrips saves through an empty path, the way the CLI
// calls this package, so the file lands at VSFLEET_STATE inside a directory
// Save has to create, and every remembered field must come back.
func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Setenv(EnvStatePath, filepath.Join(t.TempDir(), "vsfleet", "state.json"))

	want := State{
		Context: "customer-a", Kind: "network", Sort: "status", WelcomedVersion: "0.10.0",
		SSHUsers:         map[string]string{"prod/vm-1": "tdclab", "lab/host-2": "root"},
		SSHIdentityFiles: map[string]string{"prod/vm-1": "/home/op/.ssh/id_devops"},
		SSHDestinations: map[string]SSHDestination{
			"prod/vm-1": {Kind: "openssh_alias", Alias: "app-prod"},
			"prod/vm-2": {Kind: "route", Route: "http", ProxyAddress: "100.109.21.17:8080"},
			"lab/vm-1":  {Kind: "route", Route: "direct"},
		},
	}
	if err := Save("", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load(""); !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}
}

// A state file written before the SSH fields existed must still load.
func TestLoadOfOlderStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"context":"prod","kind":"vm","sort":"name"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := State{Context: "prod", Kind: "vm", Sort: "name"}
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("older state file loaded as %+v, want %+v", got, want)
	}
}

// Load drops any destination it cannot prove is safe — an unknown kind, an
// alias that looks like an ssh(1) option, or a proxy address that is not a
// plain host:port — rather than handing back something a caller might trust
// enough to put on an ssh(1) argument list.
func TestLoadSanitizesUntrustworthySSHDestinations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{
		"ssh_destinations": {
			"prod/vm-1": {"kind": "openssh_alias", "alias": "app-prod"},
			"prod/vm-2": {"kind": "openssh_alias", "alias": "-oProxyCommand=evil"},
			"prod/vm-3": {"kind": "openssh_alias", "alias": "root@app-prod"},
			"prod/vm-4": {"kind": "route", "route": "socks5", "proxy_address": "127.0.0.1:1080"},
			"prod/vm-5": {"kind": "route", "route": "socks5", "proxy_address": "-x 127.0.0.1:1080"},
			"prod/vm-6": {"kind": "shell_out", "alias": "app-prod"},
			"prod/vm-7": {"kind": "route", "route": "sudo rm -rf /"},
			"prod/vm-8": {"kind": "route", "route": "http", "proxy_address": "proxy.lab:3128"},
			"prod/vm-9": {"kind": "openssh_alias", "alias": "app-prod", "route": "direct"},
			"prod/vm-10": {"kind": "route", "route": "direct", "proxy_address": "127.0.0.1:1080"},
			"prod/vm-11": {"kind": "route", "route": "openssh", "alias": "app-prod"}
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	want := map[string]SSHDestination{
		"prod/vm-1": {Kind: "openssh_alias", Alias: "app-prod"},
		"prod/vm-4": {Kind: "route", Route: "socks5", ProxyAddress: "127.0.0.1:1080"},
		"prod/vm-8": {Kind: "route", Route: "http", ProxyAddress: "proxy.lab:3128"},
	}
	if !reflect.DeepEqual(got.SSHDestinations, want) {
		t.Errorf("Load returned %+v, want only the safe entries %+v", got.SSHDestinations, want)
	}
}
