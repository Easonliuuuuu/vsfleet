package assessment

import (
	"fmt"
	"sort"
	"strings"
)

// contextVMs is one context's comparable VMs in one capture.
type contextVMs struct {
	name string
	vms  []storedVM
}

// vcenterVMs reduces the contexts of one vCenter, on both sides of a diff, to
// one observation per VM per side. Several contexts can reach one vCenter, for
// example with different accounts, and each records the same VMs; without
// this a VM would be compared once per context and every extra context on one
// side would read as appeared or vanished.
//
// When the two sides share context names, only those contexts are compared,
// and a context recorded on one side only is a coverage warning. When they
// share none, as after a context is renamed, every context is used. A VM seen
// by several contexts is taken from the first context, by name, that recorded
// it on both sides, so both sides see it through the same account where they
// can.
func vcenterVMs(base, target []contextVMs, d *Diff) ([]storedVM, []storedVM) {
	shared := make(map[string]bool)
	baseNames := make(map[string]bool, len(base))
	for _, c := range base {
		baseNames[contextKey(c.name)] = true
	}
	for _, c := range target {
		if baseNames[contextKey(c.name)] {
			shared[contextKey(c.name)] = true
		}
	}
	if len(shared) > 0 {
		base = sharedContexts(base, shared, "baseline", d)
		target = sharedContexts(target, shared, "target", d)
	}
	baseSeen, targetSeen := vmContexts(base), vmContexts(target)
	pick := func(side []contextVMs, seen, other map[string][]string) []storedVM {
		var out []storedVM
		for _, c := range side {
			for _, vm := range c.vms {
				names := seen[vm.observation.VM.ID]
				if chosenContext(names, other[vm.observation.VM.ID]) == c.name {
					out = append(out, vm)
				}
			}
		}
		return out
	}
	return pick(base, baseSeen, targetSeen), pick(target, targetSeen, baseSeen)
}

// sharedContexts keeps the contexts both sides recorded and warns about the
// rest.
func sharedContexts(side []contextVMs, shared map[string]bool, scope string, d *Diff) []contextVMs {
	var out []contextVMs
	for _, c := range side {
		if shared[contextKey(c.name)] {
			out = append(out, c)
			continue
		}
		msg := fmt.Sprintf("%s was recorded only in %s: its VMs are not compared", c.name, scope)
		d.Warnings = append(d.Warnings, msg)
		d.Coverage = append(d.Coverage, CoverageIssue{Scope: scope, Context: c.name, Message: msg})
	}
	return out
}

// vmContexts maps each VM moref to the sorted names of the contexts that
// recorded it.
func vmContexts(side []contextVMs) map[string][]string {
	out := make(map[string][]string)
	for _, c := range side {
		for _, vm := range c.vms {
			out[vm.observation.VM.ID] = append(out[vm.observation.VM.ID], c.name)
		}
	}
	for _, names := range out {
		sort.Strings(names)
	}
	return out
}

// chosenContext returns the first of names that other also holds, or the
// first of names when they hold none in common.
func chosenContext(names, other []string) string {
	for _, name := range names {
		for _, o := range other {
			if contextKey(o) == contextKey(name) {
				return name
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func contextKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }
