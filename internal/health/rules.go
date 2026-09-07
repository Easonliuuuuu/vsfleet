package health

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// rules is deliberately a single registry: the CLI, coverage sheet, and
// evaluator all use the same IDs and ordering.
var rules = []Rule{
	{
		ID: "cdrom-connected", Category: CategoryMigration, Severity: SeverityWarning, MinSchema: 6,
		Recommendation: "Disconnect the virtual CD-ROM or remove its ISO backing before migration.", NeedsCollections: []string{"vm"},
		Summary: "a virtual CD-ROM is currently connected", Needs: "VM CD-ROM connection inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("cdrom-connected", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "cluster-network-inconsistent", Category: CategoryMigration, Severity: SeverityWarning, MinSchema: 9,
		Recommendation: "Align the standard vSwitch MTU and uplink configuration across hosts in the cluster.", NeedsCollections: []string{"host"},
		Summary: "hosts in a cluster disagree on standard vSwitch configuration", Needs: "host virtual-switch inventory",
		Eval: evaluateClusterNetworkInconsistent,
	},
	{
		ID: "datastore-inaccessible", Category: CategoryCapacity, Severity: SeverityCritical,
		Recommendation: "Restore datastore connectivity or evacuate its workloads before migration.", NeedsCollections: []string{"datastore"},
		Summary: "datastore is not accessible", Needs: "datastore inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, resource := range in.Data.Resources {
				if resource.Kind != "datastore" {
					continue
				}
				var datastore vsphere.Datastore
				if !decodeResource(resource, &datastore) || datastore.Accessible {
					continue
				}
				obj := resourceObject(in.Data, resource, "datastore", datastore.Name, datastore.ID, datastore.Datacenter)
				emit(Finding{Rule: "datastore-inaccessible", Severity: SeverityCritical, Object: obj,
					Message: "Datastore is inaccessible", Evidence: []Evidence{{Field: "accessible", Observed: "false", Expected: "true"}}})
			}
		},
	},
	{
		ID: "datastore-space-low", Category: CategoryCapacity, Severity: SeverityWarning,
		Recommendation: "Free space or expand the datastore before migration.", NeedsCollections: []string{"datastore"},
		Summary: "datastore free space is below the configured floor", Needs: "datastore capacity and free-space inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("datastore-space-low", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "datastore-zombie-vmdk", Category: CategoryCapacity, Severity: SeverityWarning,
		Recommendation: "Validate the file is unused, then archive or remove the unreferenced VMDK.", NeedsCollections: []string{"vm", "datastore"},
		Summary: "datastore contains an unreferenced virtual disk", Needs: "a capture run with --browse-datastores",
		Skip: func(in Input) (bool, string) {
			for _, resource := range in.Data.Resources {
				if resource.Kind != "datastore" {
					continue
				}
				var datastore vsphere.Datastore
				if decodeResource(resource, &datastore) && datastore.BrowseStatus == "success" {
					return false, ""
				}
			}
			return true, "a capture run with --browse-datastores"
		},
		Eval: func(in Input, emit func(Finding)) {
			for _, orphan := range Orphans(in.Data).Entries {
				if orphan.Confidence == ConfidenceUnknown {
					continue
				}
				severity := SeverityInfo
				if orphan.Confidence == ConfidenceVerified {
					severity = SeverityWarning
				}
				evidence := []Evidence{
					{Field: "confidence", Observed: string(orphan.Confidence)},
					{Field: "path", Observed: orphan.Path, Expected: "referenced by a VM or template"},
					{Field: "size", Observed: humanize.Bytes(orphan.SizeBytes)},
				}
				if !orphan.Modified.IsZero() {
					evidence = append(evidence, Evidence{Field: "last_modified", Observed: orphan.Modified.UTC().Format(time.RFC3339)})
				}
				if len(orphan.CheckedContexts) > 0 {
					evidence = append(evidence, Evidence{Field: "checked_contexts", Observed: strings.Join(orphan.CheckedContexts, ", ")})
				}
				message := fmt.Sprintf("%s VMDK %q (%s)", orphanConfidenceLabel(orphan.Confidence), orphan.Path, humanize.Bytes(orphan.SizeBytes))
				if len(orphan.ReferencedBy) > 0 {
					message += "; referenced by " + orphan.ReferencedBy[0].VM + " @ " + orphan.ReferencedBy[0].Context
				}
				emit(Finding{Rule: "datastore-zombie-vmdk", Severity: severity, Confidence: orphan.Confidence, Object: orphan.Object, Message: message, Evidence: evidence})
			}
		},
		Resolve: func(in Input) (string, string, []string) {
			unknown := false
			blindSet := make(map[string]bool)
			for _, orphan := range Orphans(in.Data).Entries {
				if orphan.Confidence != ConfidenceUnknown {
					continue
				}
				unknown = true
				for _, blind := range orphan.Blind {
					blindSet[blind.Context] = true
				}
			}
			if !unknown {
				return "", "", nil
			}
			blind := make([]string, 0, len(blindSet))
			for context := range blindSet {
				blind = append(blind, context)
			}
			sort.Strings(blind)
			return "unknown", "orphan coverage is incomplete", blind
		},
	},
	{
		ID: "dvportgroup-promiscuous", Category: CategorySecurity, Severity: SeverityWarning, MinSchema: 10,
		Recommendation: "Disable promiscuous mode, forged transmits, and MAC changes unless explicitly required.", NeedsCollections: []string{"dvswitch"},
		Summary: "a distributed port group permits insecure frame policies", Needs: "distributed port-group security inventory",
		Eval: evaluateDVPortGroupPromiscuous,
	},
	{
		ID: "dvswitch-host-coverage", Category: CategoryAvailability, Severity: SeverityWarning, MinSchema: 10,
		Recommendation: "Attach every host in the cluster to the distributed switch used by the migration network.", NeedsCollections: []string{"host", "dvswitch"},
		Summary: "a distributed switch does not cover every host in a cluster", Needs: "host and distributed-switch inventory",
		Eval: evaluateDVSwitchHostCoverage,
	},
	{
		ID: "guest-disk-space-low", Category: CategoryCapacity, Severity: SeverityWarning, MinSchema: 4,
		Recommendation: "Free space in the guest filesystem or expand its virtual disk before migration.", NeedsCollections: []string{"vm"},
		Summary: "guest filesystem free space is below the configured floor", Needs: "guest partition inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("guest-disk-space-low", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "host-disconnected", Category: CategoryAvailability, Severity: SeverityCritical,
		Recommendation: "Reconnect the host or remove it from the migration source estate.", NeedsCollections: []string{"host"},
		Summary: "host connection state is not connected", Needs: "host inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, resource := range in.Data.Resources {
				if resource.Kind != "host" {
					continue
				}
				var host vsphere.Host
				if !decodeResource(resource, &host) || host.ConnectionState == "connected" {
					continue
				}
				obj := resourceObject(in.Data, resource, "host", host.Name, host.ID, host.Datacenter)
				emit(Finding{Rule: "host-disconnected", Severity: SeverityCritical, Object: obj,
					Message: fmt.Sprintf("Host is %s", nonempty(host.ConnectionState, "not connected")), Evidence: []Evidence{{Field: "connection_state", Observed: nonempty(host.ConnectionState, "unknown"), Expected: "connected"}}})
			}
		},
	},
	{
		ID: "host-in-maintenance", Category: CategoryHygiene, Severity: SeverityInfo,
		Recommendation: "Complete host maintenance or account for it in the migration plan.", NeedsCollections: []string{"host"},
		Summary: "host is in maintenance mode", Needs: "host inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, resource := range in.Data.Resources {
				if resource.Kind != "host" {
					continue
				}
				var host vsphere.Host
				if !decodeResource(resource, &host) || !host.InMaintenance {
					continue
				}
				obj := resourceObject(in.Data, resource, "host", host.Name, host.ID, host.Datacenter)
				emit(Finding{Rule: "host-in-maintenance", Severity: SeverityInfo, Object: obj,
					Message: "Host is in maintenance mode", Evidence: []Evidence{{Field: "in_maintenance", Observed: "true", Expected: "false"}}})
			}
		},
	},
	{
		ID: "host-path-redundancy", Category: CategoryAvailability, Severity: SeverityWarning, MinSchema: 9,
		Recommendation: "Restore redundant active paths to the LUN and investigate dead paths before migration.", NeedsCollections: []string{"host"},
		Summary: "a host LUN has insufficient or dead paths", Needs: "host multipath inventory",
		Eval: evaluateHostPathRedundancy,
	},
	{
		ID: "portgroup-promiscuous", Category: CategorySecurity, Severity: SeverityWarning, MinSchema: 9,
		Recommendation: "Disable promiscuous mode, forged transmits, and MAC changes unless explicitly required.", NeedsCollections: []string{"host"},
		Summary: "a standard port group or vSwitch permits insecure frame policies", Needs: "host virtual-switch and port-group security inventory",
		Eval: evaluatePortGroupPromiscuous,
	},
	{
		ID: "snapshot-age", Category: CategoryHygiene, Severity: SeverityWarning,
		Recommendation: "Remove or consolidate the snapshot after confirming it is no longer needed.", NeedsCollections: []string{"vm"},
		Summary: "snapshot is at least the configured age", Needs: "VM snapshot inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("snapshot-age", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "tools-not-installed", Category: CategoryHygiene, Severity: SeverityWarning, MinSchema: 3,
		Recommendation: "Install VMware Tools before migration so guest state and quiescing are available.", NeedsCollections: []string{"vm"},
		Summary: "VMware Tools are not installed", Needs: "VMware Tools version inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("tools-not-installed", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "tools-not-running", Category: CategoryHygiene, Severity: SeverityWarning,
		Recommendation: "Start VMware Tools in the guest before migration.", NeedsCollections: []string{"vm"},
		Summary: "VMware Tools are not running on a powered-on VM", Needs: "VMware Tools running status",
		Eval: func(in Input, emit func(Finding)) {
			for _, item := range in.Data.VMs {
				vm := item.Observation.VM
				if vm.IsTemplate || vm.PowerState != "poweredOn" || vm.ToolsState == "guestToolsRunning" {
					continue
				}
				emit(Finding{Rule: "tools-not-running", Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("VMware Tools are not running (%s)", nonempty(vm.ToolsState, "unknown")), Evidence: []Evidence{{Field: "tools_state", Observed: nonempty(vm.ToolsState, "unknown"), Expected: "guestToolsRunning"}}})
			}
		},
	},
	{
		ID: "tools-outdated", Category: CategoryHygiene, Severity: SeverityWarning, MinSchema: 3,
		Recommendation: "Upgrade VMware Tools to a supported version before migration.", NeedsCollections: []string{"vm"},
		Summary: "VMware Tools version needs an upgrade", Needs: "VMware Tools version inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("tools-outdated", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "usb-connected", Category: CategoryMigration, Severity: SeverityWarning, MinSchema: 6,
		Recommendation: "Disconnect the virtual USB device or document its destination dependency before migration.", NeedsCollections: []string{"vm"},
		Summary: "a virtual USB device is currently connected", Needs: "VM USB connection inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("usb-connected", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "vm-inaccessible", Category: CategoryAvailability, Severity: SeverityCritical, MinSchema: 7,
		Recommendation: "Restore VM accessibility before migration.", NeedsCollections: []string{"vm"},
		Summary: "VM connection state is inaccessible", Needs: "VM connection state inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, item := range in.Data.VMs {
				vm := item.Observation.VM
				if vm.IsTemplate || !inaccessibleVMStates[vm.ConnectionState] {
					continue
				}
				emit(Finding{Rule: "vm-inaccessible", Severity: SeverityCritical, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("VM is %s", nonempty(vm.ConnectionState, "inaccessible")), Evidence: []Evidence{{Field: "connection_state", Observed: nonempty(vm.ConnectionState, "unknown"), Expected: "connected"}}})
			}
		},
	},
	{
		ID: "vm-orphaned", Category: CategoryAvailability, Severity: SeverityCritical, MinSchema: 7,
		Recommendation: "Reconnect the VM to a host or recover it before migration.", NeedsCollections: []string{"vm"},
		Summary: "VM is orphaned from every host", Needs: "VM connection state inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, item := range in.Data.VMs {
				vm := item.Observation.VM
				if vm.IsTemplate || vm.ConnectionState != "orphaned" {
					continue
				}
				emit(Finding{Rule: "vm-orphaned", Severity: SeverityCritical, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("VM is %s", nonempty(vm.ConnectionState, "orphaned")), Evidence: []Evidence{{Field: "connection_state", Observed: "orphaned", Expected: "connected"}}})
			}
		},
	},
}

