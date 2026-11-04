package health

import (
	"fmt"
	"sort"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

type haCluster struct {
	resource assessment.ResourceObservation
	cluster  vsphere.Cluster
}

// haClusters decodes the clusters with HA on, the only ones whose failover
// capacity means anything.
func haClusters(data assessment.ExportData) []haCluster {
	var out []haCluster
	for _, resource := range data.Resources {
		if resource.Kind != "cluster" {
			continue
		}
		var c vsphere.Cluster
		if !assessment.DecodeResource(resource, &c) || c.Standalone || !c.HAEnabled || c.HA == nil {
			continue
		}
		out = append(out, haCluster{resource, c})
	}
	return out
}

// evaluateHAFailoverCapacity flags a cluster whose current failover
// capacity is below what its policy keeps. With admission control on,
// vCenter refuses power-ons and new VMs until capacity returns, so the
// finding is critical; with it off, power-ons proceed but HA can no longer
// restart every VM after the failures it was sized for.
func evaluateHAFailoverCapacity(in Input, emit func(Finding)) {
	for _, hc := range haClusters(in.Data) {
		state := hc.cluster.HA.Failover()
		if len(state.Short) == 0 {
			continue
		}
		c := hc.cluster
		obj := resourceObject(in.Data, hc.resource, "cluster", c.Name, c.ID, c.Datacenter)
		var parts []string
		var evidence []Evidence
		for _, s := range state.Short {
			if s.Resource == "host failures" {
				parts = append(parts, fmt.Sprintf("tolerates %d of %d host failures", s.Current, s.Configured))
				evidence = append(evidence, Evidence{Field: "host_failures_tolerated", Observed: fmt.Sprint(s.Current), Expected: fmt.Sprint(s.Configured)})
				continue
			}
			parts = append(parts, fmt.Sprintf("%s %d%% free for failover, %d%% reserved", s.Resource, s.Current, s.Configured))
			evidence = append(evidence, Evidence{Field: strings.ToLower(s.Resource) + "_failover_percent", Observed: fmt.Sprint(s.Current), Expected: fmt.Sprint(s.Configured)})
		}
		evidence = append(evidence, Evidence{Field: "admission_control", Observed: fmt.Sprint(c.HA.AdmissionControl)})
		severity, effect := SeverityCritical, "vCenter refuses power-ons and new VMs"
		if !state.Refusing {
			severity, effect = SeverityWarning, "admission control is off, so HA cannot restart every VM after a host failure"
		}
		emit(Finding{Rule: "ha-failover-capacity-low", Severity: severity, Object: obj,
			Message:  fmt.Sprintf("HA failover capacity is below the reserve (%s): %s", strings.Join(parts, "; "), effect),
			Evidence: evidence})
	}
}

// resolveHAFailoverCapacity reports the rule unknown when no cluster failed
// and some cluster's policy has capacity to compare but vCenter reported
// none, rather than passing clusters it could not judge.
func resolveHAFailoverCapacity(in Input) (string, string, []string) {
	unknown := make(map[string]bool)
	for _, hc := range haClusters(in.Data) {
		state := hc.cluster.HA.Failover()
		if len(state.Short) > 0 {
			return "", "", nil
		}
		if !state.Known && hc.cluster.HA.Policy == vsphere.HAPolicyResources {
			unknown[hc.resource.Context] = true
		}
	}
	if len(unknown) == 0 {
		return "", "", nil
	}
	blind := make([]string, 0, len(unknown))
	for context := range unknown {
		blind = append(blind, context)
	}
	sort.Strings(blind)
	return "unknown", "vCenter reported no current failover capacity", blind
}
