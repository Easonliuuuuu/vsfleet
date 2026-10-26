package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// renamedCommand keeps a removed command name answering with where its job
// went, so a script or a habit gets a pointer instead of "unknown command".
// It is hidden from help and completion and always fails.
func renamedCommand(name string, aliases []string, hint string) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Aliases:            aliases,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(c *cobra.Command, _ []string) error {
			// The path names a nested stub in full: "vm list", not "list".
			path := strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
			return fmt.Errorf("%q was renamed: %s", path, hint)
		},
	}
}

// renamedGroupCommand stubs a removed group whose subcommands moved to
// different places: the first argument that names one of subs gets that
// hint, and anything else gets the group's.
func renamedGroupCommand(name, hint string, subs map[string]string) *cobra.Command {
	cmd := renamedCommand(name, nil, hint)
	generic := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		path := strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
		// Flag parsing is off, so args can hold flags too.
		for _, arg := range args {
			if subHint, ok := subs[arg]; ok {
				return fmt.Errorf("%q was renamed: %s", path+" "+arg, subHint)
			}
		}
		return generic(c, args)
	}
	return cmd
}

// renamedPerfCommand answers the removed "assessment perf" group.
func renamedPerfCommand() *cobra.Command {
	return renamedGroupCommand("perf", `use "vsfleet capture perf" to collect and "vsfleet get perfcollections" to list`, map[string]string{
		"collect": `use "vsfleet capture perf"`,
		"list":    `use "vsfleet get perfcollections"`,
		"ls":      `use "vsfleet get perfcollections"`,
		"show":    `use "vsfleet top vm", or "vsfleet top vm --window ID" for an older collection`,
	})
}
