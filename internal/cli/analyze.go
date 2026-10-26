package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/health"
	"github.com/easonliuuuuu/vsfleet/internal/network"
)

func newAnalyzeCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:   "analyze",
		Short: "Analyze stored captures: health, readiness, sizing, capacity and dependencies",
		Long: strings.TrimSpace(`
Answer questions about the estate from stored captures: what is unhealthy,
what blocks a migration, whether it fits a destination, where capacity is
going, and what depends on what.

Every analysis reads the capture --run names, the most recent by default,
and never contacts a vCenter. It covers every captured context unless -n
narrows it, because the questions cross vCenters: a migration's source and
target are often on different ones. A subject is given as KIND/NAME or
KIND NAME, as in "vsfleet get".`),
		Example: `  # Health findings in the most recent capture
  vsfleet analyze health

  # Is the estate ready to migrate, and are two clusters' networks?
  vsfleet analyze readiness
  vsfleet analyze readiness network cl/prod-a cl/dr-a

  # What breaks if a host goes down, as a labelled capture saw it
  vsfleet analyze blast-radius host/esxi-07 --run nightly

  # Datastore growth across the stored history
  vsfleet analyze capacity --trend`,
	})
	cmd.AddCommand(
		newAnalyzeHealthCommand(a),
		newAnalyzeReadinessCommand(a),
		newAnalyzeSizingCommand(a),
		newAnalyzeCapacityCommand(a),
		newAnalyzeOrphansCommand(a),
		newAnalyzeChurnCommand(a),
		newAnalyzeSnapshotsCommand(a),
		newTopologyCommand(a),
		newDependenciesCommand(a),
		newBlastRadiusCommand(a),
		newAnalyzeDecommissionCommand(a),
	)
	return cmd
}

// addRunFlag gives an analysis its --run: the capture to read.
func addRunFlag(cmd *cobra.Command, run *string) {
	cmd.Flags().StringVar(run, "run", "latest", "capture to analyze: ID, label, latest, previous or date")
}

// flagRename is a removed flag and what replaced it.
type flagRename struct {
	name, hint string
	boolean    bool
}

// renameFlags keeps removed flag spellings answering with their replacement,
// hidden from help, rather than leaving cobra to call them unknown.
func renameFlags(cmd *cobra.Command, renames ...flagRename) {
	for _, r := range renames {
		if r.boolean {
			cmd.Flags().Bool(r.name, false, r.hint)
		} else {
			cmd.Flags().String(r.name, "", r.hint)
		}
		_ = cmd.Flags().MarkHidden(r.name)
	}
	next := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		for _, r := range renames {
			if c.Flags().Changed(r.name) {
				return fmt.Errorf("--%s was renamed: %s", r.name, r.hint)
			}
		}
		return next(c, args)
	}
}

func newAnalyzeReadinessCommand(a *App) *cobra.Command {
	var flags healthFlags
	var failOnBlockers bool
	cmd := &cobra.Command{
		Use:   "readiness",
		Short: "Reduce the health rules to a migration-readiness verdict",
		Long: strings.TrimSpace(`
Reduce the health rules to one verdict on whether the estate is ready to
migrate: ready, blocked or unknown, with the blockers, advisories and rules
that could not be evaluated. "readiness network" answers the same for the
networks between two clusters.`),
		Example: `  # Readiness verdict for the most recent capture
  vsfleet analyze readiness

  # Verdict for a labelled capture, as JSON
  vsfleet analyze readiness --run pre-migration -o json

  # Gate a migration pipeline: exit 2 when blockers remain
  vsfleet analyze readiness --fail-on-blockers`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Readiness always considers every category and severity. The tuning and
			// disable flags still apply because they change the evaluated report.
			flags.minimumSeverity = "info"
			flags.category = ""
			report, err := evaluateHealthCommand(cmd, a, flags)
			if err != nil {
				return err
			}
			readiness := health.Readiness(report)
			if a.json() {
				if err := writeJSON(a.out(), readiness); err != nil {
					return err
				}
			} else {
				printMigrationReadiness(a, readiness)
			}
			if failOnBlockers && len(readiness.Blockers) > 0 {
				return &readinessExitError{blockers: len(readiness.Blockers)}
			}
			return nil
		},
	}
	flags.add(cmd, false)
	cmd.Flags().BoolVar(&failOnBlockers, "fail-on-blockers", false, "exit 2 when migration blockers are present")
	cmd.AddCommand(newAnalyzeNetworkReadinessCommand(a))
	return cmd
}

