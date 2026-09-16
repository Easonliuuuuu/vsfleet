// Package rvimport adapts RVTools-compatible XLSX exports into vsfleet's
// normalized assessment model.
//
// It is deliberately an adapter, not a second domain model: an RVTools
// workbook layout is a source format this package translates from, and
// nothing about that layout is allowed to leak into vsphere or assessment
// types. A worksheet or column this package does not recognize is reported
// as unrecognized rather than silently dropped or guessed at, and a field
// the workbook does not carry is left absent rather than defaulted to a
// value that would read as confirmed evidence.
//
// The whole workbook is parsed into memory before anything is written to the
// assessment store, and a run is written in one all-or-nothing pass — a
// malformed or partially-mappable workbook must not leave a half-written run
// behind.
package rvimport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// ProfileVersion identifies this adapter's worksheet/column mapping. It is
// recorded on every imported run's note so a future, richer profile can be
// told apart from runs an earlier version of this importer produced.
const ProfileVersion = "rvtools-v1"

// importSource marks an imported run's Run.Source, the one field every other
// command already reads, so "vsfleet assessment list" tells an imported run
// apart from a live capture without inspecting its note text.
const importSource = "rvtools-import"

// importedSchemaVersion is the inventory schema level this profile honestly
// claims: "2", the version that added per-VM disks and network adapters,
// which vDisk and vNetwork give it. Claiming assessment.CurrentInventorySchemaVersion
// instead would tell every schema-gated health rule that Tools status,
// guest partitions, migration configuration and everything else added by a
// later schema was collected and came back empty — turning "this workbook
// never carried that evidence" into a false "confirmed absent" for whatever
// those rules check. Health rules gated by schema stay silently
// not-evaluated instead, the same way they do for an old live capture.
const importedSchemaVersion = "2"

// Recognized worksheets. Worksheet names are matched exactly, the way RVTools
// itself names its tabs; anything else is reported as ignored.
const (
	sheetVInfo      = "vInfo"
	sheetVCPU       = "vCPU"
	sheetVMemory    = "vMemory"
	sheetVDisk      = "vDisk"
	sheetVNetwork   = "vNetwork"
	sheetVHost      = "vHost"
	sheetVCluster   = "vCluster"
	sheetVDatastore = "vDatastore"
)

// skippedKinds are the persisted collection kinds this profile version has no
// worksheet mapping for at all — RVTools' layout has no standalone
// datastore-network or resource-pool/DVS worksheet the way it has vInfo or
// vHost. They are recorded as an explicit, named gap on every imported
// context rather than left silently absent, so a diff or health rule that
// needs them reports "not evaluated" rather than "collection was not
// recorded" with no explanation.
var skippedKinds = []struct{ kind, reason string }{
	{"resourcepool", "RVTools has no standalone resource-pool worksheet in this profile"},
	{"dvswitch", "RVTools has no standalone distributed-switch worksheet in this profile"},
	{"network", "RVTools has no standalone network worksheet; port-group evidence lives in vSwitch/vPort/dvPort, not yet mapped by this importer"},
}

const (
	// maxWorkbookBytes bounds the file this package will open at all, applied
	// before excelize ever sees the bytes.
	maxWorkbookBytes = 200 << 20
	// maxUnzipBytes and maxUnzipXMLBytes bound what excelize itself will
	// inflate the archive to, so a small file that is a zip bomb cannot
	// exhaust memory once past the size check above.
	maxUnzipBytes    = 512 << 20
	maxUnzipXMLBytes = 128 << 20
)

// mib mirrors internal/report's own conversion constant. RVTools sizes disks
// and datastores in MiB; vsphere types are bytes.
const mib = float64(1 << 20)

