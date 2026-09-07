// Package decommission evaluates whether a stored VM observation has enough
// evidence for a safe decommissioning review. It never connects to vCenter or
// performs a lifecycle operation.
package decommission

import (
	"fmt"
	"sort"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/topology"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const SchemaVersion = 1

const (
	VerdictReady   = "ready"
	VerdictBlocked = "blocked"
	VerdictUnknown = "unknown"

	StatusPass        = "pass"
	StatusFail        = "fail"
	StatusUnknown     = "unknown"
	StatusNotAssessed = "not_assessed"

	ImpactBlocker  = "blocker"
	ImpactUnknown  = "unknown"
	ImpactAdvisory = "advisory"
)

// Evidence is one measured fact behind a decommission check.
type Evidence struct {
	Field    string `json:"field"`
	Observed string `json:"observed"`
	Expected string `json:"expected,omitempty"`
}

// Check is a stable, machine-readable assessment item. Status describes what
// the stored evidence says; Impact describes how it affects the verdict.
type Check struct {
	ID       string     `json:"id"`
	Status   string     `json:"status"`
	Impact   string     `json:"impact,omitempty"`
	Message  string     `json:"message"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// SubjectReport contains one distinct VM identity. A name collision produces
// multiple subjects instead of silently choosing one.
type SubjectReport struct {
	Subject      topology.Subject     `json:"subject"`
	Verdict      string               `json:"verdict"`
	Checks       []Check              `json:"checks"`
	Dependencies []topology.Edge      `json:"dependencies,omitempty"`
	Unresolved   []string             `json:"unresolved,omitempty"`
	Blind        []topology.Blindness `json:"blind,omitempty"`
}

// Report is the complete result for one stored run and query.
type Report struct {
	SchemaVersion   int                  `json:"schema_version"`
	RunID           int64                `json:"run_id"`
	Query           string               `json:"query"`
	Verdict         string               `json:"verdict"`
	Ambiguous       bool                 `json:"ambiguous,omitempty"`
	CheckedContexts []string             `json:"checked_contexts,omitempty"`
	Blind           []topology.Blindness `json:"blind,omitempty"`
	Subjects        []SubjectReport      `json:"subjects"`
}

// Evaluate applies the strict safety policy to an immutable assessment. The
// context slice is an optional filter; an empty slice evaluates every context
// present in the selected run.
func Evaluate(data assessment.ExportData, query string, contexts []string) Report {
	query = strings.TrimSpace(query)
	data = scopeData(data, contexts)
	graph := topology.Build(data)
	subjects := graph.Resolve(topology.KindVM, query, contexts)
	out := Report{SchemaVersion: SchemaVersion, RunID: data.Run.ID, Query: query, Verdict: VerdictUnknown, Subjects: make([]SubjectReport, 0, len(subjects))}
	if len(subjects) == 0 {
		unknown := topology.Subject{Kind: string(topology.KindVM), Name: query, Basis: topology.BasisName}
		out.Subjects = append(out.Subjects, SubjectReport{
			Subject: unknown,
			Verdict: VerdictUnknown,
			Checks:  []Check{{ID: "identity", Status: StatusUnknown, Impact: ImpactUnknown, Message: fmt.Sprintf("no stored VM matched %q", query)}},
		})
		for _, context := range data.Contexts {
			out.CheckedContexts = append(out.CheckedContexts, context.Name)
		}
		for _, context := range assessment.BlindContexts(data.Contexts, []string{"vm"}) {
			out.Blind = append(out.Blind, topology.Blindness{Context: context, Reason: assessment.CoverageReason(data.Contexts, context, []string{"vm"})})
		}
		sort.Strings(out.CheckedContexts)
		sort.SliceStable(out.Blind, func(i, j int) bool {
			if out.Blind[i].Context != out.Blind[j].Context {
				return out.Blind[i].Context < out.Blind[j].Context
			}
			return out.Blind[i].Reason < out.Blind[j].Reason
		})
		return out
	}
	out.Ambiguous = len(subjects) > 1
	out.Verdict = VerdictReady
	for _, subject := range subjects {
		dependencies := graph.Dependencies(subject, 1)
		observations := observationsFor(data, subject)
		report := evaluateSubject(data, subject, dependencies, observations)
		out.Subjects = append(out.Subjects, report)
		out.CheckedContexts = appendUnique(out.CheckedContexts, subjectContexts(subject)...)
		out.Blind = appendUniqueBlind(out.Blind, report.Blind...)
	}
	sort.Strings(out.CheckedContexts)
	sort.SliceStable(out.Blind, func(i, j int) bool {
		if out.Blind[i].Context != out.Blind[j].Context {
			return out.Blind[i].Context < out.Blind[j].Context
		}
		return out.Blind[i].Reason < out.Blind[j].Reason
	})
	for _, subject := range out.Subjects {
		if subject.Verdict == VerdictBlocked {
			out.Verdict = VerdictBlocked
			return out
		}
		if subject.Verdict == VerdictUnknown {
			out.Verdict = VerdictUnknown
		}
	}
	if out.Ambiguous && out.Verdict == VerdictReady {
		out.Verdict = VerdictUnknown
	}
	return out
}

func scopeData(data assessment.ExportData, contexts []string) assessment.ExportData {
	if len(contexts) == 0 {
		return data
	}
	allowed := make(map[string]bool, len(contexts))
	for _, context := range contexts {
		allowed[strings.ToLower(strings.TrimSpace(context))] = true
	}
	scoped := data
	scoped.Contexts = make([]assessment.ContextRun, 0, len(data.Contexts))
	for _, context := range data.Contexts {
		if allowed[strings.ToLower(context.Name)] {
			scoped.Contexts = append(scoped.Contexts, context)
		}
	}
	scoped.VMs = make([]assessment.ExportVM, 0, len(data.VMs))
	for _, item := range data.VMs {
		if allowed[strings.ToLower(observationContext(item.Observation))] {
			scoped.VMs = append(scoped.VMs, item)
		}
	}
	scoped.Resources = make([]assessment.ResourceObservation, 0, len(data.Resources))
	for _, resource := range data.Resources {
		if allowed[strings.ToLower(resource.Context)] {
			scoped.Resources = append(scoped.Resources, resource)
		}
	}
	return scoped
}

func evaluateSubject(data assessment.ExportData, subject topology.Subject, dependencies topology.SubjectResult, observations []assessment.ExportVM) SubjectReport {
	out := SubjectReport{
		Subject:      subject,
		Verdict:      VerdictReady,
		Checks:       make([]Check, 0, 10),
		Dependencies: append([]topology.Edge(nil), dependencies.Edges...),
		Unresolved:   append([]string(nil), dependencies.Unresolved...),
		Blind:        append([]topology.Blindness(nil), dependencies.Blind...),
	}

	out.Checks = append(out.Checks,
		powerStateCheck(observations),
		connectionStateCheck(observations),
		snapshotCheck(observations),
		mediaCheck(data, observations),
		diskReferenceCheck(observations),
		dependencyCheck("datastore-dependencies", "datastore", observations, dependencies),
		dependencyCheck("network-relationships", "network", observations, dependencies),
		Check{ID: "ownership", Status: StatusNotAssessed, Impact: ImpactAdvisory, Message: "ownership metadata is not collected by this assessment"},
		Check{ID: "backup-policy", Status: StatusNotAssessed, Impact: ImpactAdvisory, Message: "backup-policy metadata is not collected by this assessment"},
	)
	if len(observations) == 0 {
		out.Checks = append(out.Checks, Check{ID: "coverage", Status: StatusUnknown, Impact: ImpactUnknown, Message: "matched VM identity has no observation in the selected run"})
	} else if len(dependencies.Blind) > 0 {
		out.Checks = append(out.Checks, Check{ID: "coverage", Status: StatusUnknown, Impact: ImpactUnknown, Message: "relationship coverage is incomplete"})
	} else if coverage := coverageCheck(data, observations); coverage.Status != StatusPass {
		out.Checks = append(out.Checks, coverage)
	}
	for _, check := range out.Checks {
		switch check.Status {
		case StatusFail:
			if check.Impact == ImpactBlocker {
				out.Verdict = VerdictBlocked
			}
		case StatusUnknown:
			if out.Verdict != VerdictBlocked {
				out.Verdict = VerdictUnknown
			}
		}
	}
	return out
}

func powerStateCheck(observations []assessment.ExportVM) Check {
	check := Check{ID: "power-state", Impact: ImpactBlocker}
	if len(observations) == 0 {
		return unknownCheck(check, "power-state evidence is unavailable")
	}
	for _, item := range observations {
		state := strings.TrimSpace(item.Observation.VM.PowerState)
		switch state {
		case "poweredOff":
			continue
		case "poweredOn", "suspended":
			return Check{ID: check.ID, Status: StatusFail, Impact: ImpactBlocker, Message: fmt.Sprintf("VM is %s", state), Evidence: []Evidence{{Field: "power_state", Observed: state, Expected: "poweredOff"}}}
		default:
			return unknownCheck(check, "VM power state is missing")
		}
	}
	return Check{ID: check.ID, Status: StatusPass, Impact: ImpactBlocker, Message: "all matched observations are powered off"}
}

func connectionStateCheck(observations []assessment.ExportVM) Check {
	check := Check{ID: "connection-state", Impact: ImpactBlocker}
	if len(observations) == 0 {
		return unknownCheck(check, "VM connection state evidence is unavailable")
	}
	for _, item := range observations {
		state := strings.TrimSpace(item.Observation.VM.ConnectionState)
		if state == "connected" {
			continue
		}
		if state == "" {
			return unknownCheck(check, "VM connection state is missing")
		}
		return Check{ID: check.ID, Status: StatusFail, Impact: ImpactBlocker, Message: fmt.Sprintf("VM connection state is %s", state), Evidence: []Evidence{{Field: "connection_state", Observed: state, Expected: "connected"}}}
	}
	return Check{ID: check.ID, Status: StatusPass, Impact: ImpactBlocker, Message: "all matched observations are connected"}
}

func snapshotCheck(observations []assessment.ExportVM) Check {
	check := Check{ID: "snapshots", Impact: ImpactBlocker}
	if len(observations) == 0 {
		return unknownCheck(check, "snapshot evidence is unavailable")
	}
	for _, item := range observations {
		snapshots := item.Snapshots
		if len(snapshots) == 0 {
			snapshots = item.Observation.VM.Snapshots
		}
		if len(snapshots) == 0 {
			continue
		}
		return Check{ID: check.ID, Status: StatusFail, Impact: ImpactBlocker, Message: fmt.Sprintf("VM has %d snapshot(s)", len(snapshots)), Evidence: []Evidence{{Field: "snapshot_count", Observed: fmt.Sprint(len(snapshots)), Expected: "0"}}}
	}
	return Check{ID: check.ID, Status: StatusPass, Impact: ImpactBlocker, Message: "no snapshots are recorded"}
}

func mediaCheck(data assessment.ExportData, observations []assessment.ExportVM) Check {
	check := Check{ID: "mounted-media", Impact: ImpactBlocker}
	if len(observations) == 0 {
		return unknownCheck(check, "CD-ROM and USB evidence is unavailable")
	}
	if inventorySchema(data.Run.InventorySchemaVersion) < 6 {
		return unknownCheck(check, "CD-ROM and USB evidence was not collected by this run")
	}
	for _, item := range observations {
		vm := item.Observation.VM
		if !vm.ConfigurationAvailable {
			return unknownCheck(check, "VM hardware configuration is unavailable")
		}
		for _, cdrom := range vm.CDROMs {
			if cdrom.Connected != nil && *cdrom.Connected {
				return Check{ID: check.ID, Status: StatusFail, Impact: ImpactBlocker, Message: fmt.Sprintf("CD-ROM %q is connected", nonempty(cdrom.Label, "unnamed CD-ROM")), Evidence: []Evidence{{Field: "device", Observed: nonempty(cdrom.Label, "unnamed CD-ROM")}, {Field: "connected", Observed: "true", Expected: "false"}}}
			}
			if cdrom.Connected == nil {
				return unknownCheck(check, "CD-ROM connection state is missing")
			}
		}
		for _, usb := range vm.USBs {
			if usb.Connected != nil && *usb.Connected {
				return Check{ID: check.ID, Status: StatusFail, Impact: ImpactBlocker, Message: fmt.Sprintf("USB device %q is connected", nonempty(usb.Label, "unnamed USB device")), Evidence: []Evidence{{Field: "device", Observed: nonempty(usb.Label, "unnamed USB device")}, {Field: "connected", Observed: "true", Expected: "false"}}}
			}
			if usb.Connected == nil {
				return unknownCheck(check, "USB connection state is missing")
			}
		}
	}
	return Check{ID: check.ID, Status: StatusPass, Impact: ImpactBlocker, Message: "no connected CD-ROM or USB devices are recorded"}
}

func diskReferenceCheck(observations []assessment.ExportVM) Check {
	check := Check{ID: "disk-references", Impact: ImpactUnknown}
	if len(observations) == 0 {
		return unknownCheck(check, "disk evidence is unavailable")
	}
	for _, item := range observations {
		if !item.Observation.VM.ConfigurationAvailable {
			return unknownCheck(check, "VM disk configuration is unavailable")
		}
		for _, disk := range item.Observation.VM.Disks {
			if disk.BackingPath == "" {
				return unknownCheck(check, fmt.Sprintf("disk %q has no backing path", nonempty(disk.Label, "unnamed disk")))
			}
			if _, relative, valid := vsphere.SplitDatastorePath(disk.BackingPath); !valid || relative == "" {
				return unknownCheck(check, fmt.Sprintf("disk %q has an unresolved backing path", nonempty(disk.Label, "unnamed disk")))
			}
		}
	}
	return Check{ID: check.ID, Status: StatusPass, Impact: ImpactUnknown, Message: "all recorded virtual disks have datastore backing paths"}
}

func dependencyCheck(id, kind string, observations []assessment.ExportVM, dependencies topology.SubjectResult) Check {
	check := Check{ID: id, Impact: ImpactUnknown}
	if len(observations) == 0 {
		return unknownCheck(check, "dependency evidence is unavailable")
	}
	needed := false
	for _, item := range observations {
		vm := item.Observation.VM
		if kind == "datastore" {
			needed = needed || len(vm.Datastores) > 0 || len(vm.Disks) > 0
		} else {
			needed = needed || len(vm.NICs) > 0
		}
	}
	if !needed {
		return Check{ID: id, Status: StatusPass, Impact: ImpactUnknown, Message: fmt.Sprintf("no %s dependencies are recorded", kind)}
	}
	matched := false
	for _, edge := range dependencies.Edges {
		if strings.EqualFold(edge.To.Kind, kind) && edge.Confidence != topology.EdgeUnresolved {
			matched = true
			break
		}
	}
	if len(dependencies.Unresolved) > 0 || len(dependencies.Blind) > 0 || !matched {
		return unknownCheck(check, fmt.Sprintf("%s dependency relationships are incomplete", kind))
	}
	return Check{ID: id, Status: StatusPass, Impact: ImpactUnknown, Message: fmt.Sprintf("%s dependencies are resolved", kind)}
}

func coverageCheck(data assessment.ExportData, observations []assessment.ExportVM) Check {
	for _, item := range observations {
		contextName := observationContext(item.Observation)
		for _, context := range data.Contexts {
			if context.Name != contextName {
				continue
			}
			if !assessment.Successful(context.VMStatus) {
				return Check{ID: "coverage", Status: StatusUnknown, Impact: ImpactUnknown, Message: fmt.Sprintf("VM collection for context %q is %s", contextName, nonempty(context.VMStatus, "not recorded"))}
			}
		}
	}
	return Check{ID: "coverage", Status: StatusPass, Impact: ImpactUnknown, Message: "VM collection completed for every matched context"}
}

func observationsFor(data assessment.ExportData, subject topology.Subject) []assessment.ExportVM {
	var out []assessment.ExportVM
	for _, item := range data.VMs {
		if item.Observation.VM.IsTemplate {
			continue
		}
		contextName := observationContext(item.Observation)
		for _, member := range subject.Members {
			if !strings.EqualFold(contextName, member.Context) {
				continue
			}
			matches := false
			if member.ID != "" {
				matches = strings.EqualFold(member.ID, item.Observation.VM.ID)
			} else {
				matches = strings.EqualFold(member.Name, item.Observation.VM.Name)
			}
			if matches {
				out = append(out, item)
				break
			}
		}
	}
	return out
}

func subjectContexts(subject topology.Subject) []string {
	contexts := make([]string, 0, len(subject.Members))
	for _, member := range subject.Members {
		if member.Context != "" {
			contexts = appendUnique(contexts, member.Context)
		}
	}
	return contexts
}

func observationContext(observation assessment.Observation) string {
	if observation.Context != "" {
		return observation.Context
	}
	return observation.VM.Context
}

func unknownCheck(check Check, message string) Check {
	check.Status = StatusUnknown
	check.Impact = ImpactUnknown
	check.Message = message
	return check
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if strings.EqualFold(value, addition) {
				found = true
				break
			}
		}
		if !found && addition != "" {
			values = append(values, addition)
		}
	}
	return values
}

func appendUniqueBlind(values []topology.Blindness, additions ...topology.Blindness) []topology.Blindness {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if value.Context == addition.Context && value.Reason == addition.Reason {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}

func inventorySchema(value string) int {
	var schema int
	_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &schema)
	return schema
}

func nonempty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
