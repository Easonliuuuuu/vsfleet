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
		Sorts: map[string]string{"vm": "memory:desc", "host": "version:asc"},
	}
	if err := Save("", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load(""); !reflect.DeepEqual(got, want) {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}
}

// A state file written by an older release, including the SSH choices it
// used to remember, still loads; the SSH fields are ignored.
func TestLoadOfOlderStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"context":"prod","kind":"vm","sort":"name",
		"ssh_users":{"prod/vm-1":"tdclab"},
		"ssh_destinations":{"prod/vm-1":{"kind":"openssh_alias","alias":"app-prod"}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	want := State{Context: "prod", Kind: "vm", Sort: "name"}
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("older state file loaded as %+v, want %+v", got, want)
	}
}