// OpenFile opens a workbook for import with bounded memory use and no
// formula evaluation or external link resolution — excelize.GetRows never
// triggers either on its own, and these limits are this package's own
// defense against a hostile or corrupt file.
func OpenFile(path string) (*excelize.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxWorkbookBytes {
		return nil, fmt.Errorf("workbook is %d bytes, over the %d byte import limit", info.Size(), int64(maxWorkbookBytes))
	}
	f, err := excelize.OpenFile(path, excelize.Options{UnzipSizeLimit: maxUnzipBytes, UnzipXMLSizeLimit: maxUnzipXMLBytes})
	if err != nil {
		return nil, fmt.Errorf("open workbook: %w", err)
	}
	return f, nil
}

// Options configures one import.
type Options struct {
	// SourceLabel is a human-readable name for the workbook — typically its
	// filename — recorded in the run's provenance. It is never parsed.
	SourceLabel string
	// CapturedAt is the explicit --captured-at time, or the zero value to
	// fall back to the workbook's own metadata and then import time.
	CapturedAt time.Time
	// ContextMap renames a reconstructed context key (see Report.Contexts)
	// to the desired final context name. A key not present here keeps its
	// reconstructed name.
	ContextMap map[string]string
	Label      string
	Note       string
}

// ContextSummary is one reconstructed vCenter context and what was found for
// it, reported before anything is written so a dry run can show it.
type ContextSummary struct {
	// Key is the identity this workbook grouped rows by — the source
	// "vsfleet Context" value when present, otherwise the VI SDK Server
	// endpoint. It is what --context-map keys against.
	Key            string `json:"key"`
	Name           string `json:"name"`
	Endpoint       string `json:"endpoint,omitempty"`
	VCenterID      string `json:"vcenter_id,omitempty"`
	VMCount        int    `json:"vm_count"`
	HostCount      int    `json:"host_count"`
	ClusterCount   int    `json:"cluster_count"`
	DatastoreCount int    `json:"datastore_count"`
}

// Report describes what an import recognized and would do, in enough detail
// that a --dry-run needs nothing else to decide whether to proceed.
type Report struct {
	ProfileVersion   string           `json:"profile_version"`
	SourceLabel      string           `json:"source_label,omitempty"`
	RecognizedSheets []string         `json:"recognized_sheets"`
	IgnoredSheets    []string         `json:"ignored_sheets,omitempty"`
	Contexts         []ContextSummary `json:"contexts"`
	CapturedAt       time.Time        `json:"captured_at"`
	CapturedAtSource string           `json:"captured_at_source"`
	Warnings         []string         `json:"warnings,omitempty"`
	SkippedKinds     []string         `json:"skipped_kinds,omitempty"`
}

// importedContext accumulates one reconstructed vCenter's rows while parsing.
// It is a plain slice-of-objects builder, not assessment.ContextResult
// itself, because a dry run needs to summarize it without ever touching the
// store.
type importedContext struct {
	key        string
	name       string
	endpoint   string
	vcenterID  string
	vms        []*vsphere.VM
	hosts      []vsphere.Host
	clusters   []vsphere.Cluster
	datastores []vsphere.Datastore
}

// Result is a fully parsed workbook, ready to report or to write. Parsing
// never touches the assessment store; only Write does, and only after every
// row that could be read has been read.
type Result struct {
	Report   Report
	contexts []*importedContext
	opts     Options
}

// sheetTable is one worksheet's header-indexed rows. Columns are looked up by
// header name rather than position, so a workbook with columns reordered, or
// with extra columns this adapter does not know about, still imports.
type sheetTable struct {
	name  string
	index map[string]int
	rows  [][]string
}

func newSheetTable(name string, rows [][]string) sheetTable {
	if len(rows) == 0 {
		return sheetTable{name: name}
	}
	index := make(map[string]int, len(rows[0]))
	for i, h := range rows[0] {
		key := normalizeHeader(h)
		if key == "" {
			continue
		}
		if _, exists := index[key]; !exists {
			index[key] = i
		}
	}
	return sheetTable{name: name, index: index, rows: rows[1:]}
}

func normalizeHeader(h string) string { return strings.ToLower(strings.TrimSpace(h)) }

