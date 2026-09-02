// Package uistate remembers what the terminal interface was showing between
// runs — the context, the resource tab and the sort order — so that opening
// vcfleet a second time picks up where the last session left off.
//
// This is deliberately not part of internal/config. The configuration file
// describes what a vCenter is and how to reach it, and is meant to be
// written once and occasionally hand-edited; this file is scratch state a
// person never needs to look at, and losing it costs nothing more than
// landing on the default view once.
package uistate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvStatePath overrides the state file location, the same way
// config.EnvConfigPath overrides the configuration file.
const EnvStatePath = "VCFLEET_STATE"

// State is everything remembered between runs. Every field is optional: a
// zero value just means "nothing to restore", never an error.
type State struct {
	Context string `json:"context,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Sort    string `json:"sort,omitempty"`
}

// DefaultPath returns the state file path, honouring VCFLEET_STATE and then the
// platform user configuration directory.
func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvStatePath)); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "vcfleet", "state.json"), nil
}

// Load reads the remembered state. A missing file, or one that fails to
// parse, is not an error: both just mean starting from the defaults, which
// is exactly what a first run looks like anyway.
func Load(path string) State {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return State{}
		}
		path = p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}
	}
	return s
}

// Save writes the state atomically, mirroring how the configuration file is
// written: a temp file in the same directory, renamed into place, so a
// process killed mid-write never leaves a truncated file behind.
func Save(path string, s State) error {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode interface state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	tmpPath = ""
	return nil
}
