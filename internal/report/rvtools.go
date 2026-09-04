// Package report contains offline renderers for persisted assessment data.
package report

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const (
	dateFormat = "yyyy/mm/dd hh:mm:ss"
	miB        = float64(1 << 20)
)

var (
	// vmTailHeaders is the identity/location/provenance tail shared by every
	// per-VM tab: vInfo has its own tail (it also carries the SMBIOS UUID and
	// omits Folder), but vDisk, vNetwork, vSnapshot, vCPU, vMemory, and vTools
	// all end in exactly these columns.
	vmTailHeaders    = []string{"Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	vmHeaders        = []string{"VM", "Powerstate", "Template", "Guest state", "CPUs", "Memory", "Primary IP Address", "Folder", "In Use MiB", "Annotation", "Datacenter", "Cluster", "Host", "OS according to the configuration file", "VM ID", "VM SMBIOS UUID", "VM UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	cpuHeaders       = append([]string{"VM", "Powerstate", "Template", "CPUs"}, vmTailHeaders...)
	memoryHeaders    = append([]string{"VM", "Powerstate", "Template", "Size MiB"}, vmTailHeaders...)
	diskHeaders      = []string{"VM", "Powerstate", "Template", "Disk", "Disk Key", "Disk UUID", "Capacity MiB", "Raw", "Disk Mode", "Sharing mode", "Thin", "Eagerly Scrub", "Split", "Write Through", "Level", "Shares", "Reservation", "Limit", "Controller", "SCSI label", "Unit number", "SharedBus", "Path", "Raw LUN ID", "Raw Compatibility Mode", "Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	networkHeaders   = []string{"VM", "Powerstate", "Template", "NIC label", "Adapter", "Network", "Connected", "Starts Connected", "Mac Address", "Mac Address type", "IPv4 Address", "IPv6 Address", "Direct Path IO", "Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	toolsHeaders     = append([]string{"VM", "Powerstate", "Template", "Tools", "Tools Version", "Tools Version Status"}, vmTailHeaders...)
	hostHeaders      = []string{"Host", "Datacenter", "Cluster", "in Maintenance Mode", "Speed", "# Cores", "CPU usage %", "# Memory", "Memory usage %", "# VMs total", "ESX Version", "Vendor", "Model", "Object ID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	clusterHeaders   = []string{"Name", "NumHosts", "NumEffectiveHosts", "TotalCpu", "NumCpuCores", "TotalMemory", "HA enabled", "DRS enabled", "Object ID", "Datacenter", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	datastoreHeaders = []string{"Name", "Datacenter", "Type", "Capacity MiB", "In Use MiB", "Free MiB", "Free %", "Accessible", "Maintenance mode", "Object ID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	snapshotHeaders  = []string{"VM", "Powerstate", "Name", "Description", "Date / time", "Quiesced", "State", "Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	coverageHeaders  = []string{"Run ID", "Run label", "Run started", "Run finished", "Run status", "Context", "Endpoint", "Datacenter", "vCenter ID", "Sheet", "Collection status", "Item count", "Error"}
)

// sheet is one rendered RVTools tab: a header row plus its data rows, in the
// canonical order shared by every export format. dateCols names the column
// indexes (0-based) that hold a time.Value and should render as dates rather
// than raw numbers where the format distinguishes the two (XLSX only; CSV
// renders every value as text).
type sheet struct {
	name     string
	headers  []string
	rows     [][]any
	dateCols []int
}

// rvtoolsSheets canonicalizes and validates the export data, then returns
// every RVTools tab in tab order. WriteRVTools and RVToolsCSV both build on
// this so the two formats render identical content on identical terms.
func rvtoolsSheets(data assessment.ExportData) ([]sheet, error) {
	data = canonicalData(data)
	if err := validateResources(data.Resources); err != nil {
		return nil, err
	}
	return []sheet{
		{name: "vInfo", headers: vmHeaders, rows: vmRows(data)},
		{name: "vCPU", headers: cpuHeaders, rows: cpuRows(data)},
		{name: "vMemory", headers: memoryHeaders, rows: memoryRows(data)},
		{name: "vDisk", headers: diskHeaders, rows: diskRows(data)},
		{name: "vNetwork", headers: networkHeaders, rows: networkRows(data)},
		{name: "vTools", headers: toolsHeaders, rows: toolsRows(data)},
		{name: "vHost", headers: hostHeaders, rows: hostRows(data)},
		{name: "vCluster", headers: clusterHeaders, rows: clusterRows(data)},
		{name: "vDatastore", headers: datastoreHeaders, rows: datastoreRows(data)},
		{name: "vSnapshot", headers: snapshotHeaders, rows: snapshotRows(data), dateCols: []int{4}},
		{name: "vsfleetCoverage", headers: coverageHeaders, rows: coverageRows(data), dateCols: []int{2, 3}},
	}, nil
}

// WriteRVTools writes the ten persisted RVTools-compatible sheets plus the
// vsfleetCoverage extension sheet. The output is normalized as a ZIP archive
// with fixed entry order and timestamps, making repeated writes byte-identical.
func WriteRVTools(w io.Writer, data assessment.ExportData) error {
	sheets, err := rvtoolsSheets(data)
	if err != nil {
		return err
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetDocProps(&excelize.DocProperties{
		Title:       "vsfleet RVTools assessment export",
		Subject:     "Persisted vsfleet assessment",
		Creator:     "vsfleet",
		Description: "Offline export of a persisted vsfleet assessment",
		Created:     data.Run.StartedAt.UTC().Format(time.RFC3339),
		Modified:    data.Run.StartedAt.UTC().Format(time.RFC3339),
	}); err != nil {
		return err
	}
	styles, err := newStyles(f)
	if err != nil {
		return err
	}
	if err := f.SetSheetName("Sheet1", sheets[0].name); err != nil {
		return err
	}
	for _, s := range sheets[1:] {
		if _, err := f.NewSheet(s.name); err != nil {
			return err
		}
	}
	for _, s := range sheets {
		if err := writeSheet(f, s.name, s.headers, s.rows, s.dateCols, styles); err != nil {
			return err
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return err
	}
	normalized, err := normalizeZip(buf.Bytes())
	if err != nil {
		return err
	}
	n, err := w.Write(normalized)
	if err == nil && n != len(normalized) {
		return io.ErrShortWrite
	}
	return err
}

// CSVFile is one rendered RVTools tab, ready to write to disk as
// "<Name>.csv".
type CSVFile struct {
	Name string
	Data []byte
}

// RVToolsCSV renders every RVTools tab as its own CSV document, in tab
// order, sharing row generation and canonical ordering with WriteRVTools so
// the two formats agree and each is deterministic on the same terms.
//
// Cells favor pipeline consumption over spreadsheet display: timestamps are
// RFC3339 in UTC, booleans are "true"/"false", numbers are unformatted, and
// an absent value is an empty field.
func RVToolsCSV(data assessment.ExportData) ([]CSVFile, error) {
	sheets, err := rvtoolsSheets(data)
	if err != nil {
		return nil, err
	}
	files := make([]CSVFile, 0, len(sheets))
	for _, s := range sheets {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		if err := w.Write(s.headers); err != nil {
			return nil, err
		}
		record := make([]string, len(s.headers))
		for _, row := range s.rows {
			for i, value := range row {
				record[i] = csvCell(value)
			}
			if err := w.Write(record); err != nil {
				return nil, err
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return nil, err
		}
		files = append(files, CSVFile{Name: s.name + ".csv", Data: buf.Bytes()})
	}
	return files, nil
}

// csvCell renders one sheet cell as CSV text. The set of types here matches
// exactly what the row builders below produce.
func csvCell(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case time.Time:
		return v.UTC().Format(time.RFC3339)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

type styles struct {
	header, date int
}

func newStyles(f *excelize.File) (styles, error) {
	date := dateFormat
	header, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E78"}}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	if err != nil {
		return styles{}, err
	}
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &date})
	if err != nil {
		return styles{}, err
	}
	return styles{header: header, date: dateStyle}, nil
}

func writeSheet(f *excelize.File, name string, headers []string, rows [][]any, dateCols []int, s styles) error {
	header := make([]any, len(headers))
	for i, h := range headers {
		header[i] = h
	}
	if err := f.SetSheetRow(name, "A1", &header); err != nil {
		return err
	}
	lastCol, err := excelize.ColumnNumberToName(len(headers))
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(name, "A1", lastCol+"1", s.header); err != nil {
		return err
	}
	for i, row := range rows {
		rowNumber := i + 2
		cell, err := excelize.CoordinatesToCellName(1, rowNumber)
		if err != nil {
			return err
		}
		if err := f.SetSheetRow(name, cell, &row); err != nil {
			return err
		}
		for _, col := range dateCols {
			if col < 0 || col >= len(row) || row[col] == nil {
				continue
			}
			ref, err := excelize.CoordinatesToCellName(col+1, rowNumber)
			if err != nil {
				return err
			}
			if err := f.SetCellStyle(name, ref, ref, s.date); err != nil {
				return err
			}
		}
	}
	lastRow := len(rows) + 1
	if err := f.AutoFilter(name, fmt.Sprintf("A1:%s%d", lastCol, lastRow), nil); err != nil {
		return err
	}
	if err := f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		width := float64(len(headers[i]) + 2)
		if width < 12 {
			width = 12
		}
		if width > 32 {
			width = 32
		}
		if err := f.SetColWidth(name, col, col, width); err != nil {
			return err
		}
	}
	return nil
}

func vmRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0, len(data.VMs))
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		rows = append(rows, []any{vm.Name, vm.PowerState, vm.IsTemplate, vm.GuestState, vm.CPU, vm.MemoryMB, vm.IPAddress, vm.Folder, storageMiB(vm.StorageGB), vm.Annotation, vm.Datacenter, vm.Cluster, vm.Host, vm.GuestOS, vm.ID, vm.BIOSUUID, vm.InstanceUUID, contextEndpoint(data, obs.Context), obs.VCenterID, obs.Context})
	}
	return rows
}

// vmTail returns the identity/location/provenance columns shared by the
// vCPU, vMemory, and vTools tabs (and, inline, by vDisk/vNetwork/vSnapshot).
func vmTail(data assessment.ExportData, obs assessment.Observation) []any {
	vm := obs.VM
	return []any{vm.Annotation, vm.Datacenter, vm.Cluster, vm.Host, vm.Folder, vm.GuestOS, vm.ID, vm.InstanceUUID, contextEndpoint(data, obs.Context), obs.VCenterID, obs.Context}
}

func cpuRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0, len(data.VMs))
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		row := append([]any{vm.Name, vm.PowerState, vm.IsTemplate, vm.CPU}, vmTail(data, obs)...)
		rows = append(rows, row)
	}
	return rows
}

func memoryRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0, len(data.VMs))
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		row := append([]any{vm.Name, vm.PowerState, vm.IsTemplate, vm.MemoryMB}, vmTail(data, obs)...)
		rows = append(rows, row)
	}
	return rows
}

func toolsRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0, len(data.VMs))
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		row := append([]any{vm.Name, vm.PowerState, vm.IsTemplate, vm.ToolsState, optionalString(vm.ToolsVersion), optionalString(vm.ToolsVersionStatus)}, vmTail(data, obs)...)
		rows = append(rows, row)
	}
	return rows
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func diskRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		for _, disk := range vm.Disks {
			rows = append(rows, []any{
				vm.Name, vm.PowerState, vm.IsTemplate, disk.Label, disk.Key, disk.UUID,
				float64(disk.CapacityBytes) / miB, disk.Raw, disk.DiskMode, disk.Sharing,
				optionalBool(disk.ThinProvisioned), optionalBool(disk.EagerlyScrub), optionalBool(disk.Split), optionalBool(disk.WriteThrough),
				disk.SharesLevel, optionalInt32(disk.Shares), optionalInt32(disk.Reservation), optionalInt64(disk.Limit),
				disk.Controller, disk.ControllerLabel, optionalInt32(disk.UnitNumber), disk.SharedBus, disk.BackingPath,
				disk.RawLUNID, disk.RawCompatibilityMode, vm.Annotation, vm.Datacenter, vm.Cluster, vm.Host, vm.Folder,
				vm.GuestOS, vm.ID, vm.InstanceUUID, contextEndpoint(data, obs.Context), obs.VCenterID, obs.Context,
			})
		}
	}
	return rows
}

func networkRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		for _, nic := range vm.NICs {
			rows = append(rows, []any{
				vm.Name, vm.PowerState, vm.IsTemplate, nic.Label, nic.Adapter, nic.Network,
				optionalBool(nic.Connected), optionalBool(nic.StartsConnected), nic.MACAddress, nic.MACAddressType,
				strings.Join(nic.IPv4, ", "), strings.Join(nic.IPv6, ", "), optionalBool(nic.DirectPathIO), vm.Annotation,
				vm.Datacenter, vm.Cluster, vm.Host, vm.Folder, vm.GuestOS, vm.ID, vm.InstanceUUID,
				contextEndpoint(data, obs.Context), obs.VCenterID, obs.Context,
			})
		}
	}
	return rows
}

func optionalBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalInt32(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func hostRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, r := range data.Resources {
		if r.Kind != "host" {
			continue
		}
		var host vsphere.Host
		if err := json.Unmarshal(r.Payload, &host); err != nil {
			continue
		}
		id := nonempty(host.ID, r.ID)
		rows = append(rows, []any{nonempty(host.Name, r.Name), host.Datacenter, host.Cluster, host.InMaintenance, host.CPUMHz, host.CPUCores, hostCPUPercent(host), host.MemoryMB, hostMemoryPercent(host), host.VMCount, host.Version, host.Vendor, host.Model, id, contextEndpoint(data, r.Context), r.VCenterID, r.Context})
	}
	return rows
}

func clusterRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, r := range data.Resources {
		if r.Kind != "cluster" {
			continue
		}
		var cluster vsphere.Cluster
		if err := json.Unmarshal(r.Payload, &cluster); err != nil {
			continue
		}
		rows = append(rows, []any{nonempty(cluster.Name, r.Name), cluster.Hosts, cluster.EffectiveHost, cluster.TotalCPUMHz, cluster.CPUCores, cluster.TotalMemoryMB, cluster.HAEnabled, cluster.DRSEnabled, nonempty(cluster.ID, r.ID), cluster.Datacenter, contextEndpoint(data, r.Context), r.VCenterID, r.Context})
	}
	return rows
}

func datastoreRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, r := range data.Resources {
		if r.Kind != "datastore" {
			continue
		}
		var datastore vsphere.Datastore
		if err := json.Unmarshal(r.Payload, &datastore); err != nil {
			continue
		}
		capacity, free := float64(datastore.CapacityBytes)/miB, float64(datastore.FreeBytes)/miB
		var freePercent any
		if datastore.CapacityBytes > 0 {
			freePercent = float64(datastore.FreeBytes) / float64(datastore.CapacityBytes) * 100
		}
		rows = append(rows, []any{nonempty(datastore.Name, r.Name), datastore.Datacenter, datastore.Type, capacity, float64(datastore.UsedBytes()) / miB, free, freePercent, datastore.Accessible, datastore.Maintenance, nonempty(datastore.ID, r.ID), contextEndpoint(data, r.Context), r.VCenterID, r.Context})
	}
	return rows
}

func snapshotRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		for _, snapshot := range item.Snapshots {
			var created any
			if !snapshot.CreateTime.IsZero() {
				created = snapshot.CreateTime.UTC()
			}
			rows = append(rows, []any{vm.Name, vm.PowerState, snapshot.Name, snapshot.Description, created, snapshot.Quiesced, snapshot.PowerState, vm.Annotation, vm.Datacenter, vm.Cluster, vm.Host, vm.Folder, vm.GuestOS, vm.ID, vm.InstanceUUID, contextEndpoint(data, obs.Context), obs.VCenterID, obs.Context})
		}
	}
	return rows
}

