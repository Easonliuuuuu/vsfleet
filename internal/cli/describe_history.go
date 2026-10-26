package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// historyFlags are describe's --history and the flags that shape it.
type historyFlags struct {
	on              bool
	allObservations bool
	includeRuntime  bool
}

func (h *historyFlags) add(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.BoolVar(&h.on, "history", false, "show how a VM changed across every stored capture")
	fl.BoolVar(&h.allObservations, "all-observations", false, "with --history, include captures in which nothing changed")
	fl.BoolVar(&h.includeRuntime, "include-runtime", false, "with --history, include volatile runtime fields in changes")
}

// check rejects flag combinations before anything is read.
func (h historyFlags) check(cmd *cobra.Command, run string) error {
	switch {
	case !h.on && (cmd.Flags().Changed("all-observations") || cmd.Flags().Changed("include-runtime")):
		return errors.New("--all-observations and --include-runtime apply with --history")
	case h.on && run != "":
		return errors.New("--history reads every capture; --run picks one")
	}
	return nil
}

// describeHistory prints one VM's change log across the stored captures. It
// finds the VM by name or UUID the way "describe vm" does, and reads stored
// evidence only.
func describeHistory(ctx context.Context, a *App, reqs []resourceRequest, h historyFlags) error {
	if len(reqs) != 1 || len(reqs[0].names) != 1 || (reqs[0].kind.Name != "vm" && reqs[0].kind.Name != "template") {
		return errors.New("--history describes one VM at a time: \"vsfleet describe vm NAME --history\"")
	}
	query := reqs[0].names[0]
	s, err := a.History()
	if err != nil {
		return err
	}
	events, err := s.TimelineForContexts(ctx, query, a.captureContexts(), h.allObservations, h.includeRuntime)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		if a.captureContexts() == nil {
			return fmt.Errorf("no stored VM history matched %q", query)
		}
		return fmt.Errorf("no stored VM history matched %q on the selected contexts; -A searches every captured context", query)
	}
	if a.json() {
		return writeJSON(a.out(), events)
	}
	t := newTable(a.out(), "RUN", "DATE", "EVENT", "CONTEXT", "VM", "DETAIL")
	for _, e := range events {
		detail := ""
		if len(e.Changes) > 0 {
			parts := make([]string, len(e.Changes))
			for i, f := range e.Changes {
				parts[i] = f.Field + ":" + f.Before + "→" + f.After
			}
			detail = strings.Join(parts, " ")
		}
		if detail == "" && e.Observation != nil {
			detail = dash(e.Observation.VM.Host)
		}
		t.row(strconv.FormatInt(e.Run.ID, 10), e.Run.StartedAt.Local().Format("2006-01-02 15:04"), e.Kind, e.Context, e.Name, detail)
	}
	t.flush()
	return nil
}
