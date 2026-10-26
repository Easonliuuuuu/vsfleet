package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/version"
)

func newVersionCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the vsfleet version",
		Long: `Print the vsfleet version and the commit it was built from, the way
"kubectl version --client" does. --version prints the same. Contacts no
vCenter.`,
		Example: `  # The version
  vsfleet version

  # As JSON, for a script
  vsfleet version -o json`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if a.json() {
				return writeJSON(a.out(), map[string]string{"version": version.String(), "release": version.Release()})
			}
			fmt.Fprintln(a.out(), version.String())
			return nil
		},
	}
}