func (t sheetTable) cell(row []string, header string) string {
	i, ok := t.index[normalizeHeader(header)]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func isSupportedSheet(name string) bool {
	switch name {
	case sheetVInfo, sheetVCPU, sheetVMemory, sheetVDisk, sheetVNetwork, sheetVHost, sheetVCluster, sheetVDatastore:
		return true
	default:
		return false
	}
}

func isBlankRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// rowContext extracts the identity this row's vCenter is grouped and named
// by. "vsfleet Context" is preferred when present — it is the literal
// original context name for a workbook vsfleet itself exported, the
// strongest identity this profile can have. Otherwise the VI SDK Server
// endpoint is the grouping key real RVTools output carries. A row with
// neither has no usable provenance and is not silently merged into whatever
// context came before it.
func rowContext(t sheetTable, row []string) (key, name, endpoint, vcenterID string, ok bool) {
	endpoint = t.cell(row, "VI SDK Server")
	vcenterID = t.cell(row, "VI SDK UUID")
	if vsfleetContext := t.cell(row, "vsfleet Context"); vsfleetContext != "" {
		return vsfleetContext, vsfleetContext, endpoint, vcenterID, true
	}
	if endpoint != "" {
		return endpoint, endpoint, endpoint, vcenterID, true
	}
	return "", "", "", "", false
}

// vmIdentityKey prefers the managed-object ID RVTools calls "VM ID" — a
// stable identity a display name is not — and falls back to name only when
// the workbook carries nothing stronger.
func vmIdentityKey(t sheetTable, row []string) string {
	if id := t.cell(row, "VM ID"); id != "" {
		return "id:" + id
	}
	if name := t.cell(row, "VM"); name != "" {
		return "name:" + name
	}
	return ""
}

// Parse reads every recognized worksheet into memory and reconstructs
// contexts, VMs, hosts, clusters and datastores from them. It never touches
// the assessment store: a caller decides separately, from the returned
// Report, whether to write the result or was only asked for --dry-run.
func Parse(f *excelize.File, opts Options) (*Result, error) {
	sheetNames := f.GetSheetList()
	tables := make(map[string]sheetTable, len(sheetNames))
	var recognized, ignored []string
	for _, name := range sheetNames {
		if !isSupportedSheet(name) {
			ignored = append(ignored, name)
			continue
		}
		rows, err := f.GetRows(name)
		if err != nil {
			return nil, fmt.Errorf("read worksheet %q: %w", name, err)
		}
		tables[name] = newSheetTable(name, rows)
		recognized = append(recognized, name)
	}
	sort.Strings(ignored)
	sort.Strings(recognized)

	vInfo, ok := tables[sheetVInfo]
	if !ok {
		return nil, fmt.Errorf("workbook has no %s worksheet; VM identity cannot be reconstructed without it", sheetVInfo)
	}

	var warnings []string
	byKey := make(map[string]*importedContext)
	var order []string
	contextFor := func(key, name, endpoint, vcenterID string) *importedContext {
		if c, exists := byKey[key]; exists {
			return c
		}
		final := name
		if mapped, remapped := opts.ContextMap[key]; remapped && strings.TrimSpace(mapped) != "" {
			final = strings.TrimSpace(mapped)
		}
		c := &importedContext{key: key, name: final, endpoint: endpoint, vcenterID: vcenterID}
		byKey[key] = c
		order = append(order, key)
		return c
	}

	vmIndex := make(map[string]*vsphere.VM)
	for _, row := range vInfo.rows {
		if isBlankRow(row) {
			continue
		}
		key, name, endpoint, vcenterID, ok := rowContext(vInfo, row)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s: row for VM %q has neither a VI SDK Server nor a vsfleet Context column; skipped", sheetVInfo, vInfo.cell(row, "VM")))
			continue
		}
		vmKey := vmIdentityKey(vInfo, row)
		if vmKey == "" {
			warnings = append(warnings, fmt.Sprintf("%s: row has neither VM ID nor VM name; skipped", sheetVInfo))
			continue
		}
		ctx := contextFor(key, name, endpoint, vcenterID)
		vm := vmFromInfoRow(vInfo, row, ctx.name)
		ctx.vms = append(ctx.vms, vm)
		vmIndex[key+"|"+vmKey] = vm
	}

	attachVMRows := func(sheetName string, apply func(sheetTable, []string, *vsphere.VM)) {
		t, ok := tables[sheetName]
		if !ok {
			return
		}
		for _, row := range t.rows {
			if isBlankRow(row) {
				continue
			}
			key, _, _, _, ok := rowContext(t, row)
			if !ok {
				continue
			}
			vmKey := vmIdentityKey(t, row)
			if vmKey == "" {
				continue
			}
			vm, found := vmIndex[key+"|"+vmKey]
			if !found {
				warnings = append(warnings, fmt.Sprintf("%s: row references a VM not present in %s; skipped", sheetName, sheetVInfo))
				continue
			}
			apply(t, row, vm)
		}
	}
	attachVMRows(sheetVDisk, applyDiskRow)
	attachVMRows(sheetVNetwork, applyNetworkRow)

	attachResourceRows(tables, sheetVHost, &warnings, contextFor, func(c *importedContext, t sheetTable, row []string) {
		c.hosts = append(c.hosts, hostFromRow(t, row, c.name))
	})
	attachResourceRows(tables, sheetVCluster, &warnings, contextFor, func(c *importedContext, t sheetTable, row []string) {
		c.clusters = append(c.clusters, clusterFromRow(t, row, c.name))
	})
	attachResourceRows(tables, sheetVDatastore, &warnings, contextFor, func(c *importedContext, t sheetTable, row []string) {
		c.datastores = append(c.datastores, datastoreFromRow(t, row, c.name))
	})

	if len(order) == 0 {
		return nil, fmt.Errorf("no vCenter/context identity could be reconstructed from this workbook")
	}
	sort.Strings(order)

	capturedAt, capturedAtSource := resolveCapturedAt(f, opts.CapturedAt)

	contexts := make([]*importedContext, 0, len(order))
	summaries := make([]ContextSummary, 0, len(order))
	for _, key := range order {
		c := byKey[key]
		contexts = append(contexts, c)
		summaries = append(summaries, ContextSummary{
			Key: c.key, Name: c.name, Endpoint: c.endpoint, VCenterID: c.vcenterID,
			VMCount: len(c.vms), HostCount: len(c.hosts), ClusterCount: len(c.clusters), DatastoreCount: len(c.datastores),
		})
	}
	skipped := make([]string, 0, len(skippedKinds))
	for _, s := range skippedKinds {
		skipped = append(skipped, s.kind)
	}

	report := Report{
		ProfileVersion:   ProfileVersion,
		SourceLabel:      opts.SourceLabel,
		RecognizedSheets: recognized,
		IgnoredSheets:    ignored,
		Contexts:         summaries,
		CapturedAt:       capturedAt,
		CapturedAtSource: capturedAtSource,
		Warnings:         warnings,
		SkippedKinds:     skipped,
	}
	return &Result{Report: report, contexts: contexts, opts: opts}, nil
}

