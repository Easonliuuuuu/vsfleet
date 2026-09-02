package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vcfleet/internal/search"
	"github.com/easonliuuuuu/vcfleet/internal/vsphere"
)

func newSearchCommand(a *App) *cobra.Command {
	var (
		kinds []string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "search <text>",
		Short: "Search every vCenter at once",
		Long: `Search every configured vCenter for objects whose name contains the text.

This is the question that is genuinely hard to answer today: which of the
estate's vCenters holds that template, and where. Each vCenter is queried
concurrently and independently, so one that is unreachable costs one line of
output rather than the whole answer.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed := make([]vsphere.Kind, 0, len(kinds))
			for _, k := range kinds {
				kind, err := vsphere.ParseKind(strings.ToLower(strings.TrimSpace(k)))
				if err != nil {
					return err
				}
				parsed = append(parsed, kind)
			}
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			// Search spans the whole estate unless the operator narrows it.
			contexts, err := cfg.Resolve(a.ContextNames, a.AllContexts || len(a.ContextNames) == 0)
			if err != nil {
				return err
			}
			res := search.Search(cmd.Context(), a.Sessions(), contexts, args[0], search.Options{
				Kinds:   parsed,
				Limit:   limit,
				Timeout: a.Timeout,
			})
			if a.json() {
				return writeJSON(a.out(), searchJSON(res))
			}
			printSearch(a, res)
			if len(res.Matches) == 0 && len(res.Failures) > 0 {
				return fmt.Errorf("no vCenter could be searched")
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "restrict to these kinds: vm, template, host, cluster, datastore, network")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of matches to show")
	return cmd
}

func printSearch(a *App, res *search.Results) {
	out := a.out()
	if len(res.Matches) == 0 {
		fmt.Fprintf(out, "No matches for %q in %d vCenter(s).\n", res.Query, res.Searched)
	} else {
		t := newTable(out, "VCENTER", "TYPE", "NAME", "DATACENTER", "PATH")
		for _, m := range res.Matches {
			t.row(m.Context, string(m.Kind), m.Name, dash(m.Datacenter), dash(m.Path))
		}
		t.flush()
		fmt.Fprintf(out, "\n%d match(es) across %d vCenter(s) in %s\n", len(res.Matches), res.Searched, humanDuration(res.Elapsed))
	}
	for _, f := range res.Failures {
		fmt.Fprintf(a.errOut(), "%s %s: %v\n", glyphFail, f.Context, f.Err)
	}
}

func searchJSON(res *search.Results) map[string]any {
	matches := make([]map[string]any, 0, len(res.Matches))
	for _, m := range res.Matches {
		matches = append(matches, map[string]any{
			"context":     m.Context,
			"kind":        string(m.Kind),
			"name":        m.Name,
			"datacenter":  m.Datacenter,
			"path":        m.Path,
			"description": m.Description,
		})
	}
	failures := make([]map[string]any, 0, len(res.Failures))
	for _, f := range res.Failures {
		failures = append(failures, map[string]any{"context": f.Context, "error": f.Err.Error()})
	}
	return map[string]any{
		"query":      res.Query,
		"matches":    matches,
		"failures":   failures,
		"searched":   res.Searched,
		"elapsed_ms": res.Elapsed.Milliseconds(),
	}
}
