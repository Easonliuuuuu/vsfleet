package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

var legacyCaptureWord = regexp.MustCompile(`(?i)\bassessments?\b`)

// sayCapture fails on the old name for a capture. The file docs/assessments.md
// keeps its name, and NOT_ASSESSED is a different word.
func sayCapture(t *testing.T, where, text string) {
	t.Helper()
	text = strings.ReplaceAll(text, "docs/assessments.md", "")
	if word := legacyCaptureWord.FindString(text); word != "" {
		t.Errorf("%s says %q:\n%s", where, word, text)
	}
}

// Issue #541: every help text a user can reach says capture and indents with
// spaces. Hidden commands are the rename hints, which name the old commands,
// and cobra writes the completion help.
func TestHelpSaysCaptureAndIndentsWithSpaces(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Name() == "completion" {
			return
		}
		text := strings.Join([]string{cmd.Long, cmd.Example, cmd.LocalFlags().FlagUsages()}, "\n")
		sayCapture(t, cmd.CommandPath()+" --help", text)
		if strings.Contains(text, "\t") {
			t.Errorf("%s --help contains a tab:\n%s", cmd.CommandPath(), text)
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCommand(&App{}))
}

// Issue #541: output read back from the history says capture too.
func TestStoredOutputSaysCapture(t *testing.T) {
	dbPath := newDecommissionTestHistoryDB(t, vsphere.VM{ID: "vm-1", Name: "app", ConfigurationAvailable: true})
	for _, args := range [][]string{
		{"analyze", "decommission", "vm/app"},
		{"analyze", "sizing", "--hosts", "2", "--host-cores", "8", "--host-ram-gib", "64", "--datastore-capacity", "1TiB"},
		{"describe", "capture", "1", "-n", "no-such-context"},
		{"explain", "rvtools"},
	} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			stdout, stderr, err := runCLI(t, dbPath, args...)
			text := stdout + stderr
			if err != nil {
				text += err.Error()
			}
			sayCapture(t, strings.Join(args, " "), text)
		})
	}
}