// attachResourceRows is the shared walk for the three one-row-per-object
// sheets (vHost, vCluster, vDatastore): each row both identifies its context
// and is entirely self-contained, unlike vDisk/vNetwork which attach to a VM
// identified elsewhere.
func attachResourceRows(tables map[string]sheetTable, sheetName string, warnings *[]string, contextFor func(key, name, endpoint, vcenterID string) *importedContext, apply func(*importedContext, sheetTable, []string)) {
	t, ok := tables[sheetName]
	if !ok {
		return
	}
	for _, row := range t.rows {
		if isBlankRow(row) {
			continue
		}
		key, name, endpoint, vcenterID, ok := rowContext(t, row)
		if !ok {
			*warnings = append(*warnings, fmt.Sprintf("%s: row has neither a VI SDK Server nor a vsfleet Context column; skipped", sheetName))
			continue
		}
		ctx := contextFor(key, name, endpoint, vcenterID)
		apply(ctx, t, row)
	}
}

func resolveCapturedAt(f *excelize.File, explicit time.Time) (time.Time, string) {
	if !explicit.IsZero() {
		return explicit.UTC(), "explicit --captured-at"
	}
	if props, err := f.GetDocProps(); err == nil && props != nil {
		for _, value := range []string{props.Created, props.Modified} {
			if value == "" {
				continue
			}
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				return t.UTC(), "workbook document properties"
			}
		}
	}
	return time.Now().UTC(), "import time (the workbook carried no usable captured-at metadata)"
}

