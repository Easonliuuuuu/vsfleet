package rvimport

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const (
	// zoneStep is the smallest step between UTC offsets in use. Time zones such
	// as Asia/Kathmandu are not a whole number of hours from UTC.
	zoneStep = 15 * time.Minute
	// maxZoneOffset is the largest distance from UTC of any time zone.
	maxZoneOffset = 14 * time.Hour
	// snapshotTimeSlop allows for a workbook recording only whole seconds.
	snapshotTimeSlop = 2 * time.Second
)

// CheckSnapshotTimes warns when a workbook read without a time zone records
// every snapshot it shares with the newest live capture a time zone's distance
// from what vCenter reported. RVTools times carry no zone and are read as UTC,
// so such a workbook most likely came from a collector in another zone. A
// shared snapshot at the same time, or at any other distance, means no warning.
func (r *Result) CheckSnapshotTimes(ctx context.Context, store *assessment.Store) error {
	if r.opts.Timezone != nil {
		return nil
	}
	imported := make(map[string][]time.Time)
	for _, c := range r.contexts {
		for _, vm := range c.vms {
			addSnapshotTimes(imported, *vm, vm.Snapshots)
		}
	}
	if len(imported) == 0 {
		return nil
	}
	runs, err := store.Runs(ctx)
	if err != nil {
		return fmt.Errorf("list captures: %w", err)
	}
	for _, run := range runs { // newest first
		if run.Imported() || run.SuccessfulContexts == 0 {
			continue
		}
		data, err := store.LoadExportData(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("read capture %d: %w", run.ID, err)
		}
		live := make(map[string][]time.Time)
		for _, item := range data.VMs {
			snapshots := item.Snapshots
			if len(snapshots) == 0 {
				snapshots = item.Observation.VM.Snapshots
			}
			addSnapshotTimes(live, item.Observation.VM, snapshots)
		}
		if offsets := zoneOffsets(imported, live); len(offsets) > 0 {
			r.Report.Warnings = append(r.Report.Warnings, fmt.Sprintf("vSnapshot times differ by %s from capture %d for every snapshot both record; they were read as UTC and look local to the collector, so import again with --timezone and its IANA zone", strings.Join(offsets, " or "), run.ID))
		}
		return nil
	}
	return nil
}

// addSnapshotTimes records each snapshot under every identity its VM has. Names
// are not unique on a VM, so a repeated name holds several times.
func addSnapshotTimes(times map[string][]time.Time, vm vsphere.VM, snapshots []vsphere.VMSnapshot) {
	for _, identity := range []string{vm.InstanceUUID, vm.BIOSUUID} {
		if identity = strings.ToLower(strings.TrimSpace(identity)); identity == "" {
			continue
		}
		for _, s := range snapshots {
			key := identity + "\x00" + s.Name
			times[key] = append(times[key], s.CreateTime)
		}
	}
}

// zoneOffsets returns the distinct time-zone distances between the snapshots
// both sides record exactly once. It returns none unless every one of them is
// such a distance: a single match rules the workbook correct. More than one is
// expected across a daylight-saving change.
func zoneOffsets(imported, live map[string][]time.Time) []string {
	var distances []time.Duration
	for key, times := range imported {
		other := live[key]
		if len(times) != 1 || len(other) != 1 {
			continue
		}
		distance := times[0].Sub(other[0]).Abs()
		step := distance.Round(zoneStep)
		if step == 0 || step > maxZoneOffset || (distance-step).Abs() > snapshotTimeSlop {
			return nil
		}
		if !slices.Contains(distances, step) {
			distances = append(distances, step)
		}
	}
	slices.Sort(distances)
	out := make([]string, len(distances))
	for i, d := range distances {
		out[i] = strings.TrimSuffix(strings.TrimSuffix(d.String(), "0s"), "0m")
	}
	return out
}
