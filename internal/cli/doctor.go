package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vcfleet/internal/vsphere"
)

func newDoctorCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [context...]",
		Short: "Diagnose the path to one or more vCenters",
		Long: `Walk every stage of the connection to a vCenter and report where it stops.

Stages are checked in order — configuration, credential, route, proxy, DNS,
TCP, TLS, authentication, API — so the output names the actual fault instead
of reporting that something, somewhere, did not work.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			names := args
			if len(names) == 0 {
				names = a.ContextNames
			}
			contexts, err := cfg.Resolve(names, a.AllContexts)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			reports := make([]*vsphere.Diagnosis, 0, len(contexts))
			failed := 0
			for i, cc := range contexts {
				d, client := vsphere.Diagnose(ctx, cc, a.ConnectOptions())
				if client != nil {
					_ = client.Close(context.WithoutCancel(ctx))
				}
				reports = append(reports, d)
				if !d.OK() {
					failed++
				}
				if !a.json() {
					if i > 0 {
						fmt.Fprintln(a.out())
					}
					fmt.Fprintf(a.out(), "Context: %s\n", cc.Name)
					printDiagnosis(a, d, true)
				}
			}
			if a.json() {
				payload := make([]any, 0, len(reports))
				for _, d := range reports {
					payload = append(payload, diagnosisJSON(d))
				}
				if err := writeJSON(a.out(), payload); err != nil {
					return err
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d contexts failed", failed, len(contexts))
			}
			return nil
		},
	}
}

// printDiagnosis renders the stage-by-stage checklist.
func printDiagnosis(a *App, d *vsphere.Diagnosis, withSummary bool) {
	out := a.out()
	t := newTable(out)
	for _, c := range d.Checks {
		glyph := glyphSkip
		switch c.Status {
		case vsphere.CheckPass:
			glyph = glyphOK
		case vsphere.CheckFail:
			glyph = glyphFail
		}
		detail := c.Detail
		if c.Status == vsphere.CheckFail && c.Err != nil {
			detail = c.Err.Error()
		}
		if c.Status == vsphere.CheckSkip && detail == "" {
			detail = "not reached"
		}
		t.row(glyph, c.Name, detail)
	}
	t.flush()
	if !withSummary {
		return
	}
	if d.OK() {
		f := newFields(out)
		f.add("vCenter", d.About.FullVersion())
		f.add("Latency", humanDuration(d.Latency))
		f.flush()
	}
}

// diagnosisJSON is the machine-readable shape of a diagnosis. Errors are
// rendered as strings because an error value does not survive JSON.
func diagnosisJSON(d *vsphere.Diagnosis) map[string]any {
	checks := make([]map[string]any, 0, len(d.Checks))
	for _, c := range d.Checks {
		m := map[string]any{
			"name":   c.Name,
			"status": string(c.Status),
		}
		if c.Detail != "" {
			m["detail"] = c.Detail
		}
		if c.Err != nil {
			m["error"] = c.Err.Error()
		}
		if c.Duration > 0 {
			m["duration_ms"] = c.Duration.Milliseconds()
		}
		checks = append(checks, m)
	}
	out := map[string]any{
		"context":  d.Context,
		"endpoint": d.Endpoint,
		"route":    d.Route,
		"tls":      d.TLS,
		"ok":       d.OK(),
		"checks":   checks,
	}
	if d.Thumbprint != "" {
		out["thumbprint"] = d.Thumbprint
	}
	if d.About.Version != "" {
		out["vcenter"] = d.About.FullVersion()
		out["version"] = d.About.Version
	}
	if d.Latency > 0 {
		out["latency_ms"] = d.Latency.Milliseconds()
	}
	return out
}
