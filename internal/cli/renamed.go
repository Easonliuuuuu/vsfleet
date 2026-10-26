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