var inaccessibleVMStates = map[string]bool{
	"inaccessible":  true,
	"invalid":       true,
	"disconnected":  true,
	"notResponding": true,
}

func nonempty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func attachedDeviceDetails(kind, path, device, host string) string {
	parts := make([]string, 0, 2)
	if kind != "" {
		parts = append(parts, kind)
	}
	if path != "" {
		parts = append(parts, path)
	} else if device != "" {
		parts = append(parts, device)
	}
	if host != "" {
		parts = append(parts, "host "+host)
	}
	if len(parts) == 0 {
		return "backing details unavailable"
	}
	return strings.Join(parts, ", ")
}

// evaluateRule supplies threshold-dependent rule bodies without putting
// mutable policy state in the package registry.
func evaluateRule(ruleID string, in Input, opts Options, emit func(Finding)) {
	thresholds := opts.Thresholds
	switch ruleID {
	case "datastore-space-low":
		if thresholds.DatastoreFreePct <= 0 {
			return
		}
		for _, resource := range in.Data.Resources {
			if resource.Kind != "datastore" {
				continue
			}
			var datastore vsphere.Datastore
			if !decodeResource(resource, &datastore) {
				continue
			}
			free, ok := freePct(datastore.CapacityBytes, datastore.FreeBytes)
			if !ok || free >= thresholds.DatastoreFreePct {
				continue
			}
			obj := resourceObject(in.Data, resource, "datastore", datastore.Name, datastore.ID, datastore.Datacenter)
			emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: obj,
				Message: fmt.Sprintf("Datastore has %s%% free space (%s of %s)", percent(free), humanize.Bytes(datastore.FreeBytes), humanize.Bytes(datastore.CapacityBytes)), Evidence: []Evidence{{Field: "free_percent", Observed: percent(free), Expected: percent(thresholds.DatastoreFreePct)}}})
		}
	case "guest-disk-space-low":
		if thresholds.GuestDiskFreePct <= 0 {
			return
		}
		for _, item := range in.Data.VMs {
			if item.Observation.VM.IsTemplate {
				continue
			}
			for _, partition := range item.Observation.VM.Partitions {
				free, ok := freePct(partition.CapacityBytes, partition.FreeBytes)
				if !ok || free >= thresholds.GuestDiskFreePct {
					continue
				}
				obj := vmObject(in.Data, item.Observation)
				label := partition.Path
				if label == "" {
					label = "guest filesystem"
				}
				emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: obj,
					Message: fmt.Sprintf("Guest filesystem %q has %s%% free space (%s of %s)", label, percent(free), humanize.Bytes(partition.FreeBytes), humanize.Bytes(partition.CapacityBytes)), Evidence: []Evidence{{Field: "free_percent", Observed: percent(free), Expected: percent(thresholds.GuestDiskFreePct)}, {Field: "filesystem", Observed: label}}})
			}
		}
	case "cdrom-connected":
		for _, item := range in.Data.VMs {
			if item.Observation.VM.IsTemplate {
				continue
			}
			for _, cdrom := range item.Observation.VM.CDROMs {
				if cdrom.Connected == nil || !*cdrom.Connected {
					continue
				}
				emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("CD-ROM %q is connected (%s)", nonempty(cdrom.Label, "unnamed CD-ROM"), attachedDeviceDetails(cdrom.BackingType, cdrom.BackingPath, cdrom.BackingDevice, cdrom.BackingHost)), Evidence: []Evidence{{Field: "connected", Observed: "true", Expected: "false"}, {Field: "backing", Observed: attachedDeviceDetails(cdrom.BackingType, cdrom.BackingPath, cdrom.BackingDevice, cdrom.BackingHost)}}})
			}
		}
	case "usb-connected":
		for _, item := range in.Data.VMs {
			if item.Observation.VM.IsTemplate {
				continue
			}
			for _, usb := range item.Observation.VM.USBs {
				if usb.Connected == nil || !*usb.Connected {
					continue
				}
				emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("USB device %q is connected (%s)", nonempty(usb.Label, "unnamed USB device"), attachedDeviceDetails(usb.BackingType, usb.BackingPath, usb.BackingDevice, usb.BackingHost)), Evidence: []Evidence{{Field: "connected", Observed: "true", Expected: "false"}, {Field: "backing", Observed: attachedDeviceDetails(usb.BackingType, usb.BackingPath, usb.BackingDevice, usb.BackingHost)}}})
			}
		}
	case "snapshot-age":
		if thresholds.SnapshotAge <= 0 {
			return
		}
		for _, item := range in.Data.VMs {
			if item.Observation.VM.IsTemplate {
				continue
			}
			finish := contextFinish(in.Data, contextName(item.Observation))
			if finish.IsZero() {
				continue
			}
			for _, snapshot := range item.Snapshots {
				if snapshot.CreateTime.IsZero() {
					continue
				}
				age := finish.Sub(snapshot.CreateTime)
				if age < thresholds.SnapshotAge {
					continue
				}
				emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("Snapshot %q is %s old (created %s)", snapshot.Name, ageText(age), snapshot.CreateTime.UTC().Format("2006-01-02")), Evidence: []Evidence{{Field: "snapshot_age_days", Observed: percent(age.Hours() / 24), Expected: percent(thresholds.SnapshotAge.Hours() / 24)}}})
			}
		}
	case "tools-not-installed":
		for _, item := range in.Data.VMs {
			vm := item.Observation.VM
			if vm.IsTemplate || vm.ToolsVersionStatus != "guestToolsNotInstalled" {
				continue
			}
			emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation), Message: "VMware Tools are not installed (guestToolsNotInstalled)", Evidence: []Evidence{{Field: "tools_version_status", Observed: "guestToolsNotInstalled", Expected: "installed"}}})
		}
	case "tools-outdated":
		outdated := map[string]bool{
			"guestToolsNeedUpgrade":  true,
			"guestToolsSupportedOld": true,
			"guestToolsTooOld":       true,
			"guestToolsBlacklisted":  true,
		}
		for _, item := range in.Data.VMs {
			vm := item.Observation.VM
			if vm.IsTemplate || !outdated[vm.ToolsVersionStatus] {
				continue
			}
			emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation), Message: fmt.Sprintf("VMware Tools are out of date (%s)", vm.ToolsVersionStatus), Evidence: []Evidence{{Field: "tools_version_status", Observed: vm.ToolsVersionStatus, Expected: "guestToolsCurrent"}}})
		}
	}
}