func vmFromInfoRow(t sheetTable, row []string, contextName string) *vsphere.VM {
	return &vsphere.VM{
		Location:     vsphere.Location{Context: contextName, Datacenter: t.cell(row, "Datacenter")},
		ID:           t.cell(row, "VM ID"),
		InstanceUUID: t.cell(row, "VM UUID"),
		BIOSUUID:     t.cell(row, "VM SMBIOS UUID"),
		Name:         t.cell(row, "VM"),
		PowerState:   t.cell(row, "Powerstate"),
		IsTemplate:   parseBool(t.cell(row, "Template")),
		GuestState:   t.cell(row, "Guest state"),
		CPU:          int32(parseInt(t.cell(row, "CPUs"))),
		MemoryMB:     parseInt(t.cell(row, "Memory")),
		IPAddress:    t.cell(row, "Primary IP Address"),
		Host:         t.cell(row, "Host"),
		Cluster:      t.cell(row, "Cluster"),
		Folder:       t.cell(row, "Folder"),
		StorageGB:    parseFloat(t.cell(row, "In Use MiB")) / 1024,
		Annotation:   t.cell(row, "Annotation"),
		GuestOS:      t.cell(row, "OS according to the configuration file"),
		// ConfigurationAvailable is deliberately left false: RVTools exposes
		// only the subset of configuration mapped below, never the deep
		// hardware configuration that flag promises to callers.
	}
}

func applyDiskRow(t sheetTable, row []string, vm *vsphere.VM) {
	vm.Disks = append(vm.Disks, vsphere.VMDisk{
		Label:                t.cell(row, "Disk"),
		Key:                  int32(parseInt(t.cell(row, "Disk Key"))),
		UUID:                 t.cell(row, "Disk UUID"),
		CapacityBytes:        int64(parseFloat(t.cell(row, "Capacity MiB")) * mib),
		Raw:                  parseBool(t.cell(row, "Raw")),
		DiskMode:             t.cell(row, "Disk Mode"),
		Sharing:              t.cell(row, "Sharing mode"),
		ThinProvisioned:      parseOptBool(t.cell(row, "Thin")),
		EagerlyScrub:         parseOptBool(t.cell(row, "Eagerly Scrub")),
		Split:                parseOptBool(t.cell(row, "Split")),
		WriteThrough:         parseOptBool(t.cell(row, "Write Through")),
		SharesLevel:          t.cell(row, "Level"),
		Shares:               parseOptInt32(t.cell(row, "Shares")),
		Reservation:          parseOptInt32(t.cell(row, "Reservation")),
		Limit:                parseOptInt64(t.cell(row, "Limit")),
		Controller:           t.cell(row, "Controller"),
		ControllerLabel:      t.cell(row, "SCSI label"),
		UnitNumber:           parseOptInt32(t.cell(row, "Unit number")),
		SharedBus:            t.cell(row, "SharedBus"),
		BackingPath:          t.cell(row, "Path"),
		RawLUNID:             t.cell(row, "Raw LUN ID"),
		RawCompatibilityMode: t.cell(row, "Raw Compatibility Mode"),
	})
}

func applyNetworkRow(t sheetTable, row []string, vm *vsphere.VM) {
	vm.NICs = append(vm.NICs, vsphere.VMNIC{
		Label:           t.cell(row, "NIC label"),
		Adapter:         t.cell(row, "Adapter"),
		Network:         t.cell(row, "Network"),
		Connected:       parseOptBool(t.cell(row, "Connected")),
		StartsConnected: parseOptBool(t.cell(row, "Starts Connected")),
		MACAddress:      t.cell(row, "Mac Address"),
		MACAddressType:  t.cell(row, "Mac Address type"),
		IPv4:            splitList(t.cell(row, "IPv4 Address")),
		IPv6:            splitList(t.cell(row, "IPv6 Address")),
		UPTCompatible:   parseOptBool(t.cell(row, "Direct Path IO")),
	})
}

