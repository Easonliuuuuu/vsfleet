package assessment

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Diff compares two immutable runs. Only vCenters successfully collected in
// both runs participate in lifecycle claims; this is the guard that prevents
// an outage from looking like mass deletion.
func (s *Store) Diff(ctx context.Context, baseID, targetID int64, includeRuntime bool) (Diff, error) {
	base, err := s.GetRun(ctx, baseID)
	if err != nil {
		return Diff{}, err
	}
	target, err := s.GetRun(ctx, targetID)
	if err != nil {
		return Diff{}, err
	}
	bv, bc, err := s.loadVMs(ctx, baseID)
	if err != nil {
		return Diff{}, err
	}
	tv, tc, err := s.loadVMs(ctx, targetID)
	if err != nil {
		return Diff{}, err
	}
	d := Diff{SchemaVersion: 1, Base: base, Target: target}
	baseByVC := make(map[string][]storedVM)
	targetByVC := make(map[string][]storedVM)
	for id, c := range bc {
		if c.VCenterID != "" && successful(c.VMStatus) {
			baseByVC[c.VCenterID] = append(baseByVC[c.VCenterID], bv[id]...)
		} else if c.VMStatus != "" && !successful(c.VMStatus) {
			msg := fmt.Sprintf("%s was not fully collected in baseline: %s", c.Name, nonempty(c.Error, c.VMStatus))
			d.Warnings = append(d.Warnings, msg)
			d.Coverage = append(d.Coverage, CoverageIssue{Scope: "baseline", Context: c.Name, Message: msg})
		}
	}
	for id, c := range tc {
		if c.VCenterID != "" && successful(c.VMStatus) {
			targetByVC[c.VCenterID] = append(targetByVC[c.VCenterID], tv[id]...)
		} else if c.VMStatus != "" && !successful(c.VMStatus) {
			msg := fmt.Sprintf("%s was not fully collected in target: %s", c.Name, nonempty(c.Error, c.VMStatus))
			d.Warnings = append(d.Warnings, msg)
			d.Coverage = append(d.Coverage, CoverageIssue{Scope: "target", Context: c.Name, Message: msg})
		}
	}
	var allBase, allTarget []storedVM
	for vc, values := range baseByVC {
		if _, ok := targetByVC[vc]; ok {
			allBase = append(allBase, values...)
			allTarget = append(allTarget, targetByVC[vc]...)
		}
	}
	changes, snapshots := compareVMs(allBase, allTarget, includeRuntime)
	d.VMs = append(d.VMs, changes...)
	d.Snapshots = append(d.Snapshots, snapshots...)
	for vc := range baseByVC {
		if _, ok := targetByVC[vc]; !ok {
			msg := "vCenter " + vc + " is not comparable: it was not successfully collected in target"
			d.Warnings = append(d.Warnings, msg)
			d.Coverage = append(d.Coverage, CoverageIssue{Scope: "target", Context: vc, Message: msg})
		}
	}
	for vc := range targetByVC {
		if _, ok := baseByVC[vc]; !ok {
			msg := "vCenter " + vc + " is not comparable: it was not successfully collected in baseline"
			d.Warnings = append(d.Warnings, msg)
			d.Coverage = append(d.Coverage, CoverageIssue{Scope: "baseline", Context: vc, Message: msg})
		}
	}
	d.Counts = countChanges(d.VMs, d.Snapshots)
	if err := s.infrastructureDiff(ctx, baseID, targetID, includeRuntime, &d); err != nil {
		return Diff{}, err
	}
	sort.Slice(d.VMs, func(i, j int) bool { return strings.ToLower(d.VMs[i].Name) < strings.ToLower(d.VMs[j].Name) })
	sort.Slice(d.Snapshots, func(i, j int) bool {
		return strings.ToLower(d.Snapshots[i].VMName) < strings.ToLower(d.Snapshots[j].VMName)
	})
	d.SnapshotAges, err = s.snapshotAges(ctx, targetID, 0)
	if err != nil {
		return Diff{}, err
	}
	return d, nil
}

func successful(s string) bool { return s == "success" || s == "empty" }
func nonempty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func countChanges(vms []VMChange, snaps []SnapshotChange) DiffCounts {
	var c DiffCounts
	for _, v := range vms {
		for _, k := range v.Changes {
			switch k {
			case "appeared":
				c.Appeared++
			case "vanished":
				c.Vanished++
			case "moved":
				c.Moved++
			case "modified":
				c.Modified++
			}
		}
	}
	c.Snapshots = len(snaps)
	return c
}