func evaluateClusterNetworkInconsistent(in Input, emit func(Finding)) {
	type switchConfig struct {
		resource assessment.ResourceObservation
		host     vsphere.Host
		switches vsphere.HostVSwitch
	}
	byCluster := make(map[string]map[string][]switchConfig)
	for _, resource := range in.Data.Resources {
		if resource.Kind != "host" {
			continue
		}
		var host vsphere.Host
		if !decodeResource(resource, &host) || host.Cluster == "" {
			continue
		}
		for _, sw := range host.VSwitches {
			if byCluster[resource.Context] == nil {
				byCluster[resource.Context] = make(map[string][]switchConfig)
			}
			key := host.Cluster + "\x00" + sw.Name
			byCluster[resource.Context][key] = append(byCluster[resource.Context][key], switchConfig{resource: resource, host: host, switches: sw})
		}
	}
	for _, switches := range byCluster {
		keys := make([]string, 0, len(switches))
		for key := range switches {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values := switches[key]
			sort.SliceStable(values, func(i, j int) bool { return values[i].host.Name < values[j].host.Name })
			if len(values) < 2 {
				continue
			}
			baseline := values[0].switches
			for _, value := range values[1:] {
				if value.switches.MTU == baseline.MTU && len(value.switches.Uplinks) == len(baseline.Uplinks) {
					continue
				}
				obj := resourceObject(in.Data, value.resource, "host", value.host.Name, value.host.ID, value.host.Datacenter)
				emit(Finding{Rule: "cluster-network-inconsistent", Object: obj,
					Message: fmt.Sprintf("Standard vSwitch %q differs from hosts in cluster %q", value.switches.Name, value.host.Cluster),
					Evidence: []Evidence{
						{Field: "switch", Observed: value.switches.Name},
						{Field: "mtu", Observed: fmt.Sprint(value.switches.MTU), Expected: fmt.Sprint(baseline.MTU)},
						{Field: "uplinks", Observed: fmt.Sprint(len(value.switches.Uplinks)), Expected: fmt.Sprint(len(baseline.Uplinks))},
					}})
			}
		}
	}
}

