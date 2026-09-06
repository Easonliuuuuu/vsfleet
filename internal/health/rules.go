package health

import (
	"fmt"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// rules is deliberately a single registry: the CLI, coverage sheet, and
// evaluator all use the same IDs and ordering.
var rules = []Rule{
	{
		ID: "cdrom-connected", Severity: SeverityWarning, MinSchema: 6,
		Summary: "a virtual CD-ROM is currently connected", Needs: "VM CD-ROM connection inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("cdrom-connected", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "datastore-inaccessible", Severity: SeverityCritical,
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
					Message: "Datastore is inaccessible"})
			}
		},
	},
	{
		ID: "datastore-space-low", Severity: SeverityWarning,
		Summary: "datastore free space is below the configured floor", Needs: "datastore capacity and free-space inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("datastore-space-low", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "guest-disk-space-low", Severity: SeverityWarning, MinSchema: 4,
		Summary: "guest filesystem free space is below the configured floor", Needs: "guest partition inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("guest-disk-space-low", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "host-disconnected", Severity: SeverityCritical,
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
					Message: fmt.Sprintf("Host is %s", nonempty(host.ConnectionState, "not connected"))})
			}
		},
	},
	{
		ID: "host-in-maintenance", Severity: SeverityInfo,
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
					Message: "Host is in maintenance mode"})
			}
		},
	},
	{
		ID: "snapshot-age", Severity: SeverityWarning,
		Summary: "snapshot is at least the configured age", Needs: "VM snapshot inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("snapshot-age", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "tools-not-installed", Severity: SeverityWarning, MinSchema: 3,
		Summary: "VMware Tools are not installed", Needs: "VMware Tools version inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("tools-not-installed", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "tools-not-running", Severity: SeverityWarning,
		Summary: "VMware Tools are not running on a powered-on VM", Needs: "VMware Tools running status",
		Eval: func(in Input, emit func(Finding)) {
			for _, item := range in.Data.VMs {
				vm := item.Observation.VM
				if vm.IsTemplate || vm.PowerState != "poweredOn" || vm.ToolsState == "guestToolsRunning" {
					continue
				}
				emit(Finding{Rule: "tools-not-running", Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("VMware Tools are not running (%s)", nonempty(vm.ToolsState, "unknown"))})
			}
		},
	},
	{
		ID: "tools-outdated", Severity: SeverityWarning, MinSchema: 3,
		Summary: "VMware Tools version needs an upgrade", Needs: "VMware Tools version inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("tools-outdated", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "usb-connected", Severity: SeverityWarning, MinSchema: 6,
		Summary: "a virtual USB device is currently connected", Needs: "VM USB connection inventory",
		Eval: func(in Input, emit func(Finding)) {
			evaluateRule("usb-connected", in, Options{Thresholds: in.Thresholds}, emit)
		},
	},
	{
		ID: "vm-inaccessible", Severity: SeverityCritical, MinSchema: 7,
		Summary: "VM connection state is inaccessible", Needs: "VM connection state inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, item := range in.Data.VMs {
				vm := item.Observation.VM
				if vm.IsTemplate || !inaccessibleVMStates[vm.ConnectionState] {
					continue
				}
				emit(Finding{Rule: "vm-inaccessible", Severity: SeverityCritical, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("VM is %s", nonempty(vm.ConnectionState, "inaccessible"))})
			}
		},
	},
	{
		ID: "vm-orphaned", Severity: SeverityCritical, MinSchema: 7,
		Summary: "VM is orphaned from every host", Needs: "VM connection state inventory",
		Eval: func(in Input, emit func(Finding)) {
			for _, item := range in.Data.VMs {
				vm := item.Observation.VM
				if vm.IsTemplate || vm.ConnectionState != "orphaned" {
					continue
				}
				emit(Finding{Rule: "vm-orphaned", Severity: SeverityCritical, Object: vmObject(in.Data, item.Observation),
					Message: fmt.Sprintf("VM is %s", nonempty(vm.ConnectionState, "orphaned"))})
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
				Message: fmt.Sprintf("Datastore has %s%% free space (%s of %s)", percent(free), humanize.Bytes(datastore.FreeBytes), humanize.Bytes(datastore.CapacityBytes))})
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
					Message: fmt.Sprintf("Guest filesystem %q has %s%% free space (%s of %s)", label, percent(free), humanize.Bytes(partition.FreeBytes), humanize.Bytes(partition.CapacityBytes))})
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
					Message: fmt.Sprintf("CD-ROM %q is connected (%s)", nonempty(cdrom.Label, "unnamed CD-ROM"), attachedDeviceDetails(cdrom.BackingType, cdrom.BackingPath, cdrom.BackingDevice, cdrom.BackingHost))})
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
					Message: fmt.Sprintf("USB device %q is connected (%s)", nonempty(usb.Label, "unnamed USB device"), attachedDeviceDetails(usb.BackingType, usb.BackingPath, usb.BackingDevice, usb.BackingHost))})
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
					Message: fmt.Sprintf("Snapshot %q is %s old (created %s)", snapshot.Name, ageText(age), snapshot.CreateTime.UTC().Format("2006-01-02"))})
			}
		}
	case "tools-not-installed":
		for _, item := range in.Data.VMs {
			vm := item.Observation.VM
			if vm.IsTemplate || vm.ToolsVersionStatus != "guestToolsNotInstalled" {
				continue
			}
			emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation), Message: "VMware Tools are not installed (guestToolsNotInstalled)"})
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
			emit(Finding{Rule: ruleID, Severity: SeverityWarning, Object: vmObject(in.Data, item.Observation), Message: fmt.Sprintf("VMware Tools are out of date (%s)", vm.ToolsVersionStatus)})
		}
	}
}
