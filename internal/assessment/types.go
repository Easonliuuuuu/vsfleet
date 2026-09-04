// Package assessment stores point-in-time VM observations and explains the
// changes between two observations. It is deliberately independent of the
// live TUI cache: a historical record is immutable evidence, not stale UI.
package assessment

import (
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

type RunStatus string

const (
	RunRunning  RunStatus = "running"
	RunComplete RunStatus = "complete"
	RunPartial  RunStatus = "partial"
	RunFailed   RunStatus = "failed"
)

type Run struct {
	ID                     int64     `json:"id"`
	Source                 string    `json:"source"`
	Label                  string    `json:"label,omitempty"`
	Note                   string    `json:"note,omitempty"`
	Pinned                 bool      `json:"pinned,omitempty"`
	ToolVersion            string    `json:"tool_version,omitempty"`
	InventorySchemaVersion string    `json:"inventory_schema_version,omitempty"`
	StartedAt              time.Time `json:"started_at"`
	FinishedAt             time.Time `json:"finished_at,omitempty"`
	Status                 RunStatus `json:"status"`
	RequestedContexts      int       `json:"requested_contexts"`
	SuccessfulContexts     int       `json:"successful_contexts"`
}

type ContextRun struct {
	RunID      int64     `json:"run_id"`
	Name       string    `json:"context"`
	Endpoint   string    `json:"endpoint"`
	Datacenter string    `json:"datacenter,omitempty"`
	VCenterID  string    `json:"vcenter_id,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	VMStatus   string    `json:"vm_status"`
	Error      string    `json:"error,omitempty"`
}

type Observation struct {
	VCenterID string     `json:"vcenter_id"`
	Context   string     `json:"context"`
	VM        vsphere.VM `json:"vm"`
}

type FieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type VMChange struct {
	Kind       string        `json:"kind"`
	Changes    []string      `json:"changes,omitempty"`
	Name       string        `json:"name"`
	Context    string        `json:"context"`
	MatchBasis string        `json:"match_basis,omitempty"`
	Fields     []FieldChange `json:"fields,omitempty"`
	Before     *Observation  `json:"before,omitempty"`
	After      *Observation  `json:"after,omitempty"`
}

type SnapshotChange struct {
	Kind       string `json:"kind"`
	VMName     string `json:"vm_name"`
	Context    string `json:"context"`
	Name       string `json:"snapshot"`
	Before     string `json:"before,omitempty"`
	After      string `json:"after,omitempty"`
	CreateTime string `json:"create_time,omitempty"`
}

type SnapshotAge struct {
	VMName     string        `json:"vm_name"`
	Context    string        `json:"context"`
	Name       string        `json:"snapshot"`
	CreateTime time.Time     `json:"create_time"`
	Age        time.Duration `json:"age"`
	FirstSeen  time.Time     `json:"first_seen"`
	LastSeen   time.Time     `json:"last_seen"`
}

type Diff struct {
	Base         Run              `json:"base"`
	Target       Run              `json:"target"`
	VMs          []VMChange       `json:"vms,omitempty"`
	Snapshots    []SnapshotChange `json:"snapshots,omitempty"`
	SnapshotAges []SnapshotAge    `json:"snapshot_ages,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
	Coverage     []CoverageIssue  `json:"coverage,omitempty"`
	Policy       *PolicyResult    `json:"policy,omitempty"`
	Counts       DiffCounts       `json:"counts"`
}

type DiffCounts struct {
	Appeared  int `json:"appeared"`
	Vanished  int `json:"vanished"`
	Moved     int `json:"moved"`
	Modified  int `json:"modified"`
	Snapshots int `json:"snapshots"`
}

type ContextProgress struct {
	Context string
	Status  string
	VMs     int
	Error   error
}

type VMHistoryEntry struct {
	Run         Run                  `json:"run"`
	Observation Observation          `json:"observation"`
	Snapshots   []vsphere.VMSnapshot `json:"snapshots,omitempty"`
}

// CaptureLease identifies the one process allowed to write a live capture.
// Token is a fencing value: a stale process cannot save after its lease has
// expired and another process has taken over.
type CaptureLease struct {
	Token     string
	RunID     int64
	ExpiresAt time.Time
}

type RunMetadata struct {
	Label                  string
	Note                   string
	Pinned                 bool
	ToolVersion            string
	InventorySchemaVersion string
}

type CoverageIssue struct {
	Scope   string `json:"scope"`
	Context string `json:"context,omitempty"`
	Message string `json:"message"`
}

type PolicyViolation struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type PolicyResult struct {
	Passed     bool              `json:"passed"`
	Violations []PolicyViolation `json:"violations,omitempty"`
}

type VMHistoryEvent struct {
	Kind        string               `json:"kind"`
	Run         Run                  `json:"run"`
	Context     string               `json:"context"`
	Name        string               `json:"name"`
	Changes     []FieldChange        `json:"changes,omitempty"`
	Observation *Observation         `json:"observation,omitempty"`
	Snapshots   []vsphere.VMSnapshot `json:"snapshots,omitempty"`
}