func evaluateDVSwitchHostCoverage(in Input, emit func(Finding)) {
	type hostRecord struct {
		resource assessment.ResourceObservation
		host     vsphere.Host
	}
	hosts := make(map[string][]hostRecord)
	for _, resource := range in.Data.Resources {
		if resource.Kind != "host" {
			continue
		}
		var host vsphere.Host
		if decodeResource(resource, &host) && host.Cluster != "" {
			hosts[resource.Context] = append(hosts[resource.Context], hostRecord{resource: resource, host: host})
		}
	}
	for _, resource := range in.Data.Resources {
		if resource.Kind != "dvswitch" {
			continue
		}
		var sw vsphere.DVSwitch
		if !decodeResource(resource, &sw) {
			continue
		}
		members := make(map[string]bool, len(sw.Hosts))
		for _, member := range sw.Hosts {
			members[member] = true
		}
		byCluster := make(map[string][]hostRecord)
		for _, host := range hosts[resource.Context] {
			byCluster[host.host.Cluster] = append(byCluster[host.host.Cluster], host)
		}
		clusters := make([]string, 0, len(byCluster))
		for cluster := range byCluster {
			clusters = append(clusters, cluster)
		}
		sort.Strings(clusters)
		for _, cluster := range clusters {
			clusterHosts := byCluster[cluster]
			memberNames := make([]string, 0, len(clusterHosts))
			clusterNames := make([]string, 0, len(clusterHosts))
			for _, host := range clusterHosts {
				clusterNames = append(clusterNames, host.host.Name)
				if members[host.host.Name] || members[host.host.ID] {
					memberNames = append(memberNames, host.host.Name)
				}
			}
			if len(memberNames) == len(clusterHosts) {
				continue
			}
			obj := Object{Kind: "cluster", Name: cluster, Context: resource.Context, VCenterID: resource.VCenterID, Datacenter: resourceDC(resource)}
			for _, candidate := range in.Data.Resources {
				if candidate.Kind != "cluster" || candidate.Context != resource.Context || candidate.Name != cluster {
					continue
				}
				var c vsphere.Cluster
				if decodeResource(candidate, &c) {
					obj = resourceObject(in.Data, candidate, "cluster", c.Name, c.ID, c.Datacenter)
				}
				break
			}
			sort.Strings(memberNames)
			sort.Strings(clusterNames)
			emit(Finding{Rule: "dvswitch-host-coverage", Object: obj,
				Message:  fmt.Sprintf("Distributed switch %q covers %d of %d hosts in cluster %q", sw.Name, len(memberNames), len(clusterHosts), cluster),
				Evidence: []Evidence{{Field: "switch", Observed: sw.Name}, {Field: "member_hosts", Observed: strings.Join(memberNames, ", "), Expected: strings.Join(clusterNames, ", ")}, {Field: "cluster_hosts", Observed: fmt.Sprint(len(memberNames)), Expected: fmt.Sprint(len(clusterHosts))}}})
		}
	}
}