func coverageRows(data assessment.ExportData) [][]any {
	counts := make(map[string]int)
	diskCounts := make(map[string]int)
	networkCounts := make(map[string]int)
	for _, item := range data.VMs {
		counts[item.Observation.Context]++
		diskCounts[item.Observation.Context] += len(item.Observation.VM.Disks)
		networkCounts[item.Observation.Context] += len(item.Observation.VM.NICs)
	}
	snapshotCounts := make(map[string]int)
	for _, item := range data.VMs {
		snapshotCounts[item.Observation.Context] += len(item.Snapshots)
	}
	resources := make(map[string]map[string]int)
	for _, r := range data.Resources {
		if resources[r.Context] == nil {
			resources[r.Context] = make(map[string]int)
		}
		resources[r.Context][r.Kind]++
	}
	rows := make([][]any, 0, len(data.Contexts)*10)
	devicesRecorded := inventoryAtLeast(data.Run.InventorySchemaVersion, 2)
	toolsRecorded := inventoryAtLeast(data.Run.InventorySchemaVersion, 3)
	if !devicesRecorded {
		diskCounts = make(map[string]int)
		networkCounts = make(map[string]int)
	}
	for _, c := range data.Contexts {
		collections := make(map[string]assessment.CollectionRun)
		for _, collection := range c.Collections {
			collections[collection.Kind] = collection
		}
		for _, spec := range []struct {
			kind, sheet string
			count       int
		}{
			{kind: "vm", sheet: "vInfo", count: counts[c.Name]},
			{kind: "vcpu", sheet: "vCPU", count: counts[c.Name]},
			{kind: "vmemory", sheet: "vMemory", count: counts[c.Name]},
			{kind: "vdisk", sheet: "vDisk", count: diskCounts[c.Name]},
			{kind: "vnetwork", sheet: "vNetwork", count: networkCounts[c.Name]},
			{kind: "vtools", sheet: "vTools", count: counts[c.Name]},
			{kind: "host", sheet: "vHost", count: resources[c.Name]["host"]},
			{kind: "cluster", sheet: "vCluster", count: resources[c.Name]["cluster"]},
			{kind: "datastore", sheet: "vDatastore", count: resources[c.Name]["datastore"]},
			{kind: "snapshot", sheet: "vSnapshot", count: snapshotCounts[c.Name]},
		} {
			status, message := "not recorded", ""
			switch {
			case (spec.kind == "vdisk" || spec.kind == "vnetwork") && !devicesRecorded:
				status = "not recorded"
				message = "capture predates per-VM device inventory"
			// Disks and NICs ride along with the VM capture rather than
			// being their own collection pass, so their coverage mirrors the
			// VM collection's status.
			case spec.kind == "vm" || spec.kind == "snapshot" || spec.kind == "vcpu" || spec.kind == "vmemory" || spec.kind == "vtools" || spec.kind == "vdisk" || spec.kind == "vnetwork":
				status = c.VMStatus
				if status == "" {
					status = "not recorded"
				}
				if status != "success" && status != "empty" {
					message = c.Error
				} else if spec.kind == "vtools" && !toolsRecorded {
					// The vTools tab still has one row per VM here (the
					// running-status column predates schema 3), just without
					// a version — say so rather than claiming the tab is
					// unrecorded.
					message = "capture predates VMware Tools version inventory"
				}
			default:
				if collection, ok := collections[spec.kind]; ok {
					status, message = collection.Status, collection.Error
				}
			}
			rows = append(rows, coverageRow(data, c, spec.sheet, status, spec.count, message))
		}
	}
	return rows
}

func inventoryAtLeast(version string, minVersion int) bool {
	value, err := strconv.Atoi(strings.TrimSpace(version))
	return err == nil && value >= minVersion
}

func coverageRow(data assessment.ExportData, c assessment.ContextRun, sheet, status string, count int, message string) []any {
	var finished any
	if !c.FinishedAt.IsZero() {
		finished = c.FinishedAt.UTC()
	}
	return []any{data.Run.ID, data.Run.Label, data.Run.StartedAt.UTC(), finished, string(data.Run.Status), c.Name, c.Endpoint, c.Datacenter, c.VCenterID, sheet, status, count, message}
}

