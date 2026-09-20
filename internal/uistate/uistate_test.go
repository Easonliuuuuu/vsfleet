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
