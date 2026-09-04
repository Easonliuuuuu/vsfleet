package assessment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type storedResource struct {
	observation ResourceObservation
}

type resourceRunData struct {
	ByKind   map[string][]storedResource
	Coverage map[string]CollectionRun // context name + NUL + kind
}

func (s *Store) loadResources(ctx context.Context, runID int64) (resourceRunData, error) {
	out := resourceRunData{ByKind: make(map[string][]storedResource), Coverage: make(map[string]CollectionRun)}
	rows, err := s.db.QueryContext(ctx, `SELECT cr.name,cr.vcenter_id,cc.kind,cc.started_at,cc.finished_at,cc.status,cc.error,cc.item_count FROM context_collections cc JOIN context_runs cr ON cr.id=cc.context_run_id WHERE cr.run_id=?`, runID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var contextName, vc, kind, status, message string
		var start, finish sql.NullInt64
		var count int
		if err := rows.Scan(&contextName, &vc, &kind, &start, &finish, &status, &message, &count); err != nil {
			rows.Close()
			return out, err
		}
		out.Coverage[contextName+"\x00"+kind] = CollectionRun{Kind: kind, StartedAt: fromMillis(start), FinishedAt: fromMillis(finish), Status: status, Error: message, ItemCount: count}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT cr.name,cr.vcenter_id,cc.kind,ro.moref,ro.name,ro.payload,ro.cpu_capacity,ro.cpu_used,ro.memory_capacity,ro.memory_used,ro.storage_capacity,ro.storage_free FROM resource_observations ro JOIN context_collections cc ON cc.id=ro.collection_id JOIN context_runs cr ON cr.id=cc.context_run_id WHERE cr.run_id=?`, runID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var contextName, vc, kind, id, name string
		var payload []byte
		var cpuCap, cpuUsed, memCap, memUsed, storageCap, storageFree sql.NullFloat64
		if err := rows.Scan(&contextName, &vc, &kind, &id, &name, &payload, &cpuCap, &cpuUsed, &memCap, &memUsed, &storageCap, &storageFree); err != nil {
			return out, err
		}
		observation := ResourceObservation{VCenterID: vc, Context: contextName, Kind: kind, ID: id, Name: name, Payload: append(json.RawMessage(nil), payload...)}
		observation.CPUCapacity, observation.CPUUsed = nullFloatPtr(cpuCap), nullFloatPtr(cpuUsed)
		observation.MemoryCapacity, observation.MemoryUsed = nullFloatPtr(memCap), nullFloatPtr(memUsed)
		observation.StorageCapacity, observation.StorageFree = nullFloatPtr(storageCap), nullFloatPtr(storageFree)
		out.ByKind[kind] = append(out.ByKind[kind], storedResource{observation: observation})
	}
	return out, rows.Err()
}

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func (s *Store) infrastructureDiff(ctx context.Context, baseID, targetID int64, includeRuntime bool, d *Diff) error {
	base, err := s.loadResources(ctx, baseID)
	if err != nil {
		return err
	}
	target, err := s.loadResources(ctx, targetID)
	if err != nil {
		return err
	}
	for _, kind := range []string{"host", "cluster", "datastore"} {
		baseByVC, targetByVC := make(map[string][]storedResource), make(map[string][]storedResource)
		// vcNames resolves a VCenterID back to its context name for the "not
		// comparable" messages below, the same way Diff does for VMs.
		vcNames := make(map[string]string)
		for _, key := range sortedCoverageKeys(base.Coverage) {
			c := base.Coverage[key]
			if !strings.HasSuffix(key, "\x00"+kind) {
				continue
			}
			contextName := strings.TrimSuffix(key, "\x00"+kind)
			if c.Status == "success" || c.Status == "empty" {
				vc := contextVCenter(ctx, s, baseID, contextName)
				if vc != "" {
					baseByVC[vc] = append(baseByVC[vc], resourcesForVC(base.ByKind[kind], vc)...)
					vcNames[vc] = contextName
				}
			} else {
				msg := fmt.Sprintf("%s %s collection was not complete in baseline: %s", contextName, kind, nonempty(c.Error, c.Status))
				d.Warnings = append(d.Warnings, msg)
				d.Coverage = append(d.Coverage, CoverageIssue{Scope: "baseline", Context: contextName, Message: msg})
			}
		}
		for _, key := range sortedCoverageKeys(target.Coverage) {
			c := target.Coverage[key]
			if !strings.HasSuffix(key, "\x00"+kind) {
				continue
			}
			contextName := strings.TrimSuffix(key, "\x00"+kind)
			if c.Status == "success" || c.Status == "empty" {
				vc := contextVCenter(ctx, s, targetID, contextName)
				if vc != "" {
					targetByVC[vc] = append(targetByVC[vc], resourcesForVC(target.ByKind[kind], vc)...)
					vcNames[vc] = contextName
				}
			} else {
				msg := fmt.Sprintf("%s %s collection was not complete in target: %s", contextName, kind, nonempty(c.Error, c.Status))
				d.Warnings = append(d.Warnings, msg)
				d.Coverage = append(d.Coverage, CoverageIssue{Scope: "target", Context: contextName, Message: msg})
			}
		}
		// v1/v2 runs have no collection rows. Explicitly report unknown
		// coverage instead of treating an absent table as an empty estate.
		if !hasCoverageKind(base.Coverage, kind) {
			msg := fmt.Sprintf("%s collection was not recorded in baseline", kind)
			d.Warnings = append(d.Warnings, msg)
			d.Coverage = append(d.Coverage, CoverageIssue{Scope: "baseline", Message: msg})
		}
		if !hasCoverageKind(target.Coverage, kind) {
			msg := fmt.Sprintf("%s collection was not recorded in target", kind)
			d.Warnings = append(d.Warnings, msg)
			d.Coverage = append(d.Coverage, CoverageIssue{Scope: "target", Message: msg})
		}
		for vc := range baseByVC {
			if _, ok := targetByVC[vc]; !ok {
				name := vcLabel(vcNames, vc)
				msg := fmt.Sprintf("vCenter %s %s collection is not comparable: it was not successfully collected in target", name, kind)
				d.Warnings = append(d.Warnings, msg)
				d.Coverage = append(d.Coverage, CoverageIssue{Scope: "target", Context: name, Message: msg})
			}
		}
		for vc := range targetByVC {
			if _, ok := baseByVC[vc]; !ok {
				name := vcLabel(vcNames, vc)
				msg := fmt.Sprintf("vCenter %s %s collection is not comparable: it was not successfully collected in baseline", name, kind)
				d.Warnings = append(d.Warnings, msg)
				d.Coverage = append(d.Coverage, CoverageIssue{Scope: "baseline", Context: name, Message: msg})
			}
		}
		changes := compareResources(kind, flattenComparable(baseByVC, targetByVC), flattenComparable(targetByVC, baseByVC), includeRuntime)
		d.Resources = append(d.Resources, changes...)
	}
	sort.SliceStable(d.Resources, func(i, j int) bool {
		if d.Resources[i].Kind != d.Resources[j].Kind {
			return d.Resources[i].Kind < d.Resources[j].Kind
		}
		return strings.ToLower(d.Resources[i].Name) < strings.ToLower(d.Resources[j].Name)
	})
	d.Counts.Resources = len(d.Resources)
	return nil
}

func hasCoverageKind(coverage map[string]CollectionRun, kind string) bool {
	for _, collection := range coverage {
		if collection.Kind == kind {
			return true
		}
	}
	return false
}

func sortedCoverageKeys(coverage map[string]CollectionRun) []string {
	keys := make([]string, 0, len(coverage))
	for key := range coverage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resourcesForVC(resources []storedResource, vc string) []storedResource {
	var out []storedResource
	for _, r := range resources {
		if r.observation.VCenterID == vc {
			out = append(out, r)
		}
	}
	return out
}

// flattenComparable keeps only entries whose vCenter exists on both sides.
// The caller passes the opposite side as the second map to make this guard
// explicit at the comparison boundary.
func flattenComparable(left, right map[string][]storedResource) []storedResource {
	var out []storedResource
	for vc, resources := range left {
		if _, ok := right[vc]; ok {
			out = append(out, resources...)
		}
	}
	return out
}

func compareResources(kind string, base, target []storedResource, includeRuntime bool) []ResourceChange {
	baseByID := make(map[string]storedResource, len(base))
	for _, r := range base {
		baseByID[r.observation.VCenterID+"\x00"+r.observation.ID] = r
	}
	targetByID := make(map[string]storedResource, len(target))
	for _, r := range target {
		targetByID[r.observation.VCenterID+"\x00"+r.observation.ID] = r
	}
	var out []ResourceChange
	for key, r := range targetByID {
		before, ok := baseByID[key]
		if !ok {
			out = append(out, ResourceChange{Kind: kind, Changes: []string{"appeared"}, ID: r.observation.ID, Name: r.observation.Name, Context: r.observation.Context, After: &r.observation})
			continue
		}
		fields := changedResourceFields(kind, before.observation.Payload, r.observation.Payload, includeRuntime)
		if len(fields) > 0 {
			out = append(out, ResourceChange{Kind: kind, Changes: []string{"modified"}, ID: r.observation.ID, Name: r.observation.Name, Context: r.observation.Context, MatchBasis: "moref", Fields: fields, Before: &before.observation, After: &r.observation})
		}
		delete(baseByID, key)
	}
	for _, r := range baseByID {
		out = append(out, ResourceChange{Kind: kind, Changes: []string{"vanished"}, ID: r.observation.ID, Name: r.observation.Name, Context: r.observation.Context, Before: &r.observation})
	}
	return out
}

var resourceStableFields = map[string][]string{
	"host":      {"name", "datacenter", "path", "cluster", "vendor", "model", "version", "build", "cpu_cores", "cpu_threads", "cpu_mhz", "memory_mb"},
	"cluster":   {"name", "datacenter", "path", "standalone", "cpu_cores", "total_cpu_mhz", "total_memory_mb", "drs_enabled", "ha_enabled"},
	"datastore": {"name", "datacenter", "path", "type"},
}

var resourceRuntimeFields = map[string][]string{
	"host":      {"power_state", "connection_state", "in_maintenance", "vm_count", "cpu_usage_mhz", "memory_usage_mb"},
	"cluster":   {"hosts", "effective_hosts"},
	"datastore": {"accessible", "maintenance", "capacity_bytes", "free_bytes"},
}

func changedResourceFields(kind string, before, after json.RawMessage, includeRuntime bool) []FieldChange {
	var bm, am map[string]any
	if json.Unmarshal(before, &bm) != nil || json.Unmarshal(after, &am) != nil {
		if string(before) != string(after) {
			return []FieldChange{{Field: "payload", Before: string(before), After: string(after)}}
		}
		return nil
	}
	keys := append([]string(nil), resourceStableFields[kind]...)
	if includeRuntime {
		keys = append(keys, resourceRuntimeFields[kind]...)
	}
	var out []FieldChange
	for _, key := range keys {
		bv, bok := resourceJSONValue(bm, key)
		av, aok := resourceJSONValue(am, key)
		bs, as := jsonValueString(bv, bok), jsonValueString(av, aok)
		if bs != as {
			out = append(out, FieldChange{Field: key, Before: bs, After: as})
		}
	}
	return out
}

func resourceJSONValue(m map[string]any, path string) (any, bool) {
	if value, ok := resourceJSONValuePath(m, strings.Split(path, ".")); ok {
		return value, true
	}
	// Location is embedded in the native vSphere structs (flat JSON), but
	// accepting a nested representation keeps imported/versioned observations
	// comparable as well.
	if path == "datacenter" || path == "path" {
		if location, ok := m["location"].(map[string]any); ok {
			if value, exists := location[path]; exists {
				return value, true
			}
		}
	}
	return nil, false
}

func resourceJSONValuePath(m map[string]any, parts []string) (any, bool) {
	var current any = m
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func jsonValueString(v any, ok bool) string {
	if !ok {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func contextVCenter(ctx context.Context, s *Store, runID int64, contextName string) string {
	var vc string
	_ = s.db.QueryRowContext(ctx, `SELECT vcenter_id FROM context_runs WHERE run_id=? AND name=?`, runID, contextName).Scan(&vc)
	return vc
}