func canonicalData(data assessment.ExportData) assessment.ExportData {
	data.Contexts = append([]assessment.ContextRun(nil), data.Contexts...)
	data.VMs = append([]assessment.ExportVM(nil), data.VMs...)
	data.Resources = append([]assessment.ResourceObservation(nil), data.Resources...)
	sort.SliceStable(data.Contexts, func(i, j int) bool { return data.Contexts[i].Name < data.Contexts[j].Name })
	sort.SliceStable(data.VMs, func(i, j int) bool {
		a, b := data.VMs[i].Observation, data.VMs[j].Observation
		return less(a.Context, a.VM.Datacenter, a.VM.Name, a.VM.ID, b.Context, b.VM.Datacenter, b.VM.Name, b.VM.ID)
	})
	for i := range data.VMs {
		data.VMs[i].Snapshots = append([]vsphere.VMSnapshot(nil), data.VMs[i].Snapshots...)
		data.VMs[i].Observation.VM.Disks = append([]vsphere.VMDisk(nil), data.VMs[i].Observation.VM.Disks...)
		data.VMs[i].Observation.VM.NICs = append([]vsphere.VMNIC(nil), data.VMs[i].Observation.VM.NICs...)
		for n := range data.VMs[i].Observation.VM.NICs {
			data.VMs[i].Observation.VM.NICs[n].IPv4 = append([]string(nil), data.VMs[i].Observation.VM.NICs[n].IPv4...)
			data.VMs[i].Observation.VM.NICs[n].IPv6 = append([]string(nil), data.VMs[i].Observation.VM.NICs[n].IPv6...)
			sort.Strings(data.VMs[i].Observation.VM.NICs[n].IPv4)
			sort.Strings(data.VMs[i].Observation.VM.NICs[n].IPv6)
		}
		sort.SliceStable(data.VMs[i].Observation.VM.Disks, func(a, b int) bool {
			return data.VMs[i].Observation.VM.Disks[a].Key < data.VMs[i].Observation.VM.Disks[b].Key
		})
		sort.SliceStable(data.VMs[i].Observation.VM.NICs, func(a, b int) bool {
			return data.VMs[i].Observation.VM.NICs[a].Key < data.VMs[i].Observation.VM.NICs[b].Key
		})
		sort.SliceStable(data.VMs[i].Snapshots, func(a, b int) bool {
			x, y := data.VMs[i].Snapshots[a], data.VMs[i].Snapshots[b]
			if !x.CreateTime.Equal(y.CreateTime) {
				return x.CreateTime.Before(y.CreateTime)
			}
			return x.ID < y.ID
		})
	}
	sort.SliceStable(data.Resources, func(i, j int) bool {
		a, b := data.Resources[i], data.Resources[j]
		return less(a.Context, resourceDC(a), a.Name, a.ID, b.Context, resourceDC(b), b.Name, b.ID)
	})
	return data
}

func less(ac, ad, an, ai, bc, bd, bn, bi string) bool {
	for _, pair := range [][2]string{{ac, bc}, {ad, bd}, {strings.ToLower(an), strings.ToLower(bn)}, {ai, bi}} {
		if pair[0] != pair[1] {
			return pair[0] < pair[1]
		}
	}
	return false
}

func resourceDC(r assessment.ResourceObservation) string {
	var loc struct {
		Datacenter string `json:"datacenter"`
	}
	_ = json.Unmarshal(r.Payload, &loc)
	return loc.Datacenter
}

func contextEndpoint(data assessment.ExportData, name string) string {
	for _, c := range data.Contexts {
		if c.Name == name {
			return c.Endpoint
		}
	}
	return ""
}

func storageMiB(gb float64) any {
	return gb * 1024
}

func hostCPUPercent(h vsphere.Host) any {
	denom := float64(h.CPUCores) * float64(h.CPUMHz)
	if denom <= 0 {
		return nil
	}
	return float64(h.CPUUsageMHz) / denom * 100
}

func hostMemoryPercent(h vsphere.Host) any {
	if h.MemoryMB <= 0 {
		return nil
	}
	return float64(h.MemoryUsageMB) / float64(h.MemoryMB) * 100
}

func nonempty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func validateResources(resources []assessment.ResourceObservation) error {
	for _, resource := range resources {
		var value any
		switch resource.Kind {
		case "host":
			value = &vsphere.Host{}
		case "cluster":
			value = &vsphere.Cluster{}
		case "datastore":
			value = &vsphere.Datastore{}
		default:
			return fmt.Errorf("unsupported persisted resource kind %q", resource.Kind)
		}
		if err := json.Unmarshal(resource.Payload, value); err != nil {
			return fmt.Errorf("malformed persisted %s %q payload: %w", resource.Kind, resource.ID, err)
		}
	}
	return nil
}

func normalizeZip(input []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		return nil, err
	}
	files := append([]*zip.File(nil), r.File...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	epoch := time.Unix(0, 0).UTC()
	for _, file := range files {
		src, err := file.Open()
		if err != nil {
			_ = w.Close()
			return nil, err
		}
		contents, readErr := io.ReadAll(src)
		closeErr := src.Close()
		if readErr != nil {
			_ = w.Close()
			return nil, readErr
		}
		if closeErr != nil {
			_ = w.Close()
			return nil, closeErr
		}
		header := &zip.FileHeader{Name: file.Name, Method: zip.Deflate, Modified: epoch}
		writer, err := w.CreateHeader(header)
		if err != nil {
			_ = w.Close()
			return nil, err
		}
		if _, err := writer.Write(contents); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
