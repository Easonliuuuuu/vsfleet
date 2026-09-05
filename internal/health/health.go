// Package health evaluates read-only health rules over persisted assessment
// evidence. It deliberately has no connection to configuration, credentials,
// sessions, or the network.
package health

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// ReportSchemaVersion versions the machine-readable finding envelope.
const ReportSchemaVersion = 1

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Object is what a finding is about. Kind is vm, host, or datastore.
type Object struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ID         string `json:"id"`
	Context    string `json:"context"`
	VCenterID  string `json:"vcenter_id"`
	Datacenter string `json:"datacenter"`
}

type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Object   Object   `json:"object"`
	Message  string   `json:"message"`
}

// Input is the evidence made available to a rule. Rules receive a value
// rather than the assessment store, keeping evaluation offline and testable.
type Input struct {
	Data       assessment.ExportData
	Thresholds Thresholds
}

// Rule is one check. MinSchema is the inventory schema version its evidence
// first appeared in; zero means any capture can answer it.
type Rule struct {
	ID        string
	Severity  Severity
	Summary   string
	MinSchema int
	Needs     string
	Eval      func(in Input, emit func(Finding))
}

type Thresholds struct {
	SnapshotAge      time.Duration `json:"max_snapshot_age"`
	DatastoreFreePct float64       `json:"min_datastore_free_pct"`
	GuestDiskFreePct float64       `json:"min_guest_disk_free_pct"`
}

// DefaultThresholds are the policy defaults used by the CLI, exports, and
// the History health pane.
func DefaultThresholds() Thresholds {
	return Thresholds{
		SnapshotAge:      30 * 24 * time.Hour,
		DatastoreFreePct: 10,
		GuestDiskFreePct: 10,
	}
}

type Options struct {
	Thresholds Thresholds
	Disabled   []string
}

// RuleStatus is why a rule did or did not contribute. This keeps an empty
// vHealth tab from being mistaken for a healthy estate when evidence was not
// collected or a rule was deliberately disabled.
type RuleStatus struct {
	Rule     string `json:"rule"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Findings int    `json:"findings"`
}

type Counts struct {
	Info     int `json:"info"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
	Total    int `json:"total"`
}

type Report struct {
	SchemaVersion int          `json:"schema_version"`
	RunID         int64        `json:"run_id"`
	Thresholds    Thresholds   `json:"thresholds"`
	Rules         []RuleStatus `json:"rules"`
	Findings      []Finding    `json:"findings"`
	Counts        Counts       `json:"counts"`
}

// Evaluate is the whole health API. It is a pure function of data and opts;
// in particular it never reads wall clock time.
func Evaluate(data assessment.ExportData, opts Options) Report {
	data = canonicalData(data)
	report := Report{
		SchemaVersion: ReportSchemaVersion,
		RunID:         data.Run.ID,
		Thresholds:    opts.Thresholds,
		Rules:         make([]RuleStatus, 0, len(rules)),
		Findings:      make([]Finding, 0),
	}
	disabled := make(map[string]bool, len(opts.Disabled))
	for _, id := range opts.Disabled {
		disabled[strings.TrimSpace(id)] = true
	}
	schema := inventorySchema(data.Run.InventorySchemaVersion)
	in := Input{Data: data, Thresholds: opts.Thresholds}
	for _, rule := range rules {
		status := RuleStatus{Rule: rule.ID}
		switch {
		case disabled[rule.ID]:
			status.Status = "disabled"
			status.Reason = "disabled by option"
		case rule.MinSchema > 0 && schema < rule.MinSchema:
			status.Status = "not-evaluated"
			status.Reason = rule.Needs
		default:
			status.Status = "evaluated"
			before := len(report.Findings)
			rule.Eval(in, func(f Finding) {
				if f.Rule == "" {
					f.Rule = rule.ID
				}
				if f.Severity == "" {
					f.Severity = rule.Severity
				}
				report.Findings = append(report.Findings, f)
			})
			status.Findings = len(report.Findings) - before
		}
		report.Rules = append(report.Rules, status)
	}
	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		for _, pair := range [][2]string{
			{a.Rule, b.Rule},
			{a.Object.Context, b.Object.Context},
			{a.Object.Kind, b.Object.Kind},
			{strings.ToLower(a.Object.Name), strings.ToLower(b.Object.Name)},
			{a.Object.ID, b.Object.ID},
			{a.Message, b.Message},
		} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
	for _, finding := range report.Findings {
		switch finding.Severity {
		case SeverityInfo:
			report.Counts.Info++
		case SeverityWarning:
			report.Counts.Warning++
		case SeverityCritical:
			report.Counts.Critical++
		}
	}
	report.Counts.Total = len(report.Findings)
	return report
}