func evaluateHostPathRedundancy(in Input, emit func(Finding)) {
	for _, resource := range in.Data.Resources {
		if resource.Kind != "host" {
			continue
		}
		var host vsphere.Host
		if !decodeResource(resource, &host) {
			continue
		}
		for _, path := range host.Multipaths {
			if path.PathCount >= 2 && path.Dead == 0 {
				continue
			}
			obj := resourceObject(in.Data, resource, "host", host.Name, host.ID, host.Datacenter)
			emit(Finding{Rule: "host-path-redundancy", Object: obj,
				Message: fmt.Sprintf("LUN %q has insufficient storage-path redundancy", path.LUN), Evidence: []Evidence{{Field: "lun", Observed: path.LUN}, {Field: "path_count", Observed: fmt.Sprint(path.PathCount), Expected: ">= 2"}, {Field: "active", Observed: fmt.Sprint(path.Active)}, {Field: "dead", Observed: fmt.Sprint(path.Dead), Expected: "0"}}})
		}
	}
}

func evaluatePortGroupPromiscuous(in Input, emit func(Finding)) {
	for _, resource := range in.Data.Resources {
		if resource.Kind != "host" {
			continue
		}
		var host vsphere.Host
		if !decodeResource(resource, &host) {
			continue
		}
		obj := resourceObject(in.Data, resource, "host", host.Name, host.ID, host.Datacenter)
		for _, sw := range host.VSwitches {
			if !securityPolicyEnabled(sw.Promiscuous, sw.ForgedTransmits, sw.MACChanges) {
				continue
			}
			emit(Finding{Rule: "portgroup-promiscuous", Object: obj,
				Message: fmt.Sprintf("Standard vSwitch %q permits insecure frame policies", sw.Name), Evidence: securityEvidence("switch", sw.Name, sw.Promiscuous, sw.ForgedTransmits, sw.MACChanges)})
		}
		for _, group := range host.PortGroups {
			if !securityPolicyEnabled(group.Promiscuous, group.ForgedTransmits, group.MACChanges) {
				continue
			}
			emit(Finding{Rule: "portgroup-promiscuous", Object: obj,
				Message: fmt.Sprintf("Standard port group %q permits insecure frame policies", group.Name), Evidence: securityEvidence("port_group", group.Name, group.Promiscuous, group.ForgedTransmits, group.MACChanges)})
		}
	}
}