func hostFromRow(t sheetTable, row []string, contextName string) vsphere.Host {
	return vsphere.Host{
		Location:      vsphere.Location{Context: contextName, Datacenter: t.cell(row, "Datacenter")},
		ID:            t.cell(row, "Object ID"),
		Name:          t.cell(row, "Host"),
		Cluster:       t.cell(row, "Cluster"),
		InMaintenance: parseBool(t.cell(row, "in Maintenance Mode")),
		CPUMHz:        int32(parseInt(t.cell(row, "Speed"))),
		CPUCores:      int32(parseInt(t.cell(row, "# Cores"))),
		MemoryMB:      parseInt(t.cell(row, "# Memory")),
		VMCount:       int(parseInt(t.cell(row, "# VMs total"))),
		Version:       t.cell(row, "ESX Version"),
		Vendor:        t.cell(row, "Vendor"),
		Model:         t.cell(row, "Model"),
	}
}

func clusterFromRow(t sheetTable, row []string, contextName string) vsphere.Cluster {
	return vsphere.Cluster{
		Location:      vsphere.Location{Context: contextName, Datacenter: t.cell(row, "Datacenter")},
		ID:            t.cell(row, "Object ID"),
		Name:          t.cell(row, "Name"),
		Hosts:         int(parseInt(t.cell(row, "NumHosts"))),
		EffectiveHost: int(parseInt(t.cell(row, "NumEffectiveHosts"))),
		CPUCores:      int32(parseInt(t.cell(row, "NumCpuCores"))),
		TotalCPUMHz:   parseInt(t.cell(row, "TotalCpu")),
		TotalMemoryMB: parseInt(t.cell(row, "TotalMemory")),
		HAEnabled:     parseBool(t.cell(row, "HA enabled")),
		DRSEnabled:    parseBool(t.cell(row, "DRS enabled")),
	}
}

func datastoreFromRow(t sheetTable, row []string, contextName string) vsphere.Datastore {
	return vsphere.Datastore{
		Location:      vsphere.Location{Context: contextName, Datacenter: t.cell(row, "Datacenter")},
		ID:            t.cell(row, "Object ID"),
		Name:          t.cell(row, "Name"),
		Type:          t.cell(row, "Type"),
		CapacityBytes: int64(parseFloat(t.cell(row, "Capacity MiB")) * mib),
		FreeBytes:     int64(parseFloat(t.cell(row, "Free MiB")) * mib),
		Accessible:    parseBool(t.cell(row, "Accessible")),
		Maintenance:   t.cell(row, "Maintenance mode"),
	}
}

func parseBool(s string) bool {
	v, _ := strconv.ParseBool(strings.TrimSpace(s))
	return v
}

func parseOptBool(s string) *bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return nil
	}
	return &v
}

// parseInt tolerates the numeric-cell forms excelize's GetRows can produce
// (plain integers and "123.0"-style floats alike) and returns zero for
// anything it cannot parse or an empty cell.
func parseInt(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseOptInt32(s string) *int32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	v := int32(f)
	return &v
}

func parseOptInt64(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	v := int64(f)
	return &v
}

