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

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vsfleet", "state.json")
	want := State{Context: "prod", Kind: "host", Sort: "status"}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load(path)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
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

func TestSaveCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does", "not", "exist", "yet", "state.json")
	if err := Save(path, State{Context: "prod"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load(path); got.Context != "prod" {
		t.Errorf("Load returned %+v after Save created the directory", got)
	}
}

func TestDefaultPathHonoursTheEnvironmentVariable(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-state.json")
	t.Setenv(EnvStatePath, want)

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if got != want {
		t.Errorf("DefaultPath returned %q, want %q", got, want)
	}
}

// TestLoadAndSaveDefaultToTheEnvOverride checks that passing an empty path,
// the way the CLI actually calls this package, resolves through
// VSFLEET_STATE rather than needing every caller to look the path up itself.
func TestLoadAndSaveDefaultToTheEnvOverride(t *testing.T) {
	t.Setenv(EnvStatePath, filepath.Join(t.TempDir(), "state.json"))

	want := State{Context: "customer-a", Kind: "network", Sort: "status"}
	if err := Save("", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load(""); !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}
}

// The SSH users typed in the TUI are remembered per machine and must survive
// a restart, while a state file written before they existed must still load.
func TestSSHUsersRoundTripAndOlderFilesStillLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := State{Context: "prod", SSHUsers: map[string]string{"prod/vm-1": "tdclab", "lab/host-2": "root"}, SSHIdentityFiles: map[string]string{"prod/vm-1": "/home/eason/.ssh/id_devops"}}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}

	old := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(old, []byte(`{"context":"prod","kind":"vm","sort":"name"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Load(old)
	if got.Context != "prod" || len(got.SSHUsers) != 0 || len(got.SSHIdentityFiles) != 0 {
		t.Errorf("older state file loaded as %+v", got)
	}
}

// The remembered SSH destination — an OpenSSH alias or a per-VM route
// override — round-trips the same way the user and identity maps do, and
// the same MoRef in two different contexts is isolated.
func TestSSHDestinationsRoundTripAndAreIsolatedByContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := State{
		SSHDestinations: map[string]SSHDestination{
			"tdc-1f/vm-1234": {Kind: "openssh_alias", Alias: "app-prod"},
			"tdc-1f/vm-5678": {Kind: "route", Route: "http", ProxyAddress: "100.109.21.17:8080"},
			"lab/vm-1234":    {Kind: "route", Route: "direct"},
		},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load(path)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}
	if got.SSHDestinations["tdc-1f/vm-1234"] == got.SSHDestinations["lab/vm-1234"] {
		t.Errorf("same MoRef in two contexts should be independent, got identical entries")
	}
}

// A renamed VM keeps its destination because the key is <context>/<moref>,
// never the display name.
func TestSSHDestinationsKeyIsContextAndMoRefNotName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := State{SSHDestinations: map[string]SSHDestination{"prod/vm-42": {Kind: "openssh_alias", Alias: "app-prod"}}}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load(path)
	if d, ok := got.SSHDestinations["prod/vm-42"]; !ok || d.Alias != "app-prod" {
		t.Errorf("Load returned %+v, want the entry keyed prod/vm-42 to survive a rename", got)
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
			"prod/vm-7": {"kind": "route", "route": "sudo rm -rf /"}
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	want := map[string]SSHDestination{
		"prod/vm-1": {Kind: "openssh_alias", Alias: "app-prod"},
		"prod/vm-4": {Kind: "route", Route: "socks5", ProxyAddress: "127.0.0.1:1080"},
	}
	if !reflect.DeepEqual(got.SSHDestinations, want) {
		t.Errorf("Load returned %+v, want only the safe entries %+v", got.SSHDestinations, want)
	}
}

// A state file with no ssh_destinations key at all (every file written
// before this feature existed) must still load cleanly.
func TestLoadOfStateWithoutSSHDestinationsStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"context":"prod"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.Context != "prod" || got.SSHDestinations != nil {
		t.Errorf("Load returned %+v", got)
	}
}
