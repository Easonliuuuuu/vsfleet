package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// mutatingVerbs are kubectl verbs that change a cluster. vsfleet never
// changes a vCenter, so each answers with that instead of "unknown command",
// and points at the read-only command that comes closest, if any.
var mutatingVerbs = []struct{ verb, instead string }{
	{"apply", ""},
	{"create", ""},
	{"edit", ""},
	{"patch", ""},
	{"replace", ""},
	{"delete", ""},
	{"scale", ""},
	{"rollout", ""},
	{"cordon", ""},
	{"uncordon", ""},
	{"drain", ""},
	{"taint", ""},
	{"label", ""},
	{"annotate", ""},
	{"exec", ""},
	{"attach", ""},
	{"cp", ""},
	{"logs", `"vsfleet events --for vm/NAME" shows what vCenter logged for a VM`},
}

// readOnlyVerbCommands are hidden, so they never crowd help or completion;
// they exist only to answer someone typing a kubectl habit.
func readOnlyVerbCommands() []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(mutatingVerbs))
	for _, v := range mutatingVerbs {
		cmds = append(cmds, &cobra.Command{
			Use:                v.verb,
			Hidden:             true,
			DisableFlagParsing: true,
			RunE: func(*cobra.Command, []string) error {
				msg := fmt.Sprintf("read-only: vsfleet never changes a vCenter, so there is no %q", v.verb)
				if v.instead != "" {
					msg += "; " + v.instead
				}
				return errors.New(msg)
			},
		})
	}
	return cmds
}