// Rules returns a copy of the registry in stable ID order.
func Rules() []Rule {
	return append([]Rule(nil), rules...)
}

func inventorySchema(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

func canonicalData(data assessment.ExportData) assessment.ExportData {
	data.Contexts = append([]assessment.ContextRun(nil), data.Contexts...)
	data.VMs = append([]assessment.ExportVM(nil), data.VMs...)
	data.Resources = append([]assessment.ResourceObservation(nil), data.Resources...)
	sort.SliceStable(data.Contexts, func(i, j int) bool {
		return data.Contexts[i].Name < data.Contexts[j].Name
	})
	sort.SliceStable(data.VMs, func(i, j int) bool {
		a, b := data.VMs[i].Observation, data.VMs[j].Observation
		return less(a.Context, a.VM.Datacenter, a.VM.Name, a.VM.ID, b.Context, b.VM.Datacenter, b.VM.Name, b.VM.ID)
	})
	for i := range data.VMs {
		data.VMs[i].Snapshots = append([]vsphere.VMSnapshot(nil), data.VMs[i].Snapshots...)
		if len(data.VMs[i].Snapshots) == 0 {
			data.VMs[i].Snapshots = append([]vsphere.VMSnapshot(nil), data.VMs[i].Observation.VM.Snapshots...)
		}
		data.VMs[i].Observation.VM.Partitions = append([]vsphere.VMPartition(nil), data.VMs[i].Observation.VM.Partitions...)
		sort.SliceStable(data.VMs[i].Observation.VM.Partitions, func(a, b int) bool {
			x, y := data.VMs[i].Observation.VM.Partitions[a], data.VMs[i].Observation.VM.Partitions[b]
			if !strings.EqualFold(x.Path, y.Path) {
				return strings.ToLower(x.Path) < strings.ToLower(y.Path)
			}
			return x.Path < y.Path
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

func contextName(obs assessment.Observation) string {
	if obs.Context != "" {
		return obs.Context
	}
	return obs.VM.Context
}

func contextFinish(data assessment.ExportData, name string) time.Time {
	for _, contextRun := range data.Contexts {
		if contextRun.Name == name && !contextRun.FinishedAt.IsZero() {
			return contextRun.FinishedAt
		}
	}
	return data.Run.FinishedAt
}

func vmObject(data assessment.ExportData, obs assessment.Observation) Object {
	vm := obs.VM
	datacenter := vm.Datacenter
	if datacenter == "" {
		for _, contextRun := range data.Contexts {
			if contextRun.Name == contextName(obs) {
				datacenter = contextRun.Datacenter
				break
			}
		}
	}
	return Object{Kind: "vm", Name: vm.Name, ID: vm.ID, Context: contextName(obs), VCenterID: obs.VCenterID, Datacenter: datacenter}
}

func resourceObject(data assessment.ExportData, r assessment.ResourceObservation, kind, name, id, datacenter string) Object {
	if name == "" {
		name = r.Name
	}
	if id == "" {
		id = r.ID
	}
	if datacenter == "" {
		datacenter = resourceDC(r)
	}
	if datacenter == "" {
		for _, contextRun := range data.Contexts {
			if contextRun.Name == r.Context {
				datacenter = contextRun.Datacenter
				break
			}
		}
	}
	return Object{Kind: kind, Name: name, ID: id, Context: r.Context, VCenterID: r.VCenterID, Datacenter: datacenter}
}

func decodeResource(r assessment.ResourceObservation, target any) bool {
	return json.Unmarshal(r.Payload, target) == nil
}

func freePct(capacity, free int64) (float64, bool) {
	if capacity <= 0 {
		return 0, false
	}
	return float64(free) / float64(capacity) * 100, true
}

func percent(value float64) string {
	s := strconv.FormatFloat(value, 'f', 1, 64)
	return strings.TrimSuffix(strings.TrimSuffix(s, "0"), ".")
}

func ageText(age time.Duration) string {
	if age >= 24*time.Hour {
		return fmt.Sprintf("%d days", int(age/(24*time.Hour)))
	}
	if age >= time.Hour {
		return fmt.Sprintf("%d hours", int(age/time.Hour))
	}
	if age >= time.Minute {
		return fmt.Sprintf("%d minutes", int(age/time.Minute))
	}
	return age.Round(time.Second).String()
}
