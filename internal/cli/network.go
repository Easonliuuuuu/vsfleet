package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/network"
)

func newNetworkCompareCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "compare SOURCE_CLUSTER TARGET_CLUSTER [RUN]",
		Short: "Compare stored network readiness between two clusters",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := loadRunExportData(cmd, a, args[2:])
			if err != nil {
				return err
			}
			comparison := network.Compare(data, args[0], args[1], a.ContextNames)
			printNetworkBlindness(a.errOut(), comparison.Blind)
			if a.json() {
				return writeJSON(a.out(), comparison)
			}
			return printNetworkComparison(a.out(), comparison)
		},
	}
}

func newAssessmentNetworkReadinessCommand(a *App) *cobra.Command {
	var source, target string
	var failOnBlockers bool
	cmd := &cobra.Command{
		Use:   "network-readiness [RUN]",
		Short: "Assess stored network readiness between two clusters",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := loadRunExportData(cmd, a, args)
			if err != nil {
				return err
			}
			readiness := network.Verdict(network.Compare(data, source, target, a.ContextNames))
			printNetworkBlindness(a.errOut(), readiness.Blind)
			if a.json() {
				if err := writeJSON(a.out(), readiness); err != nil {
					return err
				}
			} else {
				printNetworkReadiness(a.out(), readiness)
			}
			if failOnBlockers && readiness.Verdict == "blocked" {
				return &readinessExitError{blockers: len(readiness.Blockers)}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "source cluster name")
	cmd.Flags().StringVar(&target, "target", "", "target cluster name")
	cmd.Flags().BoolVar(&failOnBlockers, "fail-on-blockers", false, "exit 2 when network migration blockers are present")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func printNetworkBlindness(out io.Writer, blind []network.Blindness) {
	for _, item := range blind {
		fmt.Fprintf(out, "%s %s: %s\n", glyphFail, item.Context, item.Reason)
	}
}

func printNetworkComparison(out io.Writer, comparison network.Comparison) error {
	f := newFields(out)
	f.add("Assessment", strconv.FormatInt(comparison.RunID, 10))
	f.add("Source", clusterLabel(comparison.Source))
	f.add("Target", clusterLabel(comparison.Target))
	f.add("Confidence", comparison.Confidence)
	if comparison.Ambiguous {
		f.add("Resolution", "ambiguous; use --context to narrow the cluster name")
	}
	f.add("Checked contexts", strings.Join(comparison.CheckedContexts, ", "))
	f.flush()

	if len(comparison.Candidates) > 0 {
		fmt.Fprintln(out, "Candidates:")
		t := newTable(out, "NAME", "CONTEXT", "VCenter", "HOSTS")
		for _, candidate := range comparison.Candidates {
			t.row(candidate.Name, candidate.Context, dash(candidate.VCenterID), strconv.Itoa(candidate.HostCount))
		}
		t.flush()
	}
	if len(comparison.Matched) > 0 {
		fmt.Fprintln(out, "Matched networks:")
		t := newTable(out, "SOURCE", "TARGET", "MATCH", "VLAN", "MTU", "HOST COVERAGE")
		for _, match := range comparison.Matched {
			t.row(match.Source.Name, match.Target.Name, match.MatchBasis, dash(match.Source.VLAN), networkMTU(match.Source.MTU, match.Target.MTU), networkCoverage(match.Source), networkCoverage(match.Target))
		}
		t.flush()
	}
	if len(comparison.MappingGaps) > 0 {
		fmt.Fprintln(out, "Mapping gaps:")
		t := newTable(out, "NETWORK", "VLAN", "HOST COVERAGE", "ATTACHED VMS", "SEVERITY")
		for _, gap := range comparison.MappingGaps {
			names := make([]string, 0, len(gap.AttachedVMs))
			for _, vm := range gap.AttachedVMs {
				names = append(names, vm.Name)
			}
			t.row(gap.Network.Name, dash(gap.Network.VLAN), networkCoverage(gap.Network), dash(strings.Join(names, ", ")), gap.Severity)
		}
		t.flush()
	}
	if len(comparison.TargetOnly) > 0 {
		fmt.Fprintln(out, "Target-only networks:")
		t := newTable(out, "NETWORK", "VLAN", "PARENT SWITCH", "MTU", "HOST COVERAGE")
		for _, network := range comparison.TargetOnly {
			t.row(network.Name, dash(network.VLAN), dash(network.ParentSwitch), strconv.FormatInt(int64(network.MTU), 10), networkCoverage(network))
		}
		t.flush()
	}
	if len(comparison.Differences) > 0 {
		fmt.Fprintln(out, "Differences:")
		t := newTable(out, "NETWORK", "FIELD", "SOURCE", "TARGET", "SEVERITY")
		for _, difference := range comparison.Differences {
			t.row(difference.Network, difference.Field, dash(difference.Source), dash(difference.Target), difference.Severity)
		}
		t.flush()
	}
	return nil
}

func printNetworkReadiness(out io.Writer, readiness network.Readiness) {
	fmt.Fprintf(out, "Network readiness: %s\n", strings.ToUpper(readiness.Verdict))
	if len(readiness.Blockers) > 0 {
		fmt.Fprintln(out, "Blockers:")
		for _, blocker := range readiness.Blockers {
			fmt.Fprintf(out, "  %s\n", blocker)
		}
	}
	if len(readiness.Advisories) > 0 {
		fmt.Fprintln(out, "Advisories:")
		for _, advisory := range readiness.Advisories {
			fmt.Fprintf(out, "  %s\n", advisory)
		}
	}
	if (readiness.Verdict == "unknown" && len(readiness.Blind) == 0 && (!readiness.Source.Resolved || !readiness.Target.Resolved)) || readiness.Ambiguous {
		fmt.Fprintln(out, "Not evaluated:")
		if readiness.Ambiguous {
			fmt.Fprintln(out, "  cluster name is ambiguous")
		} else if !readiness.Source.Resolved || !readiness.Target.Resolved {
			fmt.Fprintln(out, "  source or target cluster could not be resolved")
		}
	}
	if len(readiness.Blind) > 0 {
		fmt.Fprintln(out, "Not evaluated:")
		for _, item := range readiness.Blind {
			fmt.Fprintf(out, "  %s — %s\n", item.Context, item.Reason)
		}
	}
}

func clusterLabel(ref network.ClusterRef) string {
	if ref.Context == "" {
		return ref.Name
	}
	return ref.Name + " (" + ref.Context + ")"
}

func networkMTU(source, target int32) string {
	if source == target && source > 0 {
		return strconv.FormatInt(int64(source), 10)
	}
	if source > 0 {
		return strconv.FormatInt(int64(source), 10) + "/" + strconv.FormatInt(int64(target), 10)
	}
	return ""
}

func networkCoverage(value network.NetworkSummary) string {
	return strconv.Itoa(value.CoveredHosts) + "/" + strconv.Itoa(value.TotalHosts)
}
