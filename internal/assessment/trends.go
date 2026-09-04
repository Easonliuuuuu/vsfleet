package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// TrendOptions controls historical windows. A zero limit means unlimited;
// callers generally leave it at 30 to match the CLI default.
type TrendOptions struct {
	FromID         int64
	ToID           int64
	Limit          int
	IncludePartial bool
	Contexts       []string
}

type ChurnPoint struct {
	Run      Run `json:"run"`
	VMCount  int `json:"vm_count"`
	Appeared int `json:"appeared"`
	Vanished int `json:"vanished"`
	Moved    int `json:"moved"`
	Modified int `json:"modified"`
}

type TrendWindow struct {
	FromRunID      int64 `json:"from_run_id,omitempty"`
	ToRunID        int64 `json:"to_run_id,omitempty"`
	Limit          int   `json:"limit"`
	IncludePartial bool  `json:"include_partial"`
}

type ChurnTrend struct {
	SchemaVersion int             `json:"schema_version"`
	Window        TrendWindow     `json:"window"`
	Coverage      []CoverageIssue `json:"coverage"`
	Points        []ChurnPoint    `json:"points"`
}

type SnapshotTrendPoint struct {
	Run       Run           `json:"run"`
	Total     int           `json:"total"`
	Stale     int           `json:"stale"`
	OldestAge time.Duration `json:"oldest_age"`
	MaxAge    time.Duration `json:"max_age"`
}

type SnapshotTrend struct {
	SchemaVersion int                  `json:"schema_version"`
	Window        TrendWindow          `json:"window"`
	OlderThan     time.Duration        `json:"older_than"`
	Coverage      []CoverageIssue      `json:"coverage"`
	Points        []SnapshotTrendPoint `json:"points"`
}

// CapacityValue is nullable in JSON when a source did not report a metric;
// zero is a real value and is never used as an unknown sentinel.
type CapacityValue struct {
	Value *float64 `json:"value"`
	Unit  string   `json:"unit"`
}

type CapacityPoint struct {
	Run                Run      `json:"run"`
	CPUCapacity        *float64 `json:"cpu_capacity"`
	CPUUsed            *float64 `json:"cpu_used"`
	CPUUtilization     *float64 `json:"cpu_utilization_percent"`
	MemoryCapacity     *float64 `json:"memory_capacity"`
	MemoryUsed         *float64 `json:"memory_used"`
	MemoryUtilization  *float64 `json:"memory_utilization_percent"`
	StorageCapacity    *float64 `json:"storage_capacity"`
	StorageUsed        *float64 `json:"storage_used"`
	StorageFree        *float64 `json:"storage_free"`
	StorageUtilization *float64 `json:"storage_utilization_percent"`
}

type CapacitySeries struct {
	Kind   string          `json:"kind"`
	Scope  string          `json:"scope"` // estate, context, or resource
	Name   string          `json:"name"`
	Points []CapacityPoint `json:"points"`
}

type CapacityTrend struct {
	SchemaVersion int               `json:"schema_version"`
	Window        TrendWindow       `json:"window"`
	Kinds         []string          `json:"kinds"`
	Units         map[string]string `json:"units"`
	Coverage      []CoverageIssue   `json:"coverage"`
	Series        []CapacitySeries  `json:"series"`
}