func compareVMs(base, target []storedVM, includeRuntime bool) ([]VMChange, []SnapshotChange) {
	used := make([]bool, len(base))
	targetIdentityCounts := make(map[string]int, len(target)*2)
	for _, vm := range target {
		if value := vm.observation.VM.InstanceUUID; value != "" {
			targetIdentityCounts[identityKey("instance_uuid", value)]++
		}
		if value := vm.observation.VM.BIOSUUID; value != "" {
			targetIdentityCounts[identityKey("bios_uuid", value)]++
		}
	}
	var out []VMChange
	var snaps []SnapshotChange
	for i := range target {
		j, basis := findMatch(base, used, target[i].observation, targetIdentityCounts)
		if j < 0 {
			out = append(out, VMChange{Kind: "vm", Changes: []string{"appeared"}, Name: target[i].observation.VM.Name, Context: target[i].observation.Context, After: &target[i].observation})
			for _, snap := range target[i].snapshots {
				snaps = append(snaps, SnapshotChange{Kind: "created", VMName: target[i].observation.VM.Name, Context: target[i].observation.Context, Name: snap.Name, After: snap.CreateTime.UTC().Format(time.RFC3339), CreateTime: snap.CreateTime.UTC().Format(time.RFC3339)})
			}
			continue
		}
		used[j] = true
		before, after := base[j], target[i]
		fields := changedFields(before.observation.VM, after.observation.VM, includeRuntime)
		changes := make([]string, 0, 2)
		if moved(before.observation.VM, after.observation.VM, before.observation.VCenterID, after.observation.VCenterID) {
			changes = append(changes, "moved")
		}
		if len(fields) > 0 {
			changes = append(changes, "modified")
		}
		if len(changes) > 0 {
			out = append(out, VMChange{Kind: "vm", Changes: changes, Name: after.observation.VM.Name, Context: after.observation.Context, MatchBasis: basis, Fields: fields, Before: &before.observation, After: &after.observation})
		}
		if before.observation.VCenterID == after.observation.VCenterID {
			snaps = append(snaps, compareSnapshots(before, after)...)
		}
	}
	for i := range base {
		if used[i] {
			continue
		}
		b := base[i]
		out = append(out, VMChange{Kind: "vm", Changes: []string{"vanished"}, Name: b.observation.VM.Name, Context: b.observation.Context, Before: &b.observation})
	}
	return out, snaps
}

func findMatch(base []storedVM, used []bool, target Observation, targetIdentityCounts map[string]int) (int, string) {
	for i := range base {
		if used[i] {
			continue
		}
		if base[i].observation.VCenterID == target.VCenterID && base[i].observation.VM.ID == target.VM.ID {
			return i, "moref"
		}
	}
	for _, kind := range []string{"instance_uuid", "bios_uuid"} {
		value := target.VM.InstanceUUID
		if kind == "bios_uuid" {
			value = target.VM.BIOSUUID
		}
		if value == "" {
			continue
		}
		// A duplicated identity is not strong enough evidence to infer a move.
		// Treat every ambiguous target identity as appeared/vanished instead of
		// silently pairing the wrong VM.
		if targetIdentityCounts[identityKey(kind, value)] != 1 {
			continue
		}
		candidate, count := -1, 0
		for i := range base {
			if used[i] {
				continue
			}
			v := base[i].observation.VM
			other := v.InstanceUUID
			if kind == "bios_uuid" {
				other = v.BIOSUUID
			}
			if other == value {
				candidate = i
				count++
			}
		}
		if count == 1 {
			return candidate, kind
		}
	}
	return -1, ""
}

func identityKey(kind, value string) string { return kind + "\x00" + value }

func moved(a, b vsphere.VM, avc, bvc string) bool {
	if avc != bvc || a.Host != b.Host || a.Cluster != b.Cluster || a.Folder != b.Folder {
		return true
	}
	return !equalStrings(a.Datastores, b.Datastores)
}

