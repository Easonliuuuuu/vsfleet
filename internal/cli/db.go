package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type doctorExitError struct{ message string }

func (e *doctorExitError) Error() string { return e.message }
func (e *doctorExitError) ExitCode() int { return 1 }

// newDBCommand maintains the local capture database. Its subcommands act on
// the database file as a whole, so none of them takes -n or --run.
func newDBCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:   "db",
		Short: "Back up, restore and check the local capture database",
		Long: strings.TrimSpace(`
Back up, restore and check the SQLite database that holds every capture.

The database is --db, else $VSFLEET_DB, else history.db in the platform user
config directory. None of these commands contacts a vCenter.`),
		Example: `  # Back the database up, then check it
  vsfleet db backup ./vsfleet.db.bak
  vsfleet db doctor

  # Check a specific database, as JSON
  vsfleet db doctor --db ./vsfleet.db -o json`,
	})
	cmd.AddCommand(newDBBackupCommand(a), newDBRestoreCommand(a), newDBDoctorCommand(a))
	return cmd
}

func newDBBackupCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "backup FILE", Short: "Write a consistent copy of the capture database", Example: `  # Back the capture database up to a file
  vsfleet db backup ./vsfleet.db.bak

  # Overwrite an existing backup, e.g. from a nightly job
  vsfleet db backup ./vsfleet.db.bak --force`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectStoredContextSelection(cmd); err != nil {
			return err
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		if err := s.Backup(cmd.Context(), args[0], force); err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), map[string]string{"backup": args[0]})
		}
		fmt.Fprintf(a.out(), "database backup written to %s\n", args[0])
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing backup")
	return cmd
}

func newDBRestoreCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "restore FILE", Short: "Restore the capture database from a backup", Example: `  # Restore into a database that holds no captures yet
  vsfleet db restore ./vsfleet.db.bak

  # Replace existing captures in place (destructive, so it must be confirmed)
  vsfleet db restore ./vsfleet.db.bak --force`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectStoredContextSelection(cmd); err != nil {
			return err
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		safety, err := s.Restore(cmd.Context(), args[0], force)
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), map[string]string{"restored": args[0], "safety_backup": safety})
		}
		fmt.Fprintf(a.out(), "database restored from %s\npre-restore safety backup: %s\n", args[0], safety)
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "confirm replacing existing captures")
	return cmd
}

func newDBDoctorCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Check the capture database's integrity", Long: strings.TrimSpace(`
Check the capture database's schema, integrity and foreign keys, and count
what it holds. Exits 1 when a check reports a warning. "vsfleet doctor"
diagnoses the connection to a vCenter instead.`), Example: `  # Check the local capture database
  vsfleet db doctor

  # Check a specific database, as JSON
  vsfleet db doctor --db ./vsfleet.db -o json`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rejectStoredContextSelection(cmd); err != nil {
			return err
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		report, err := s.Doctor(cmd.Context())
		if err != nil {
			return err
		}
		if a.json() {
			if err := writeJSON(a.out(), report); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(a.out(), "schema: %d  integrity: %s  foreign keys: %s\n", report.SchemaVersion, report.Integrity, report.ForeignKeys)
			fmt.Fprintf(a.out(), "captures: %d  VMs: %d  resources: %d  database: %s\n", report.Runs, report.VMObservations, report.ResourceObservations, humanBytes(report.DatabaseBytes))
			for _, warning := range report.Warnings {
				fmt.Fprintf(a.out(), "%s %s\n", glyphFail, warning)
			}
		}
		if len(report.Warnings) > 0 {
			return &doctorExitError{message: "capture database checks reported warnings"}
		}
		return nil
	}}
	return cmd
}