type ReportCoverage struct {
	Context   string `json:"context"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	ItemCount int    `json:"item_count"`
	Error     string `json:"error,omitempty"`
}

type AssessmentReport struct {
	SchemaVersion     int               `json:"schema_version"`
	Run               Run               `json:"run"`
	Coverage          []ReportCoverage  `json:"coverage"`
	VMCount           int               `json:"vm_count"`
	HostCount         int               `json:"host_count"`
	ClusterCount      int               `json:"cluster_count"`
	DatastoreCount    int               `json:"datastore_count"`
	SnapshotTotal     int               `json:"snapshot_total"`
	SnapshotStale     int               `json:"snapshot_stale"`
	HostCapacity      CapacityPoint     `json:"host_capacity"`
	ClusterCapacity   CapacityPoint     `json:"cluster_capacity"`
	DatastoreCapacity CapacityPoint     `json:"datastore_capacity"`
	Units             map[string]string `json:"units"`
	Warnings          []string          `json:"warnings,omitempty"`
}

func (s *Store) trendRuns(ctx context.Context, opts TrendOptions) ([]Run, error) {
	if opts.Limit < 0 {
		return nil, fmt.Errorf("trend limit cannot be negative")
	}
	runs, err := s.Runs(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]Run, 0, len(runs))
	for _, run := range runs {
		if !opts.IncludePartial && run.Status != RunComplete {
			continue
		}
		if opts.IncludePartial && run.Status != RunComplete && run.Status != RunPartial {
			continue
		}
		if opts.FromID != 0 && run.ID < opts.FromID {
			continue
		}
		if opts.ToID != 0 && run.ID > opts.ToID {
			continue
		}
		filtered = append(filtered, run)
	}
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	if opts.FromID != 0 && opts.ToID != 0 && opts.FromID > opts.ToID {
		return nil, fmt.Errorf("trend window --from must not be after --to")
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].StartedAt.Before(filtered[j].StartedAt) })
	return filtered, nil
}

func (s *Store) ChurnTrend(ctx context.Context, opts TrendOptions) (ChurnTrend, error) {
	runs, err := s.trendRuns(ctx, opts)
	if err != nil {
		return ChurnTrend{}, err
	}
	trend := ChurnTrend{SchemaVersion: 1, Window: trendWindow(opts, runs), Coverage: make([]CoverageIssue, 0), Points: make([]ChurnPoint, 0, len(runs))}
	var previous int64
	for _, run := range runs {
		vms, contexts, err := s.loadVMs(ctx, run.ID)
		if err != nil {
			return ChurnTrend{}, err
		}
		count := 0
		for id, c := range contexts {
			if !successful(c.VMStatus) || !trendContextAllowed(c.Name, opts.Contexts) {
				continue
			}
			count += len(vms[id])
		}
		point := ChurnPoint{Run: run, VMCount: count}
		if previous != 0 {
			d, err := s.Diff(ctx, previous, run.ID, false)
			if err != nil {
				return ChurnTrend{}, err
			}
			for _, change := range d.VMs {
				if !trendContextAllowed(change.Context, opts.Contexts) {
					continue
				}
				for _, kind := range change.Changes {
					switch kind {
					case "appeared":
						point.Appeared++
					case "vanished":
						point.Vanished++
					case "moved":
						point.Moved++
					case "modified":
						point.Modified++
					}
				}
			}
			trend.Coverage = append(trend.Coverage, d.Coverage...)
		}
		trend.Points = append(trend.Points, point)
		previous = run.ID
	}
	return trend, nil
}

func (s *Store) SnapshotTrend(ctx context.Context, opts TrendOptions, olderThan time.Duration) (SnapshotTrend, error) {
	if olderThan <= 0 {
		olderThan = 30 * 24 * time.Hour
	}
	runs, err := s.trendRuns(ctx, opts)
	if err != nil {
		return SnapshotTrend{}, err
	}
	trend := SnapshotTrend{SchemaVersion: 1, Window: trendWindow(opts, runs), OlderThan: olderThan, Coverage: make([]CoverageIssue, 0), Points: make([]SnapshotTrendPoint, 0, len(runs))}
	for _, run := range runs {
		contextRuns, err := s.ContextRuns(ctx, run.ID)
		if err != nil {
			return SnapshotTrend{}, err
		}
		for _, contextRun := range contextRuns {
			if !trendContextAllowed(contextRun.Name, opts.Contexts) {
				continue
			}
			if contextRun.VMStatus != "success" && contextRun.VMStatus != "empty" {
				trend.Coverage = append(trend.Coverage, CoverageIssue{Scope: "snapshot", Context: contextRun.Name, Message: "VM collection is not complete: " + nonempty(contextRun.Error, contextRun.VMStatus)})
			}
		}
		ages, err := s.snapshotAges(ctx, run.ID, 0)
		if err != nil {
			return SnapshotTrend{}, err
		}
		point := SnapshotTrendPoint{}
		for _, age := range ages {
			if !trendContextAllowed(age.Context, opts.Contexts) {
				continue
			}
			point.Total++
			if age.Age >= olderThan {
				point.Stale++
			}
			if age.Age > point.MaxAge {
				point.MaxAge = age.Age
			}
		}
		point.OldestAge = point.MaxAge
		trend.Points = append(trend.Points, point)
	}
	return trend, nil
}

func trendContextAllowed(name string, contexts []string) bool {
	if len(contexts) == 0 {
		return true
	}
	for _, allowed := range contexts {
		if strings.EqualFold(strings.TrimSpace(allowed), name) {
			return true
		}
	}
	return false
}

func (s *Store) CapacityTrend(ctx context.Context, opts TrendOptions, kinds []string) (CapacityTrend, error) {
	if len(kinds) == 0 {
		kinds = []string{"host", "cluster", "datastore"}
	}
	runs, err := s.trendRuns(ctx, opts)
	if err != nil {
		return CapacityTrend{}, err
	}
	trend := CapacityTrend{SchemaVersion: 1, Window: trendWindow(opts, runs), Kinds: append([]string(nil), kinds...), Units: map[string]string{"cpu_capacity": "MHz (host/cluster)", "cpu_used": "MHz", "cpu_utilization_percent": "%", "memory_capacity": "MB", "memory_used": "MB", "memory_utilization_percent": "%", "storage_capacity": "bytes", "storage_used": "bytes", "storage_free": "bytes", "storage_utilization_percent": "%"}, Coverage: make([]CoverageIssue, 0)}
	for _, kind := range kinds {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind == "all" {
			continue
		}
		series := make(map[string]*CapacitySeries)
		perRun := make([][]storedResource, len(runs))
		contextNames := make(map[string]bool)
		resourceNames := make(map[string]bool)
		for _, run := range runs {
			data, err := s.loadResources(ctx, run.ID)
			if err != nil {
				return CapacityTrend{}, err
			}
			for _, key := range sortedCoverageKeys(data.Coverage) {
				collection := data.Coverage[key]
				if collection.Kind == kind && collection.Status != "success" && collection.Status != "empty" {
					trend.Coverage = append(trend.Coverage, CoverageIssue{Scope: "trend", Context: strings.TrimSuffix(key, "\x00"+collection.Kind), Message: fmt.Sprintf("%s collection %s: %s", collection.Kind, collection.Status, nonempty(collection.Error, "incomplete"))})
				}
			}
			if !hasCoverageKind(data.Coverage, kind) {
				trend.Coverage = append(trend.Coverage, CoverageIssue{Scope: "trend", Context: "run " + fmt.Sprint(run.ID), Message: kind + " collection was not recorded"})
			}
			runIndex := 0
			for i := range runs {
				if runs[i].ID == run.ID {
					runIndex = i
					break
				}
			}
			runResources := make([]storedResource, 0, len(data.ByKind[kind]))
			for _, resource := range data.ByKind[kind] {
				if trendContextAllowed(resource.observation.Context, opts.Contexts) {
					runResources = append(runResources, resource)
					contextNames[resource.observation.Context] = true
					resourceNames[resource.observation.Context+"/"+resource.observation.Name] = true
				}
			}
			perRun[runIndex] = runResources
		}
		for i, run := range runs {
			appendCapacityPoint(series, kind, "estate", "", run, capacityForResources(kind, perRun[i], "estate", ""))
		}
		contextsSorted := make([]string, 0, len(contextNames))
		for name := range contextNames {
			contextsSorted = append(contextsSorted, name)
		}
		sort.Strings(contextsSorted)
		for _, contextName := range contextsSorted {
			for i, run := range runs {
				appendCapacityPoint(series, kind, "context", contextName, run, capacityForResources(kind, filterResourcesContext(perRun[i], contextName), "context", contextName))
			}
		}
		resourcesSorted := make([]string, 0, len(resourceNames))
		for name := range resourceNames {
			resourcesSorted = append(resourcesSorted, name)
		}
		sort.Strings(resourcesSorted)
		for _, resourceName := range resourcesSorted {
			parts := strings.SplitN(resourceName, "/", 2)
			contextName, name := parts[0], parts[len(parts)-1]
			for i, run := range runs {
				appendCapacityPoint(series, kind, "resource", resourceName, run, capacityForResources(kind, filterResourcesName(perRun[i], contextName, name), "resource", name))
			}
		}
		keys := make([]string, 0, len(series))
		for key := range series {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			scope := func(key string) string { return strings.SplitN(key, "\x00", 2)[0] }
			rank := func(scope string) int {
				switch scope {
				case "estate":
					return 0
				case "context":
					return 1
				default:
					return 2
				}
			}
			li, lj := rank(scope(keys[i])), rank(scope(keys[j]))
			if li != lj {
				return li < lj
			}
			return keys[i] < keys[j]
		})
		for _, key := range keys {
			trend.Series = append(trend.Series, *series[key])
		}
	}
	return trend, nil
}

func filterResourcesContext(resources []storedResource, contextName string) []storedResource {
	var out []storedResource
	for _, resource := range resources {
		if resource.observation.Context == contextName {
			out = append(out, resource)
		}
	}
	return out
}

func filterResourcesName(resources []storedResource, contextName, name string) []storedResource {
	var out []storedResource
	for _, resource := range resources {
		if resource.observation.Context == contextName && resource.observation.Name == name {
			out = append(out, resource)
		}
	}
	return out
}

func trendWindow(opts TrendOptions, runs []Run) TrendWindow {
	w := TrendWindow{Limit: opts.Limit, IncludePartial: opts.IncludePartial}
	if len(runs) > 0 {
		w.FromRunID, w.ToRunID = runs[0].ID, runs[len(runs)-1].ID
	}
	return w
}

func (s *Store) Report(ctx context.Context, runID int64, olderThan time.Duration) (AssessmentReport, error) {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return AssessmentReport{}, err
	}
	if olderThan <= 0 {
		olderThan = 30 * 24 * time.Hour
	}
	report := AssessmentReport{SchemaVersion: 1, Run: run, Units: map[string]string{"cpu_capacity": "MHz", "cpu_used": "MHz", "cpu_utilization_percent": "%", "memory_capacity": "MB", "memory_used": "MB", "memory_utilization_percent": "%", "storage_capacity": "bytes", "storage_used": "bytes", "storage_free": "bytes", "storage_utilization_percent": "%"}}
	vms, contexts, err := s.loadVMs(ctx, runID)
	if err != nil {
		return report, err
	}
	for id, c := range contexts {
		if successful(c.VMStatus) {
			report.VMCount += len(vms[id])
		}
	}
	resources, err := s.loadResources(ctx, runID)
	if err != nil {
		return report, err
	}
	for _, contextRun := range contexts {
		for _, collection := range contextRun.Collections {
			report.Coverage = append(report.Coverage, ReportCoverage{Context: contextRun.Name, Kind: collection.Kind, Status: collection.Status, ItemCount: collection.ItemCount, Error: collection.Error})
			if collection.Status != "success" && collection.Status != "empty" {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s %s collection: %s", contextRun.Name, collection.Kind, nonempty(collection.Error, collection.Status)))
			}
		}
	}
	sort.Slice(report.Coverage, func(i, j int) bool {
		if report.Coverage[i].Context != report.Coverage[j].Context {
			return report.Coverage[i].Context < report.Coverage[j].Context
		}
		return report.Coverage[i].Kind < report.Coverage[j].Kind
	})
	for kind, values := range resources.ByKind {
		switch kind {
		case "host":
			report.HostCount = len(values)
			report.HostCapacity = capacityForResources(kind, values, "estate", "")
		case "cluster":
			report.ClusterCount = len(values)
			report.ClusterCapacity = capacityForResources(kind, values, "estate", "")
		case "datastore":
			report.DatastoreCount = len(values)
			report.DatastoreCapacity = capacityForResources(kind, values, "estate", "")
		}
	}
	ages, err := s.snapshotAges(ctx, runID, 0)
	if err != nil {
		return report, err
	}
	report.SnapshotTotal = len(ages)
	for _, age := range ages {
		if age.Age >= olderThan {
			report.SnapshotStale++
		}
	}
	infraCoverage := false
	for _, coverage := range report.Coverage {
		if coverage.Kind == "host" || coverage.Kind == "cluster" || coverage.Kind == "datastore" {
			infraCoverage = true
			break
		}
	}
	if !infraCoverage {
		report.Warnings = append(report.Warnings, "infrastructure collection coverage is unavailable for this run")
	}
	return report, nil
}

func appendCapacityPoint(series map[string]*CapacitySeries, kind, scope, name string, run Run, point CapacityPoint) {
	key := scope + "\x00" + name
	if series[key] == nil {
		series[key] = &CapacitySeries{Kind: kind, Scope: scope, Name: name}
	}
	series[key].Points = append(series[key].Points, point)
}

func capacityForResources(kind string, resources []storedResource, _ string, _ string) CapacityPoint {
	point := CapacityPoint{}
	var cpuCap, cpuUsed, memCap, memUsed, storageCap, storageFree float64
	var cpuCapOK, cpuUsedOK, memCapOK, memUsedOK, storageCapOK, storageFreeOK bool
	for _, resource := range resources {
		if resource.observation.CPUCapacity != nil {
			cpuCap += *resource.observation.CPUCapacity
			cpuCapOK = true
		}
		if resource.observation.CPUUsed != nil {
			cpuUsed += *resource.observation.CPUUsed
			cpuUsedOK = true
		}
		if resource.observation.MemoryCapacity != nil {
			memCap += *resource.observation.MemoryCapacity
			memCapOK = true
		}
		if resource.observation.MemoryUsed != nil {
			memUsed += *resource.observation.MemoryUsed
			memUsedOK = true
		}
		if resource.observation.StorageCapacity != nil {
			storageCap += *resource.observation.StorageCapacity
			storageCapOK = true
		}
		if resource.observation.StorageFree != nil {
			storageFree += *resource.observation.StorageFree
			storageFreeOK = true
		}
		var m map[string]any
		if json.Unmarshal(resource.observation.Payload, &m) != nil {
			continue
		}
		if kind == "host" || kind == "cluster" {
			cpuKey := "cpu_cores"
			if kind == "host" {
				cpuKey = "cpu_mhz"
			} else {
				cpuKey = "total_cpu_mhz"
			}
			if !cpuCapOK {
				if n, ok := numberField(m, cpuKey); ok {
					cpuCap += n
					cpuCapOK = true
				}
			}
			if !cpuUsedOK {
				if n, ok := numberField(m, "cpu_usage_mhz"); ok {
					cpuUsed += n
					cpuUsedOK = true
				}
			}
			memKey := "memory_mb"
			if kind == "cluster" {
				memKey = "total_memory_mb"
			}
			if !memCapOK {
				if n, ok := numberField(m, memKey); ok {
					memCap += n
					memCapOK = true
				}
			}
			if !memUsedOK {
				if n, ok := numberField(m, "memory_usage_mb"); ok {
					memUsed += n
					memUsedOK = true
				}
			}
		}
		if kind == "datastore" {
			if !storageCapOK {
				if n, ok := numberField(m, "capacity_bytes"); ok {
					storageCap += n
					storageCapOK = true
				}
			}
			if !storageFreeOK {
				if n, ok := numberField(m, "free_bytes"); ok {
					storageFree += n
					storageFreeOK = true
				}
			}
		}
	}
	if cpuCapOK {
		point.CPUCapacity = floatPtr(cpuCap)
	}
	if cpuUsedOK {
		point.CPUUsed = floatPtr(cpuUsed)
	}
	if memCapOK {
		point.MemoryCapacity = floatPtr(memCap)
	}
	if memUsedOK {
		point.MemoryUsed = floatPtr(memUsed)
	}
	if storageCapOK {
		point.StorageCapacity = floatPtr(storageCap)
	}
	if storageFreeOK {
		point.StorageFree = floatPtr(storageFree)
	}
	if storageCapOK && storageFreeOK {
		used := storageCap - storageFree
		point.StorageUsed = floatPtr(used)
	}
	if cpuCapOK && cpuUsedOK && cpuCap > 0 {
		point.CPUUtilization = floatPtr(cpuUsed / cpuCap * 100)
	}
	if memCapOK && memUsedOK && memCap > 0 {
		point.MemoryUtilization = floatPtr(memUsed / memCap * 100)
	}
	if storageCapOK && storageFreeOK && storageCap > 0 {
		point.StorageUtilization = floatPtr((storageCap - storageFree) / storageCap * 100)
	}
	return point
}

func resourceMetrics(kind string, payload json.RawMessage) [6]any {
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return [6]any{}
	}
	var metrics [6]any
	if kind == "host" {
		metrics[0], metrics[1] = m["cpu_mhz"], m["cpu_usage_mhz"]
		metrics[2], metrics[3] = m["memory_mb"], m["memory_usage_mb"]
	} else if kind == "cluster" {
		metrics[0], metrics[2] = m["total_cpu_mhz"], m["total_memory_mb"]
	} else if kind == "datastore" {
		metrics[4], metrics[5] = m["capacity_bytes"], m["free_bytes"]
	}
	return metrics
}

func numberField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	n, ok2 := v.(float64)
	return n, ok && ok2
}

func floatPtr(v float64) *float64 { return &v }