func equalStrings(a, b []string) bool {
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func changedFields(a, b vsphere.VM, runtime bool) []FieldChange {
	var out []FieldChange
	add := func(name, before, after string) {
		if before != after {
			out = append(out, FieldChange{Field: name, Before: before, After: after})
		}
	}
	add("name", a.Name, b.Name)
	add("cpu", strconv.FormatInt(int64(a.CPU), 10), strconv.FormatInt(int64(b.CPU), 10))
	add("memory", strconv.FormatInt(a.MemoryMB, 10), strconv.FormatInt(b.MemoryMB, 10))
	add("guest_os", a.GuestOS, b.GuestOS)
	add("annotation", a.Annotation, b.Annotation)
	if runtime {
		add("power_state", a.PowerState, b.PowerState)
		add("guest_state", a.GuestState, b.GuestState)
		add("tools_state", a.ToolsState, b.ToolsState)
		add("ip_address", a.IPAddress, b.IPAddress)
		add("storage_gb", fmt.Sprintf("%.3f", a.StorageGB), fmt.Sprintf("%.3f", b.StorageGB))
	}
	return out
}

func compareSnapshots(a, b storedVM) []SnapshotChange {
	byID := make(map[string]vsphere.VMSnapshot, len(a.snapshots))
	for _, s := range a.snapshots {
		byID[s.ID] = s
	}
	var out []SnapshotChange
	for _, s := range b.snapshots {
		if old, ok := byID[s.ID]; ok {
			delete(byID, s.ID)
			if old.Name != s.Name || old.Description != s.Description || old.Current != s.Current {
				out = append(out, SnapshotChange{Kind: "changed", VMName: b.observation.VM.Name, Context: b.observation.Context, Name: s.Name, Before: old.Name, After: s.Name, CreateTime: s.CreateTime.UTC().Format(time.RFC3339)})
			}
		} else {
			out = append(out, SnapshotChange{Kind: "created", VMName: b.observation.VM.Name, Context: b.observation.Context, Name: s.Name, After: s.CreateTime.UTC().Format(time.RFC3339), CreateTime: s.CreateTime.UTC().Format(time.RFC3339)})
		}
	}
	for _, s := range byID {
		out = append(out, SnapshotChange{Kind: "removed", VMName: a.observation.VM.Name, Context: a.observation.Context, Name: s.Name, Before: s.CreateTime.UTC().Format(time.RFC3339), CreateTime: s.CreateTime.UTC().Format(time.RFC3339)})
	}
	return out
}

func (s *Store) snapshotAges(ctx context.Context, runID int64, olderThan time.Duration) ([]SnapshotAge, error) {
	targetVMs, targetContexts, err := s.loadVMs(ctx, runID)
	if err != nil {
		return nil, err
	}
	type key struct{ vc, vm, snap string }
	wanted := make(map[key]SnapshotAge)
	for id, c := range targetContexts {
		if !successful(c.VMStatus) {
			continue
		}
		for _, v := range targetVMs[id] {
			for _, snap := range v.snapshots {
				if olderThan > 0 && c.FinishedAt.Sub(snap.CreateTime) < olderThan {
					continue
				}
				wanted[key{c.VCenterID, v.observation.VM.ID, snap.ID}] = SnapshotAge{VMName: v.observation.VM.Name, Context: v.observation.Context, Name: snap.Name, CreateTime: snap.CreateTime, Age: c.FinishedAt.Sub(snap.CreateTime), FirstSeen: c.FinishedAt, LastSeen: c.FinishedAt}
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT cr.vcenter_id,v.moref,so.moref,cr.finished_at FROM snapshot_observations so JOIN vm_observations v ON v.id=so.vm_observation_id JOIN context_runs cr ON cr.id=v.context_run_id WHERE cr.vm_status IN ('success','empty')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var vc, vm, snap string
		var seen sql.NullInt64
		if err := rows.Scan(&vc, &vm, &snap, &seen); err != nil {
			return nil, err
		}
		k := key{vc, vm, snap}
		a, ok := wanted[k]
		if !ok {
			continue
		}
		t := fromMillis(seen)
		if t.Before(a.FirstSeen) {
			a.FirstSeen = t
		}
		if t.After(a.LastSeen) {
			a.LastSeen = t
		}
		wanted[k] = a
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SnapshotAge, 0, len(wanted))
	for _, a := range wanted {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Age > out[j].Age })
	return out, nil
}