func evaluateDVPortGroupPromiscuous(in Input, emit func(Finding)) {
	for _, resource := range in.Data.Resources {
		if resource.Kind != "dvswitch" {
			continue
		}
		var sw vsphere.DVSwitch
		if !decodeResource(resource, &sw) {
			continue
		}
		obj := resourceObject(in.Data, resource, "dvswitch", sw.Name, sw.ID, sw.Datacenter)
		for _, group := range sw.PortGroups {
			if !securityPolicyEnabled(group.Promiscuous, group.ForgedTransmits, group.MACChanges) {
				continue
			}
			emit(Finding{Rule: "dvportgroup-promiscuous", Object: obj,
				Message: fmt.Sprintf("Distributed port group %q permits insecure frame policies", group.Name), Evidence: append([]Evidence{{Field: "switch", Observed: sw.Name}}, securityEvidence("port_group", group.Name, group.Promiscuous, group.ForgedTransmits, group.MACChanges)...)})
		}
	}
}

func securityPolicyEnabled(values ...*bool) bool {
	for _, value := range values {
		if value != nil && *value {
			return true
		}
	}
	return false
}

func securityEvidence(kind, name string, promiscuous, forged, macChanges *bool) []Evidence {
	return []Evidence{{Field: kind, Observed: name}, {Field: "promiscuous", Observed: boolText(promiscuous), Expected: "false"}, {Field: "forged_transmits", Observed: boolText(forged), Expected: "false"}, {Field: "mac_changes", Observed: boolText(macChanges), Expected: "false"}}
}

func boolText(value *bool) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprint(*value)
}
