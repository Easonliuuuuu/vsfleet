package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/update"
)

func newUpdateCommand(a *App) *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upgrade"},
		Short:   "Upgrade vsfleet to the latest release",
		Long: `Check GitHub for the latest vsfleet release now and upgrade to it.

vsfleet runs the upgrade itself only for Homebrew and "go install" on macOS
and Linux. For Scoop, a .deb or .rpm package, or a release archive it prints
the command or download link to use instead: Windows cannot replace a
running vsfleet.exe, and vsfleet never runs sudo. Contacts no vCenter.`,
		Example: `  # Upgrade, or say how to
  vsfleet update

  # Only report whether a newer release is out
  vsfleet update --check`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runUpdate(cmd, a.updates(), checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report whether a newer release is out without upgrading")
	return cmd
}

func (a *App) runUpdate(cmd *cobra.Command, u *updater, checkOnly bool) error {
	if u.current == "" || u.current == "dev" {
		return errors.New("this is a development build; pull and rebuild it, or install a release")
	}
	if u.off {
		return fmt.Errorf("release checks are off because %s is set", update.EnvNoUpdateNotifier)
	}
	found, err := u.check(cmd.Context())
	if err != nil {
		return fmt.Errorf("checking for a new release: %w", err)
	}
	out := a.out()
	if !update.Newer(found.Version, u.current) {
		fmt.Fprintf(out, "vsfleet v%s is up to date.\n", u.current)
		return nil
	}
	fmt.Fprintf(out, "A new release of vsfleet is available: %s → %s\n%s\n", u.current, found.Version, update.ReleaseURL(found.Version))
	m := u.method
	if checkOnly || !m.Runs || len(m.Command) == 0 {
		printUpgradeAdvice(out, m, checkOnly)
		return nil
	}
	w := a.errOut()
	fmt.Fprintf(w, "Upgrading with %s\n$ %s\n", m.Name, m.CommandLine())
	if err := u.runCmd(cmd.Context(), m.Command, a.in(), out, w); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("%s exited with status %d; still on v%s", m.Command[0], exit.ExitCode(), u.current)
		}
		return fmt.Errorf("running %s: %w; still on v%s", m.Command[0], err, u.current)
	}
	fmt.Fprintf(out, "✓ Upgraded to v%s\n", found.Version)
	return nil
}

// printUpgradeAdvice says how to upgrade when vsfleet will not do it itself.
func printUpgradeAdvice(w io.Writer, m update.Method, checkOnly bool) {
	cl := m.CommandLine()
	if cl == "" {
		fmt.Fprintf(w, "%s Download it from the release page.\n", m.Why)
		return
	}
	if !checkOnly && m.Why != "" {
		fmt.Fprintf(w, "%s\n", m.Why)
	}
	if m.Name == "Scoop" {
		fmt.Fprintf(w, "Quit vsfleet, then run: %s\n", cl)
		return
	}
	fmt.Fprintf(w, "To upgrade, run: %s\n", cl)
}