func printMigrationReadiness(a *App, readiness health.ReadinessReport) {
	fmt.Fprintf(a.out(), "Migration readiness: %s\n", strings.ToUpper(readiness.Verdict))
	if len(readiness.Blockers) > 0 {
		fmt.Fprintln(a.out(), "Blockers:")
		for _, finding := range readiness.Blockers {
			fmt.Fprintf(a.out(), "  %s [%s] %s — %s\n", finding.Rule, finding.Object.Kind+"/"+finding.Object.Name, finding.Message, finding.Recommendation)
		}
	}
	if len(readiness.Advisories) > 0 {
		fmt.Fprintln(a.out(), "Advisories:")
		for _, finding := range readiness.Advisories {
			fmt.Fprintf(a.out(), "  %s [%s] %s — %s\n", finding.Rule, finding.Object.Kind+"/"+finding.Object.Name, finding.Message, finding.Recommendation)
		}
	}
	if len(readiness.Unresolved) > 0 {
		fmt.Fprintln(a.out(), "Not evaluated:")
		for _, rule := range readiness.Unresolved {
			reason := rule.Reason
			if len(rule.Blind) > 0 {
				reason = strings.TrimSpace(strings.TrimSuffix(reason, ".") + "; blind vCenters: " + strings.Join(rule.Blind, ", "))
			}
			fmt.Fprintf(a.out(), "  %s — %s\n", rule.Rule, reason)
		}
	}
}

func newAnalyzeNetworkReadinessCommand(a *App) *cobra.Command {
	var (
		run            string
		failOnBlockers bool
	)
	cmd := &cobra.Command{
		Use:   "network SOURCE TARGET",
		Short: "Judge whether two clusters' networks are ready for a migration",
		Long: strings.TrimSpace(`
Compare the networks reachable from two clusters, SOURCE and TARGET, and
reduce the comparison to a readiness verdict: VLAN mappings, policy, MTU,
host coverage and the VMs a gap would strand. -o wide adds the full
comparison behind the verdict, and -o json always carries it.

Each cluster is given as cluster/NAME, cl/NAME or NAME. They may be on
different vCenters; a name found on several contexts is ambiguous until -n
narrows it.`),
		Example: `  # Readiness between two clusters
  vsfleet analyze readiness network cl/prod-cluster-a cl/dr-cluster-a

  # With the full comparison, against a labelled capture
  vsfleet analyze readiness network prod-cluster-a dr-cluster-a --run pre-migration -o wide

  # Gate a migration pipeline: exit 2 when blockers remain
  vsfleet analyze readiness network cl/prod-cluster-a cl/dr-cluster-a --fail-on-blockers`,
		Annotations: map[string]string{annotationWideOutput: "true"},
		Args:        cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := clusterArg(args[0])
			if err != nil {
				return err
			}
			target, err := clusterArg(args[1])
			if err != nil {
				return err
			}
			data, err := loadRunExportData(cmd, a, run)
			if err != nil {
				return err
			}
			readiness := network.Verdict(network.Compare(data, source, target, a.StoredContextNames()))
			printNetworkBlindness(a.errOut(), readiness.Blind)
			if a.json() {
				if err := writeJSON(a.out(), readiness); err != nil {
					return err
				}
			} else {
				printNetworkReadiness(a.out(), readiness)
				if a.wide() {
					fmt.Fprintln(a.out())
					if err := printNetworkComparison(a.out(), readiness.Comparison); err != nil {
						return err
					}
				}
			}
			if failOnBlockers && readiness.Verdict == "blocked" {
				return &readinessExitError{blockers: len(readiness.Blockers)}
			}
			return nil
		},
	}
	addRunFlag(cmd, &run)
	cmd.Flags().BoolVar(&failOnBlockers, "fail-on-blockers", false, "exit 2 when network migration blockers are present")
	return cmd
}

// clusterArg reads a cluster given as cluster/NAME, any spelling of the
// cluster kind, or a bare NAME.
func clusterArg(arg string) (string, error) {
	kindName, name, ok := strings.Cut(arg, "/")
	if !ok {
		return arg, nil
	}
	if k, found := apiresource.Lookup(kindName); !found || k.Name != "cluster" {
		return "", fmt.Errorf("%q: readiness network compares clusters; give cluster/NAME or NAME", arg)
	}
	if name == "" {
		return "", errors.New("a cluster NAME is required after " + kindName + "/")
	}
	return name, nil
}
