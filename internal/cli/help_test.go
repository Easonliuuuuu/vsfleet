package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/topology"
)

// runArgs executes the command tree without touching the machine: every
// command exercised here fails argument validation before any lazy accessor on
// App can run.
func runArgs(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var in, out, errOut bytes.Buffer
	a := &App{In: &in, Out: &out, Err: &errOut}
	root := NewRootCommand(a)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// walk visits every command in the tree except the ones cobra generates.
func walk(cmd *cobra.Command, fn func(*cobra.Command)) {
	for _, sub := range cmd.Commands() {
		if sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		fn(sub)
		walk(sub, fn)
	}
}

// A parent command used to accept any argument, print its own help and report
// success, so "vsfleet assessment lst --all-contexts" looked to a scheduled job
// exactly like a capture that worked. Every parent must now fail instead.
func TestUnknownSubcommandFails(t *testing.T) {
	for _, parent := range [][]string{
		{"assessment"},
		{"assessment", "trends"},
		{"vm"},
		{"host"},
		{"network"},
		{"context"},
		{"compatibility"},
	} {
		args := append(append([]string{}, parent...), "no-such-subcommand")
		t.Run(strings.Join(parent, " "), func(t *testing.T) {
			_, _, err := runArgs(t, args...)
			if err == nil {
				t.Fatalf("%v reported success for an unknown subcommand", args)
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("%v: got %q, want an unknown-command error", args, err)
			}
		})
	}
}

// A bare parent is a reasonable question — "what is under assessment?" — so it
// still answers with help and still succeeds.
func TestBareParentPrintsHelp(t *testing.T) {
	out, _, err := runArgs(t, "assessment")
	if err != nil {
		t.Fatalf("bare parent command failed: %v", err)
	}
	if !strings.Contains(out, "Available Commands:") {
		t.Fatalf("bare parent printed no command list, got:\n%s", out)
	}
}

// Root cannot use cobra's default argument validator, because a bare "vsfleet"
// opens the terminal interface. That choice should not also cost the near miss.
func TestTyposSuggestTheIntendedCommand(t *testing.T) {
	for _, tc := range []struct{ args, want []string }{
		{[]string{"statas"}, []string{"status"}},
		{[]string{"assessment", "lst"}, []string{"list"}},
		{[]string{"vm", "lst"}, []string{"list"}},
		{[]string{"contxt"}, []string{"context"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, _, err := runArgs(t, tc.args...)
			if err == nil {
				t.Fatalf("%v reported success", tc.args)
			}
			if !strings.Contains(err.Error(), "Did you mean this?") {
				t.Fatalf("%v: no suggestion offered, got %q", tc.args, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%v: suggestion did not mention %q, got %q", tc.args, want, err)
				}
			}
		})
	}
}

// Help is the only documentation a user reaches from the terminal, and the
// command tree keeps growing. A new command arrives with its examples or this
// fails.
func TestEveryCommandHasExamples(t *testing.T) {
	root := NewRootCommand(&App{})
	if root.Example == "" {
		t.Error("the root command has no examples")
	}
	walk(root, func(cmd *cobra.Command) {
		if cmd.Example == "" {
			t.Errorf("%q has no examples", cmd.CommandPath())
		}
	})
}

// An example that does not name its own command is a copy-and-paste slip, and
// the reader has no way to tell.
func TestExamplesInvokeTheirOwnCommand(t *testing.T) {
	root := NewRootCommand(&App{})
	walk(root, func(cmd *cobra.Command) {
		if !strings.Contains(cmd.Example, cmd.CommandPath()) {
			t.Errorf("%q has examples that never invoke it:\n%s", cmd.CommandPath(), cmd.Example)
		}
	})
}

// The root listing is grouped so a newcomer can tell where to start. A command
// added without a group silently falls into "Additional Commands" below the
// generated ones, which is where it stops being found.
func TestEveryTopLevelCommandIsGrouped(t *testing.T) {
	root := NewRootCommand(&App{})
	groups := map[string]bool{}
	for _, g := range root.Groups() {
		groups[g.ID] = true
	}
	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		if !groups[cmd.GroupID] {
			t.Errorf("%q is in no root help group (GroupID %q)", cmd.Name(), cmd.GroupID)
		}
	}
}

// Giving a parent a RunE is what makes the unknown-subcommand check possible,
// but it also makes cobra think the parent is worth invoking on its own. The
// usage line must not start advertising "vsfleet assessment [flags]", which
// does nothing but print the help the reader is already looking at.
func TestParentsDoNotAdvertiseABareFlagsInvocation(t *testing.T) {
	root := NewRootCommand(&App{})
	walk(root, func(cmd *cobra.Command) {
		if !cmd.HasAvailableSubCommands() {
			return
		}
		usage := cmd.UsageString()
		if strings.Contains(usage, cmd.CommandPath()+" [flags]") {
			t.Errorf("%q advertises a bare [flags] invocation:\n%s", cmd.CommandPath(), usage)
		}
		if !strings.Contains(usage, cmd.CommandPath()+" [command]") {
			t.Errorf("%q does not tell the reader a subcommand is required:\n%s", cmd.CommandPath(), usage)
		}
	})
}

// The parent template is inherited by descendants, so a leaf under a parent
// must still print the usage line that tells the reader how to invoke it.
func TestLeafCommandsStillShowTheirUsageLine(t *testing.T) {
	root := NewRootCommand(&App{})
	walk(root, func(cmd *cobra.Command) {
		if cmd.HasAvailableSubCommands() {
			return
		}
		if !strings.Contains(cmd.UsageString(), cmd.UseLine()) {
			t.Errorf("%q prints no usage line:\n%s", cmd.CommandPath(), cmd.UsageString())
		}
	})
}

// KIND is a closed vocabulary, so completion should offer it. The values have
// to stay in step with topology.ParseKind, which is what actually enforces them.
func TestTopologyKindsAreOfferedForCompletion(t *testing.T) {
	root := NewRootCommand(&App{})
	for _, name := range []string{"topology", "dependencies", "blast-radius"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("finding %q: %v", name, err)
		}
		if len(cmd.ValidArgs) == 0 {
			t.Errorf("%q offers no kinds for completion", name)
		}
		for _, kind := range topologyKinds {
			if !slices.Contains(cmd.ValidArgs, kind) {
				t.Errorf("%q does not offer kind %q", name, kind)
			}
		}
	}
}

// Completion and help advertise the kinds; topology.ParseKind is what actually
// accepts them. A kind offered but not parsed is worse than one never offered.
func TestOfferedTopologyKindsAllParse(t *testing.T) {
	for _, kind := range topologyKinds {
		if _, err := topology.ParseKind(kind); err != nil {
			t.Errorf("kind %q is offered in help but rejected by ParseKind: %v", kind, err)
		}
	}
}
