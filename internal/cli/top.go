package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/perf"
)

func newTopCommand(a *App) *cobra.Command {
	var window int64
	cmd := &cobra.Command{
		Use:   "top vm [NAME...]",
		Short: "Show VM CPU and memory use from stored performance collections",
		Long: strings.TrimSpace(`
Show per-VM CPU and memory use, the way "kubectl top" does, read from the
bounded performance collections "vsfleet capture perf" stores.
It never contacts a vCenter.

Each VM shows the average, peak and 95th percentile of interval averages, the
sample count behind them, and a conservative sizing signal. A dash means
unknown, never zero. The 95th percentile appears only when enough samples
support it. Signals other than "in-use" and "sustained-low" explain
themselves below the table.

By default the newest collection is read: the newest of each context named
with -n, the newest of every context with -A, else the newest overall.
--window reads one collection by the ID "vsfleet get perfcollections" shows.
NAME narrows the VMs, as an exact name or a glob such as 'web*'.

Only VMs are collected, so "top host" is not available yet.`),
		Example: `  # CPU and memory for every VM in the newest collection
  vsfleet top vm

  # The newest collection of every context
  vsfleet top vm -A

  # A few VMs from an older collection
  vsfleet top vm 'web*' --window 3

  # As JSON, for a script
  vsfleet top vm -o json`,
		Args:      cobra.MinimumNArgs(1),
		ValidArgs: []string{"vm", "vms"},
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := lookupKind(args[0])
			if err != nil {
				return err
			}
			switch kind.Name {
			case "vm":
			case "host":
				return errors.New("performance collections sample VMs only, so host use is not available; run \"vsfleet top vm\"")
			default:
				return fmt.Errorf("top reads VMs only, not %s", kind.Plural)
			}
			patterns, err := newNamePatterns(args[1:])
			if err != nil {
				return err
			}
			s, err := a.History()
			if err != nil {
				return err
			}
			windows, err := topWindows(cmd.Context(), a, s, window)
			if err != nil {
				return err
			}
			for i := range windows {
				windows[i].VMs = filterSlice(windows[i].VMs, func(vm perf.VMResult) bool {
					return patterns.match(vm.Name, vm.InstanceUUID)
				})
			}
			if a.json() {
				if len(windows) == 1 {
					err = writeJSON(a.out(), windows[0])
				} else {
					err = writeJSON(a.out(), windows)
				}
				if err != nil {
					return err
				}
			} else {
				for i, w := range windows {
					if i > 0 {
						fmt.Fprintln(a.out())
					}
					printTopVMs(a, w)
				}
			}
			return missingError([]*kindListing{{kind: kind, missing: patterns.missing()}})
		},
	}
	cmd.Flags().Int64Var(&window, "window", 0, "read this collection, by ID, instead of the newest")
	return cmd
}

// topWindows loads the collections "top" reads; see its help for the rule.
func topWindows(ctx context.Context, a *App, s *assessment.Store, id int64) ([]perf.Window, error) {
	if id != 0 {
		w, err := s.LoadPerfWindow(ctx, id)
		if err != nil {
			return nil, err
		}
		return []perf.Window{w}, nil
	}
	var newest []perf.Window
	switch names := a.StoredContextNames(); {
	case a.AllContexts:
		all, err := s.PerfWindows(ctx, "", 1000)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, w := range all {
			if !seen[w.Context] {
				seen[w.Context] = true
				newest = append(newest, w)
			}
		}
	case len(names) > 0:
		for _, name := range names {
			ws, err := s.PerfWindows(ctx, name, 1)
			if err != nil {
				return nil, err
			}
			if len(ws) == 0 {
				return nil, fmt.Errorf("no performance collections are stored for context %q; run \"vsfleet capture perf -n %s\"", name, name)
			}
			newest = append(newest, ws[0])
		}
	default:
		ws, err := s.PerfWindows(ctx, "", 1)
		if err != nil {
			return nil, err
		}
		newest = ws
	}
	if len(newest) == 0 {
		return nil, errors.New("no performance collections are stored; run \"vsfleet capture perf\"")
	}
	out := make([]perf.Window, 0, len(newest))
	for _, w := range newest {
		full, err := s.LoadPerfWindow(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, full)
	}
	return out, nil
}

func printTopVMs(a *App, w perf.Window) {
	printPerfWindowSummary(a, w)
	fmt.Fprintln(a.out())
	t := newTable(a.out(), "VM", "SIGNAL", "CPU% AVG", "PEAK", "P95", "ACTIVE MiB AVG", "PEAK", "READY% PEAK", "BALLOON MiB", "SWAP MiB", "SWAP-IN KBps", "SAMPLES")
	derived := false
	for _, vm := range w.VMs {
		cpu := perfSummary(vm, perf.CPUUsage)
		memAvg, memPeak, fromUsage := perfActiveMiB(vm)
		derived = derived || fromUsage
		t.row(vm.Name, string(vm.Signal), perfNum(cpu.Average), perfNum(cpu.Peak), perfNum(cpu.P95),
			memAvg, memPeak, perfNum(perfSummary(vm, perf.CPUReady).Peak),
			perfNum(perfSummary(vm, perf.MemBalloon).Peak), perfNum(perfSummary(vm, perf.MemSwapped).Peak),
			perfNum(perfSummary(vm, perf.MemSwapinRate).Peak), perfSamples(cpu))
	}
	t.flush()
	if derived {
		fmt.Fprintf(a.out(), "* active memory from %s × configured memory, as %s is not kept\n", perf.MemUsage, perf.MemActive)
	}
	notes := false
	for _, vm := range w.VMs {
		switch vm.Signal {
		case perf.SignalInUse, perf.SignalSustainedLow:
			continue
		}
		if !notes {
			fmt.Fprintln(a.out(), "\nNotes:")
			notes = true
		}
		fmt.Fprintf(a.out(), "  %s: %s\n", vm.Name, dash(vm.SignalReason))
	}
}
