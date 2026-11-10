package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/update"
)

func TestUpdateCommand(t *testing.T) {
	brew := update.Method{Name: "Homebrew", Command: []string{"brew", "upgrade", "vsfleet"}, Runs: true}
	scoop := update.Method{Name: "Scoop", Command: []string{"scoop", "update", "vsfleet"}, Why: "Windows can't replace a vsfleet.exe that is running."}
	archive := update.Method{Name: "release archive", Why: "There is no package manager to hand this to."}
	exit3 := exec.Command("sh", "-c", "exit 3").Run()

	tests := []struct {
		name      string
		current   string
		off       bool
		method    update.Method
		latest    string
		checkErr  error
		runErr    error
		checkOnly bool
		wantRan   string
		wantOut   []string
		wantErr   string
	}{
		{name: "upgrades with brew", method: brew, latest: "0.11.0", wantRan: "brew upgrade vsfleet",
			wantOut: []string{"0.10.0 → 0.11.0", "releases/tag/v0.11.0", "✓ Upgraded to v0.11.0"}},
		{name: "already latest", method: brew, latest: "0.10.0", wantOut: []string{"vsfleet v0.10.0 is up to date."}},
		{name: "check only does not run", method: brew, latest: "0.11.0", checkOnly: true,
			wantOut: []string{"0.10.0 → 0.11.0", "To upgrade, run: brew upgrade vsfleet"}},
		{name: "scoop prints the command", method: scoop, latest: "0.11.0",
			wantOut: []string{"Windows can't replace", "Quit vsfleet, then run: scoop update vsfleet"}},
		{name: "archive points at the release page", method: archive, latest: "0.11.0",
			wantOut: []string{"releases/tag/v0.11.0", "Download it from the release page."}},
		{name: "failed upgrade is an error", method: brew, latest: "0.11.0", runErr: exit3, wantRan: "brew upgrade vsfleet",
			wantErr: "brew exited with status 3; still on v0.10.0"},
		{name: "failed check is an error", method: brew, checkErr: errors.New("blocked"), wantErr: "checking for a new release: blocked"},
		{name: "dev build", current: "dev", method: brew, latest: "0.11.0", wantErr: "development build"},
		{name: "notifier off", off: true, method: brew, latest: "0.11.0", wantErr: update.EnvNoUpdateNotifier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeUpdater(t, update.State{})
			if tt.current != "" {
				f.current = tt.current
			}
			f.off, f.method, f.runErr = tt.off, tt.method, tt.runErr
			f.latest = func(context.Context) (update.Found, error) {
				f.checks++
				return update.Found{Version: tt.latest}, tt.checkErr
			}
			var out, errOut bytes.Buffer
			a := &App{In: strings.NewReader(""), Out: &out, Err: &errOut, upd: f.updater}
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())

			err := a.runUpdate(cmd, f.updater, tt.checkOnly)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
			if got := joinRuns(f.ran); got != tt.wantRan {
				t.Errorf("ran %q, want %q", got, tt.wantRan)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output is missing %q:\n%s", want, out.String())
				}
			}
			if f.restarts != 0 {
				t.Error("the command must not restart vsfleet")
			}
		})
	}
}

func TestUpdateCommandRecordsTheCheck(t *testing.T) {
	f := newFakeUpdater(t, update.State{})
	a := &App{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, upd: f.updater}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if err := a.runUpdate(cmd, f.updater, true); err != nil {
		t.Fatal(err)
	}
	if st := update.Load(f.path); st.Latest != "0.11.0" || !st.CheckedAt.Equal(testNow) {
		t.Errorf("the check was not remembered for the interface: %+v", st)
	}
}

func joinRuns(runs [][]string) string {
	parts := make([]string, len(runs))
	for i, r := range runs {
		parts[i] = strings.Join(r, " ")
	}
	return strings.Join(parts, "; ")
}