func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Write persists a parsed result as one new assessment run. It is
// all-or-nothing: if any context fails to save after the run row itself was
// created, the run is deleted rather than left behind half-written.
func (r *Result) Write(ctx context.Context, store *assessment.Store, now time.Time) (assessment.Run, error) {
	if len(r.contexts) == 0 {
		return assessment.Run{}, fmt.Errorf("nothing to import")
	}
	configContexts := make([]*config.Context, 0, len(r.contexts))
	for _, c := range r.contexts {
		configContexts = append(configContexts, &config.Context{Name: c.name, Endpoint: c.endpoint})
	}
	run, err := store.StartRunWithMetadata(ctx, importSource, configContexts, now, assessment.RunMetadata{
		Label:                  r.opts.Label,
		Note:                   buildNote(r.opts, r.Report),
		InventorySchemaVersion: importedSchemaVersion,
	})
	if err != nil {
		return assessment.Run{}, err
	}
	for _, c := range r.contexts {
		result := assessment.ContextResult{Name: c.name, VCenterID: c.vcenterID, Status: "success"}
		for _, vm := range c.vms {
			result.VMs = append(result.VMs, assessment.Observation{VCenterID: c.vcenterID, Context: c.name, VM: *vm})
		}
		result.Collections = append(result.Collections, assessment.CollectionResult{Kind: "vm", Status: vmCollectionStatus(len(c.vms)), ItemCount: len(c.vms)})
		result.Collections = append(result.Collections, resourceCollection("host", c.vcenterID, c.name, c.hosts, func(h vsphere.Host) (string, string) { return h.ID, h.Name }))
		result.Collections = append(result.Collections, resourceCollection("cluster", c.vcenterID, c.name, c.clusters, func(v vsphere.Cluster) (string, string) { return v.ID, v.Name }))
		result.Collections = append(result.Collections, resourceCollection("datastore", c.vcenterID, c.name, c.datastores, func(v vsphere.Datastore) (string, string) { return v.ID, v.Name }))
		for _, skip := range skippedKinds {
			result.Collections = append(result.Collections, assessment.CollectionResult{Kind: skip.kind, Status: "unavailable", Error: skip.reason})
		}
		if err := store.SaveContext(ctx, run.ID, result, now); err != nil {
			_ = store.DeleteRun(context.WithoutCancel(ctx), run.ID)
			return assessment.Run{}, fmt.Errorf("save imported context %q: %w", c.name, err)
		}
	}
	finished, err := store.FinishRun(ctx, run.ID, now)
	if err != nil {
		_ = store.DeleteRun(context.WithoutCancel(ctx), run.ID)
		return assessment.Run{}, err
	}
	return finished, nil
}

func vmCollectionStatus(n int) string {
	if n == 0 {
		return "empty"
	}
	return "success"
}

func resourceCollection[T any](kind, vcenterID, contextName string, values []T, identity func(T) (id, name string)) assessment.CollectionResult {
	collection := assessment.CollectionResult{Kind: kind, Status: "success", ItemCount: len(values)}
	if len(values) == 0 {
		collection.Status = "empty"
		return collection
	}
	for _, v := range values {
		payload, err := json.Marshal(v)
		if err != nil {
			collection.Status, collection.Error = "failed", err.Error()
			continue
		}
		id, name := identity(v)
		collection.Resources = append(collection.Resources, assessment.ResourceObservation{VCenterID: vcenterID, Context: contextName, Kind: kind, ID: id, Name: name, Payload: payload})
	}
	return collection
}

func buildNote(opts Options, report Report) string {
	var b strings.Builder
	if note := strings.TrimSpace(opts.Note); note != "" {
		b.WriteString(note)
		b.WriteString("\n\n")
	}
	b.WriteString("Imported via \"vsfleet import rvtools\"\n")
	if report.SourceLabel != "" {
		fmt.Fprintf(&b, "Source file: %s\n", report.SourceLabel)
	}
	fmt.Fprintf(&b, "Captured at: %s (%s)\n", report.CapturedAt.Format(time.RFC3339), report.CapturedAtSource)
	fmt.Fprintf(&b, "Parser profile: %s\n", report.ProfileVersion)
	fmt.Fprintf(&b, "Worksheets recognized: %s\n", strings.Join(report.RecognizedSheets, ", "))
	if len(report.IgnoredSheets) > 0 {
		fmt.Fprintf(&b, "Worksheets ignored: %s\n", strings.Join(report.IgnoredSheets, ", "))
	}
	if len(report.SkippedKinds) > 0 {
		fmt.Fprintf(&b, "Not collected by this profile: %s\n", strings.Join(report.SkippedKinds, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
