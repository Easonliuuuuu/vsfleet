package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/session"
)

func newClusterInfoCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster-info",
		Short: "Show each context's connection: route, state, latency and vCenter",
		Example: `  # Every configured context
  vsfleet cluster-info

  # The whole estate, as JSON for monitoring
  vsfleet cluster-info -o json

  # Two named contexts, with a shorter timeout
  vsfleet cluster-info -n prod -n dr --timeout 5s`,
		Long: `Connect to each context and report what happened, the way "kubectl
cluster-info" summarizes a cluster. With no -n, every configured context is
shown.

One unreachable environment is reported as one failed row. It never prevents
the others from being shown, which is the whole point of keeping routes and
credentials per context. "vsfleet doctor" diagnoses a context that fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			contexts, err := cfg.Resolve(a.ContextNames, a.AllContexts || len(a.ContextNames) == 0)
			if err != nil {
				return err
			}
			mgr := a.Sessions()
			mgr.ConnectAll(cmd.Context(), contexts)
			statuses := mgr.Statuses()

			if a.json() {
				return writeJSON(a.out(), statusPayload(statuses))
			}

			t := newTable(a.out(), "", "CONTEXT", "ROUTE", "STATUS", "LATENCY", "VCENTER")
			for _, s := range statuses {
				t.row(stateGlyph(s.State), s.Name, s.Route, s.State.String(), humanDuration(s.Latency), dash(s.Version))
			}
			t.flush()

			var failures int
			for _, s := range statuses {
				if s.Err != nil {
					failures++
				}
			}
			if failures > 0 {
				fmt.Fprintln(a.out())
				for _, s := range statuses {
					if s.Err != nil {
						fmt.Fprintf(a.out(), "%s %s: %v\n", glyphFail, s.Name, s.Err)
					}
				}
			}
			return nil
		},
	}
	cmd.AddCommand(newClusterInfoDumpCommand(a))
	return cmd
}

func statusPayload(statuses []session.Status) []map[string]any {
	payload := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		m := map[string]any{
			"context":  s.Name,
			"endpoint": s.Endpoint,
			"route":    s.Route,
			"state":    s.State.String(),
		}
		if s.Version != "" {
			m["vcenter"] = s.Version
		}
		if s.Latency > 0 {
			m["latency_ms"] = s.Latency.Milliseconds()
		}
		if s.Err != nil {
			m["error"] = s.Err.Error()
		}
		payload = append(payload, m)
	}
	return payload
}

// clusterDump is what "cluster-info dump" writes.
type clusterDump struct {
	Contexts []map[string]any `json:"contexts"`
	// Kinds holds every live kind's listing, keyed by plural.
	Kinds map[string]any `json:"kinds"`
	// Errors names the kinds that could not be listed, with why.
	Errors map[string]string `json:"errors,omitempty"`
}

func newClusterInfoDumpCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Write every live kind on the selected contexts as one JSON document",
		Long: strings.TrimSpace(`
Write the connection summary and every kind "vsfleet get" lists, for the
selected contexts, as one JSON document, the way "kubectl cluster-info dump"
does. It is the evidence to attach to a bug report or to diff between two
days. The output is always JSON; it is large on a big estate.

A kind that cannot be listed is recorded under "errors" rather than failing
the dump. Reads only, like every vsfleet command.`),
		Example: `  # Everything on the current context, to a file
  vsfleet cluster-info dump > estate.json

  # Everything on every context
  vsfleet cluster-info dump -A > estate.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			contexts, err := a.Contexts()
			if err != nil {
				return err
			}
			mgr := a.Sessions()
			mgr.ConnectAll(cmd.Context(), contexts)
			dump := clusterDump{Contexts: statusPayload(mgr.Statuses()), Kinds: map[string]any{}}
			for _, k := range apiresource.All() {
				l, ok := listers[k.Name]
				if !ok {
					continue
				}
				listing, _, err := l.list(cmd.Context(), a, nil, getOptions{})
				if err != nil {
					if dump.Errors == nil {
						dump.Errors = map[string]string{}
					}
					dump.Errors[k.Plural] = err.Error()
					continue
				}
				dump.Kinds[k.Plural] = listing.raw
			}
			return writeJSON(a.out(), dump)
		},
	}
}

func stateGlyph(s session.ConnectionState) string {
	switch s {
	case session.Connected:
		return glyphOnline
	case session.Connecting:
		return glyphPending
	case session.Failed:
		return glyphFail
	default:
		return glyphOffline
	}
}
