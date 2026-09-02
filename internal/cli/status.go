package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/session"
)

func newStatusCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show connection status for every context",
		Long: `Connect to every configured context and report what happened.

One unreachable environment is reported as one failed row. It never prevents
the others from being shown, which is the whole point of keeping routes and
credentials per context.`,
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
				return writeJSON(a.out(), payload)
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
