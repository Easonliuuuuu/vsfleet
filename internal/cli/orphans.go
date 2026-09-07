package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/health"
)

type orphanFlags struct {
	confidence []string
	minSize    int64
}

func newAssessmentOrphansCommand(a *App) *cobra.Command {
	var flags orphanFlags
	cmd := &cobra.Command{
		Use:   "orphans [RUN]",
		Short: "Explain browsed datastore VMDK orphan evidence",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for i := range flags.confidence {
				flags.confidence[i] = strings.ToLower(strings.TrimSpace(flags.confidence[i]))
				switch health.Confidence(flags.confidence[i]) {
				case health.ConfidenceVerified, health.ConfidenceSuspected, health.ConfidenceOtherContext, health.ConfidenceUnknown:
				default:
					return fmt.Errorf("unknown --confidence %q", flags.confidence[i])
				}
			}
			if flags.minSize < 0 {
				return fmt.Errorf("--min-size must be zero or greater")
			}
			data, err := loadRunExportData(cmd, a, args)
			if err != nil {
				return err
			}
			report := health.Orphans(data)
			report.Entries = filterOrphans(report.Entries, flags)
			if a.json() {
				return writeJSON(a.out(), report)
			}
			printOrphans(a, report)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&flags.confidence, "confidence", nil, "only show confidence tier (repeat or comma-separate)")
	cmd.Flags().Int64Var(&flags.minSize, "min-size", 0, "only show files at least this many bytes")
	return cmd
}

func filterOrphans(entries []health.OrphanEvidence, flags orphanFlags) []health.OrphanEvidence {
	allowed := make(map[health.Confidence]bool, len(flags.confidence))
	for _, value := range flags.confidence {
		allowed[health.Confidence(strings.ToLower(strings.TrimSpace(value)))] = true
	}
	out := make([]health.OrphanEvidence, 0, len(entries))
	for _, entry := range entries {
		if flags.minSize > 0 && entry.SizeBytes < flags.minSize {
			continue
		}
		if len(allowed) > 0 && !allowed[entry.Confidence] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func printOrphans(a *App, report health.OrphanReport) {
	if len(report.Entries) == 0 {
		fmt.Fprintln(a.out(), "No browsed VMDK orphan candidates.")
		return
	}
	for i, entry := range report.Entries {
		if i > 0 {
			fmt.Fprintln(a.out())
		}
		fmt.Fprintf(a.out(), "[%s] [%s] %s  %s  %s\n", orphanLabel(entry.Confidence), entry.Object.Name, entry.Path, humanBytes(entry.SizeBytes), orphanDate(entry.Modified))
		fields := newFields(a.out())
		if len(entry.ReferencedBy) == 0 {
			fields.add("not referenced by", strings.Join(entry.CheckedContexts, ", "))
		} else {
			for _, ref := range entry.ReferencedBy {
				label := "referenced by"
				if ref.Template {
					label = "referenced by template"
				}
				fields.add(label, fmt.Sprintf("%s @ %s", ref.VM, ref.Context))
			}
		}
		for _, blind := range entry.Blind {
			fields.add("could not verify", blind.Context+" — "+blind.Reason)
		}
		if len(entry.Identity) > 0 {
			fields.add("shared datastore", entry.Object.Name+" / "+strings.Join(entry.Identity, ", "))
		}
		if len(entry.Reasons) > 0 {
			fields.add("reason", strings.Join(entry.Reasons, "; "))
		}
		fields.flush()
	}
}

func orphanLabel(confidence health.Confidence) string {
	switch confidence {
	case health.ConfidenceVerified:
		return "VERIFIED"
	case health.ConfidenceSuspected:
		return "SUSPECTED"
	case health.ConfidenceOtherContext:
		return "IN USE"
	default:
		return "UNKNOWN"
	}
}

func orphanDate(value time.Time) string {
	if value.IsZero() {
		return "unknown date"
	}
	return value.Local().Format("2006-01-02")
}
