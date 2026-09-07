package health

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Confidence is the strength of an orphan conclusion.
type Confidence string

const (
	ConfidenceVerified     Confidence = "verified-unreferenced"
	ConfidenceSuspected    Confidence = "suspected-unreferenced"
	ConfidenceOtherContext Confidence = "referenced-other-context"
	ConfidenceUnknown      Confidence = "unknown-incomplete-coverage"
)

// OrphanReference identifies the VM or template which uses a disk path.
type OrphanReference struct {
	Context   string `json:"context"`
	VCenterID string `json:"vcenter_id,omitempty"`
	VM        string `json:"vm"`
	Template  bool   `json:"template,omitempty"`
	Path      string `json:"path"`
}

// OrphanBlindness explains why a candidate could not receive a high-confidence
// verdict. It is intentionally structured so the CLI and JSON consumers can
// show the same coverage evidence.
type OrphanBlindness struct {
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

// OrphanEvidence is the complete evidence record for one browsed VMDK.
type OrphanEvidence struct {
	Confidence      Confidence        `json:"confidence"`
	Object          Object            `json:"object"`
	Path            string            `json:"path"`
	SizeBytes       int64             `json:"size_bytes"`
	Modified        time.Time         `json:"modified,omitempty"`
	Identity        []string          `json:"identity,omitempty"`
	CheckedContexts []string          `json:"checked_contexts,omitempty"`
	Blind           []OrphanBlindness `json:"blind,omitempty"`
	ReferencedBy    []OrphanReference `json:"referenced_by,omitempty"`
	Reasons         []string          `json:"reasons,omitempty"`
}

// OrphanReport contains every browsed VMDK candidate, including unknown ones.
// Unknown entries are retained for drill-down but are not health findings.
type OrphanReport struct {
	RunID   int64            `json:"run_id"`
	Entries []OrphanEvidence `json:"entries"`
}

func orphanConfidenceLabel(value Confidence) string {
	switch value {
	case ConfidenceVerified:
		return "Verified-unreferenced"
	case ConfidenceSuspected:
		return "Suspected-unreferenced"
	case ConfidenceOtherContext:
		return "Referenced in another context"
	default:
		return "Unknown"
	}
}

type orphanDatastore struct {
	resource assessment.ResourceObservation
	ds       vsphere.Datastore
	keys     []string
	weakKey  string
	local    bool
}

type orphanReference struct {
	ref     OrphanReference
	context string
	key     string
	path    string
	stem    string
	weakKey string
	blind   bool
}

type orphanIndex struct {
	byKey  map[string][]orphanReference
	byWeak map[string][]orphanReference
}

// Orphans weighs every browsed datastore file in a stored assessment against
// every VM and template disk reference in the whole estate. It is a pure
// function of the run and never reads wall clock time; recent is relative to
// data.Run.FinishedAt.
func Orphans(data assessment.ExportData) OrphanReport {
	stores := make([]orphanDatastore, 0)
	byContextName := make(map[string][]orphanDatastore)
	for _, resource := range data.Resources {
		if resource.Kind != "datastore" {
			continue
		}
		var datastore vsphere.Datastore
		if !assessment.DecodeResource(resource, &datastore) {
			continue
		}
		store := orphanDatastore{resource: resource, ds: datastore, keys: datastore.IdentityKeys(), local: datastore.Backing.Local}
		store.weakKey = weakDatastoreKey(resource.Context, datastore.Name)
		stores = append(stores, store)
		byContextName[contextNameKey(resource.Context, datastore.Name)] = append(byContextName[contextNameKey(resource.Context, datastore.Name)], store)
	}

	index := orphanIndex{byKey: make(map[string][]orphanReference), byWeak: make(map[string][]orphanReference)}
	for _, item := range data.VMs {
		vm := item.Observation.VM
		for _, disk := range vm.Disks {
			name, relative, ok := vsphere.SplitDatastorePath(disk.BackingPath)
			if !ok || relative == "" {
				continue
			}
			refContext := item.Observation.Context
			if refContext == "" {
				refContext = vm.Context
			}
			matching := byContextName[contextNameKey(refContext, name)]
			if refContext == "" {
				matching = storesNamed(byContextName, name)
				if len(matching) == 1 {
					refContext = matching[0].resource.Context
				}
			}
			candidate := orphanReference{
				context: refContext,
				path:    vsphere.NormalizeRelativePath(relative),
				stem:    snapshotStem(relative),
				weakKey: weakDatastoreKey(refContext, name),
				ref: OrphanReference{
					Context:   refContext,
					VCenterID: item.Observation.VCenterID,
					VM:        vm.Name,
					Template:  vm.IsTemplate,
					Path:      vsphere.NormalizeRelativePath(relative),
				},
			}
			if len(matching) == 0 {
				candidate.blind = true
				index.byWeak[candidate.weakKey] = append(index.byWeak[candidate.weakKey], candidate)
				continue
			}
			registered := false
			for _, store := range matching {
				if len(store.keys) == 0 {
					continue
				}
				registered = true
				for _, key := range store.keys {
					candidate.key = key
					index.byKey[key] = append(index.byKey[key], candidate)
				}
			}
			if !registered {
				candidate.blind = true
			}
			index.byWeak[candidate.weakKey] = append(index.byWeak[candidate.weakKey], candidate)
		}
	}
	entries := make([]OrphanEvidence, 0)
	for _, store := range stores {
		if store.ds.BrowseStatus != "success" {
			continue
		}
		for _, file := range store.ds.Files {
			name, relative, ok := vsphere.SplitDatastorePath(file.Path)
			if !ok || !strings.HasSuffix(strings.ToLower(relative), ".vmdk") || sidecarVMDK(relative) {
				continue
			}
			// A malformed file path is not evidence of an orphan. Preserve it as
			// unknown so the drill-down exposes the incomplete observation.
			if name == "" {
				continue
			}
			entry := OrphanEvidence{
				Object: resourceObject(data, store.resource, "datastore", store.ds.Name, store.ds.ID, store.ds.Datacenter),
				Path:   file.Path, SizeBytes: file.SizeBytes, Modified: file.Modified,
				Identity: append([]string(nil), store.keys...),
			}
			if store.local {
				entry.Identity = append(entry.Identity, "local datastore (context-scoped)")
			}
			entry.CheckedContexts = checkedContexts(data, store)
			refs := matchingReferences(index, store, relative)
			entry.ReferencedBy = uniqueReferences(refs)
			for _, ref := range entry.ReferencedBy {
				if ref.Context == store.resource.Context {
					// A live reference in the candidate's own context is not an
					// orphan, regardless of how much other coverage is missing.
					entry = OrphanEvidence{}
					break
				}
			}
			if entry.Path == "" {
				continue
			}
			entry.Blind = blindnessFor(data, store, byContextName, index, relative)
			entry.Reasons = blindnessReasons(entry.Blind)

			schema := inventorySchema(data.Run.InventorySchemaVersion)
			if len(entry.Blind) > 0 || (!store.local && schema >= 11 && len(store.keys) == 0) {
				entry.Confidence = ConfidenceUnknown
				if !store.local && schema >= 11 && len(store.keys) == 0 {
					entry.Reasons = appendUnique(entry.Reasons, "datastore backing identity is unavailable")
				}
			} else if len(entry.ReferencedBy) > 0 {
				sameContext := false
				for _, ref := range entry.ReferencedBy {
					if ref.Context == store.resource.Context {
						sameContext = true
						break
					}
				}
				if sameContext {
					// Referenced candidates are deliberately omitted from the
					// report; this entry is retained only for the matching logic.
					continue
				}
				entry.Confidence = ConfidenceOtherContext
			} else {
				entry.Confidence = ConfidenceVerified
				if schema < 11 {
					entry.Confidence = ConfidenceSuspected
					entry.Reasons = appendUnique(entry.Reasons, "inventory schema predates datastore backing identity")
				}
				if folderHasReference(index, store, relative) {
					entry.Confidence = ConfidenceSuspected
					entry.Reasons = appendUnique(entry.Reasons, "the folder also contains a referenced disk")
				}
				if recentDisk(file.Modified, data.Run.FinishedAt) {
					entry.Confidence = ConfidenceSuspected
					entry.Reasons = appendUnique(entry.Reasons, "file was modified within 24 hours of the assessment")
				}
			}
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		for _, pair := range [][2]string{{entries[i].Object.Context, entries[j].Object.Context}, {entries[i].Object.Name, entries[j].Object.Name}, {entries[i].Path, entries[j].Path}} {
			if pair[0] != pair[1] {
				return strings.ToLower(pair[0]) < strings.ToLower(pair[1])
			}
		}
		return entries[i].Confidence < entries[j].Confidence
	})
	return OrphanReport{RunID: data.Run.ID, Entries: entries}
}

func storesNamed(byContextName map[string][]orphanDatastore, name string) []orphanDatastore {
	var out []orphanDatastore
	suffix := "\x00" + strings.ToLower(strings.TrimSpace(name))
	for key, stores := range byContextName {
		if strings.HasSuffix(key, suffix) {
			out = append(out, stores...)
		}
	}
	return out
}

func snapshotStem(value string) string {
	value = vsphere.NormalizeRelativePath(value)
	directory, filename := path.Split(value)
	ext := strings.TrimSuffix(filename, ".vmdk")
	if len(ext) > 7 {
		suffix := ext[len(ext)-7:]
		if suffix[0] == '-' && allDigits(suffix[1:]) {
			ext = ext[:len(ext)-7]
		}
	}
	return directory + ext
}

func sidecarVMDK(value string) bool {
	name := strings.ToLower(path.Base(vsphere.NormalizeRelativePath(value)))
	for _, suffix := range []string{"-flat.vmdk", "-delta.vmdk", "-sesparse.vmdk", "-ctk.vmdk", "-rdm.vmdk", "-rdmp.vmdk", "-digest.vmdk"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func weakDatastoreKey(context, name string) string {
	return strings.ToLower(strings.TrimSpace(context)) + "\x00name:" + strings.ToLower(strings.TrimSpace(name))
}

func contextNameKey(context, name string) string {
	return strings.ToLower(strings.TrimSpace(context)) + "\x00" + strings.ToLower(strings.TrimSpace(name))
}

func matchingReferences(index orphanIndex, store orphanDatastore, relative string) []orphanReference {
	pathValue := vsphere.NormalizeRelativePath(relative)
	stem := snapshotStem(relative)
	var candidates []orphanReference
	if len(store.keys) > 0 {
		for _, key := range store.keys {
			candidates = append(candidates, index.byKey[key]...)
		}
	} else {
		candidates = append(candidates, index.byWeak[store.weakKey]...)
	}
	out := make([]orphanReference, 0)
	for _, ref := range candidates {
		if ref.path == pathValue || ref.stem == stem {
			out = append(out, ref)
		}
	}
	return out
}

func uniqueReferences(values []orphanReference) []OrphanReference {
	seen := make(map[string]bool)
	out := make([]OrphanReference, 0, len(values))
	for _, value := range values {
		key := value.ref.Context + "\x00" + value.ref.VCenterID + "\x00" + value.ref.VM + "\x00" + value.ref.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value.ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].VM < out[j].VM
	})
	return out
}

func checkedContexts(data assessment.ExportData, store orphanDatastore) []string {
	seen := make(map[string]bool)
	for _, context := range data.Contexts {
		if context.Name == store.resource.Context || contextHasDatastoreName(data, context.Name, store.ds.Name) {
			seen[context.Name] = true
		}
	}
	seen[store.resource.Context] = true
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func blindnessFor(data assessment.ExportData, store orphanDatastore, byContextName map[string][]orphanDatastore, index orphanIndex, relative string) []OrphanBlindness {
	blind := make([]OrphanBlindness, 0)
	schema := inventorySchema(data.Run.InventorySchemaVersion)
	if schema < 11 {
		return blind
	}
	if store.ds.BrowseTruncated {
		blind = append(blind, OrphanBlindness{Context: store.resource.Context, Reason: "datastore browse listing was truncated"})
	}
	for _, context := range data.Contexts {
		if context.Name == store.resource.Context || contextSharesStore(context.Name, store, byContextName) {
			if reason, ok := contextCoverageFailure(context); ok {
				blind = append(blind, OrphanBlindness{Context: context.Name, Reason: reason})
			}
			if context.Name != store.resource.Context && contextHasDatastoreName(data, context.Name, store.ds.Name) && !contextHasStrongStore(byContextName, context.Name, store.keys) {
				blind = append(blind, OrphanBlindness{Context: context.Name, Reason: "datastore backing identity is unavailable"})
			}
		}
	}
	if len(data.Contexts) == 0 {
		return blind
	}
	if !store.local && len(store.keys) == 0 {
		blind = append(blind, OrphanBlindness{Context: store.resource.Context, Reason: "datastore backing identity is unavailable"})
	}
	for _, ref := range index.byWeak[store.weakKey] {
		if ref.blind && ref.context == store.resource.Context && ref.path == vsphere.NormalizeRelativePath(relative) {
			blind = append(blind, OrphanBlindness{Context: ref.context, Reason: "VM references a datastore name without an observed datastore identity"})
		}
	}
	return uniqueBlindness(blind)
}

func contextSharesStore(context string, store orphanDatastore, byContextName map[string][]orphanDatastore) bool {
	for key, stores := range byContextName {
		if !strings.HasPrefix(key, strings.ToLower(context)+"\x00") {
			continue
		}
		for _, other := range stores {
			if len(store.keys) == 0 || len(other.keys) == 0 {
				continue
			}
			if anyStringIntersection(store.keys, other.keys) {
				return true
			}
		}
	}
	return false
}

func contextHasStrongStore(byContextName map[string][]orphanDatastore, context string, keys []string) bool {
	for _, store := range byContextName[contextNameKey(context, "")] {
		if anyStringIntersection(store.keys, keys) {
			return true
		}
	}
	for key, stores := range byContextName {
		if !strings.HasPrefix(key, strings.ToLower(context)+"\x00") {
			continue
		}
		for _, store := range stores {
			if anyStringIntersection(store.keys, keys) {
				return true
			}
		}
	}
	return false
}

func contextHasDatastoreName(data assessment.ExportData, context, name string) bool {
	for _, resource := range data.Resources {
		if resource.Kind == "datastore" && resource.Context == context && strings.EqualFold(resource.Name, name) {
			return true
		}
	}
	return false
}

func contextCoverageFailure(context assessment.ContextRun) (string, bool) {
	if !assessment.Successful(context.VMStatus) {
		return "vm collection: " + nonempty(context.Error, context.VMStatus), true
	}
	for _, collection := range context.Collections {
		if collection.Kind == "datastore" && !assessment.Successful(collection.Status) {
			return "datastore collection: " + nonempty(collection.Error, collection.Status), true
		}
	}
	for _, collection := range context.Collections {
		if collection.Kind == "vm" && !assessment.Successful(collection.Status) {
			return "vm collection: " + nonempty(collection.Error, collection.Status), true
		}
	}
	return "", false
}

func folderHasReference(index orphanIndex, store orphanDatastore, relative string) bool {
	directory := path.Dir(vsphere.NormalizeRelativePath(relative))
	for _, ref := range uniqueReferences(referencesForStore(index, store)) {
		if path.Dir(ref.Path) == directory {
			return true
		}
	}
	return false
}

func referencesForStore(index orphanIndex, store orphanDatastore) []orphanReference {
	var candidates []orphanReference
	if len(store.keys) > 0 {
		for _, key := range store.keys {
			candidates = append(candidates, index.byKey[key]...)
		}
	} else {
		candidates = append(candidates, index.byWeak[store.weakKey]...)
	}
	return candidates
}

func recentDisk(modified, finished time.Time) bool {
	return !modified.IsZero() && !finished.IsZero() && modified.After(finished.Add(-24*time.Hour)) && !modified.After(finished.Add(time.Minute))
}

func blindnessReasons(values []OrphanBlindness) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = appendUnique(out, value.Context+": "+value.Reason)
	}
	return out
}

func uniqueBlindness(values []OrphanBlindness) []OrphanBlindness {
	seen := make(map[string]bool)
	out := make([]OrphanBlindness, 0, len(values))
	for _, value := range values {
		key := value.Context + "\x00" + value.Reason
		if !seen[key] {
			seen[key] = true
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Context < out[j].Context })
	return out
}

func anyStringIntersection(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, value := range a {
		set[value] = true
	}
	for _, value := range b {
		if set[value] {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
