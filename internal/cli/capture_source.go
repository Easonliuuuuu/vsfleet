package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// errNoContextAnswered is gather's error when every context failed. Commands
// that can read a capture instead say so; none of them does it on its own.
var errNoContextAnswered = errors.New("no context could be queried")

// suggestRun adds the stored alternative to a read that reached no vCenter.
func suggestRun(err error) error {
	if errors.Is(err, errNoContextAnswered) {
		return fmt.Errorf("%w; to read the most recent capture instead, add --run latest", err)
	}
	return err
}

// loadCapture reads the capture --run names, for get and describe to list
// from in place of vCenter. It touches no configuration beyond the current
// context and connects to nothing.
func (a *App) loadCapture(ctx context.Context, selector string) error {
	s, err := a.History()
	if err != nil {
		return err
	}
	id, err := s.ResolveRunForContexts(ctx, selector, a.captureContexts())
	if err != nil {
		return fmt.Errorf("--run: %w", err)
	}
	data, err := s.LoadExportDataForContexts(ctx, id, a.captureContexts())
	if errors.Is(err, assessment.ErrUnknownStoredContext) {
		return fmt.Errorf("capture %d: %w; -A reads every context it recorded", id, err)
	}
	if err != nil {
		return err
	}
	a.capture = &data
	return nil
}

// captureContexts is the context selector for a --run read. --run changes
// where get and describe read from, never which contexts: -A spans every
// context the capture recorded, -n names them, and otherwise the current
// context is read, or every recorded one when none is current.
func (a *App) captureContexts() []string {
	if a.AllContexts {
		return nil
	}
	if len(a.ContextNames) > 0 {
		return a.ContextNames
	}
	if cfg, err := a.Config(); err == nil && cfg.CurrentContext != "" {
		return []string{cfg.CurrentContext}
	}
	return nil
}

// read lists one kind from live vCenters, or from the capture --run loaded.
// A context whose capture did not record the kind is reported the way an
// unreachable vCenter is, so an empty answer is never mistaken for none.
func read[T any](ctx context.Context, a *App, kind string, live func(context.Context, *vsphere.Client) ([]T, error), stored func(*assessment.ExportData) ([]T, error)) ([]T, []contextFailure, error) {
	if a.capture == nil {
		items, failures, err := gather(ctx, a, live)
		return items, failures, suggestRun(err)
	}
	items, err := stored(a.capture)
	if err != nil {
		return nil, nil, err
	}
	return items, unrecorded(a.capture, kind), nil
}

// unrecorded lists the contexts whose capture holds no complete collection
// of kind.
func unrecorded(data *assessment.ExportData, kind string) []contextFailure {
	var out []contextFailure
	for _, c := range data.Contexts {
		var found *assessment.CollectionRun
		for i := range c.Collections {
			if c.Collections[i].Kind == kind {
				found = &c.Collections[i]
			}
		}
		plural := apiresource.Get(kind).Plural
		switch {
		case found == nil:
			out = append(out, contextFailure{Context: c.Name, Err: fmt.Errorf("capture %d did not collect %s", data.Run.ID, plural)})
		case found.Status == "failed":
			out = append(out, contextFailure{Context: c.Name, Err: fmt.Errorf("capture %d could not collect %s: %s", data.Run.ID, plural, found.Error)})
		}
	}
	return out
}

// storedVMs returns a capture's VMs, or its templates, with their snapshots.
func storedVMs(templates bool) func(*assessment.ExportData) ([]vsphere.VM, error) {
	return func(data *assessment.ExportData) ([]vsphere.VM, error) {
		var out []vsphere.VM
		for _, e := range data.VMs {
			vm := e.Observation.VM
			if vm.IsTemplate != templates {
				continue
			}
			vm.Snapshots = e.Snapshots
			out = append(out, vm)
		}
		return out, nil
	}
}

// storedObjects decodes a capture's resources of one kind into the type a
// live read returns; a capture stores exactly that type's JSON.
func storedObjects[T any](kind vsphere.Kind) func(*assessment.ExportData) ([]T, error) {
	return func(data *assessment.ExportData) ([]T, error) {
		var out []T
		for _, r := range data.Resources {
			if r.Kind != string(kind) {
				continue
			}
			var v T
			if err := json.Unmarshal(r.Payload, &v); err != nil {
				return nil, fmt.Errorf("capture %d %s %q: %w", data.Run.ID, kind, r.Name, err)
			}
			out = append(out, v)
		}
		return out, nil
	}
}

// refuseRun gives a live-only command a hidden --run that explains why it
// does not apply, rather than leaving cobra to call the flag unknown.
func refuseRun(cmd *cobra.Command, why string) *cobra.Command {
	var run string
	cmd.Flags().StringVar(&run, "run", "", "not supported: "+why)
	_ = cmd.Flags().MarkHidden("run")
	next := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if c.Flags().Changed("run") {
			return fmt.Errorf("%q does not take --run: %s", c.CommandPath(), why)
		}
		return next(c, args)
	}
	return cmd
}
