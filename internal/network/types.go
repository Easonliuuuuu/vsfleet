// Package network compares the network evidence in stored assessments.
//
// The package is deliberately offline: a comparison is a pure function of an
// assessment.ExportData value and never opens a vCenter session.
package network

// ClusterRef identifies the cluster selected for one side of a comparison.
type ClusterRef struct {
	Name      string `json:"name"`
	Context   string `json:"context,omitempty"`
	VCenterID string `json:"vcenter_id,omitempty"`
	HostCount int    `json:"host_count"`
	Resolved  bool   `json:"resolved"`
}

// NetworkSummary is the network configuration that is relevant to a
// cross-cluster migration. A nil security pointer means that the collector
// did not expose that part of the effective policy; it is not the same as
// false.
type NetworkSummary struct {
	Name            string   `json:"name"`
	VLAN            string   `json:"vlan,omitempty"`
	ParentSwitch    string   `json:"parent_switch,omitempty"`
	Type            string   `json:"type,omitempty"`
	MTU             int32    `json:"mtu,omitempty"`
	TeamingPolicy   string   `json:"teaming_policy,omitempty"`
	ActiveUplinks   []string `json:"active_uplinks,omitempty"`
	StandbyUplinks  []string `json:"standby_uplinks,omitempty"`
	Promiscuous     *bool    `json:"promiscuous,omitempty"`
	MACChanges      *bool    `json:"mac_changes,omitempty"`
	ForgedTransmits *bool    `json:"forged_transmits,omitempty"`
	CoveredHosts    int      `json:"covered_hosts"`
	TotalHosts      int      `json:"total_hosts"`
	Contact         string   `json:"contact,omitempty"`
	ContactDetail   string   `json:"contact_detail,omitempty"`
}

// NetworkMatch pairs a source network with the target network selected by
// VLAN or, when VLAN evidence is unavailable, by display name.
type NetworkMatch struct {
	Source     NetworkSummary `json:"source"`
	Target     NetworkSummary `json:"target"`
	MatchBasis string         `json:"match_basis"`
}

// AttachedVM identifies a VM found through the topology graph's reverse
// network relationship. Confidence and basis are retained so callers can see
// whether the attachment was confirmed by a port-group key or inferred by
// name.
type AttachedVM struct {
	Name       string `json:"name"`
	ID         string `json:"id,omitempty"`
	Context    string `json:"context,omitempty"`
	VCenterID  string `json:"vcenter_id,omitempty"`
	Confidence string `json:"confidence"`
	Basis      string `json:"basis"`
}

// MappingGap is a network present on the source but not on the target.
type MappingGap struct {
	Network     NetworkSummary `json:"network"`
	AttachedVMs []AttachedVM   `json:"attached_vms,omitempty"`
	VMsResolved bool           `json:"vms_resolved"`
	Severity    string         `json:"severity"`
}

// Difference is a field-level mismatch between a matched pair. Values stay
// strings so table and JSON consumers receive the same representation.
type Difference struct {
	Network  string `json:"network"`
	Field    string `json:"field"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Severity string `json:"severity"`
}

// Blindness explains why a context could not provide all evidence needed for
// this comparison.
type Blindness struct {
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

// Comparison is the deterministic, evidence-only result of comparing two
// clusters. An unresolved cluster is represented in Source or Target with
// Resolved=false; it is not treated as an empty cluster.
type Comparison struct {
	SchemaVersion   int              `json:"schema_version"`
	RunID           int64            `json:"run_id"`
	Source          ClusterRef       `json:"source"`
	Target          ClusterRef       `json:"target"`
	Matched         []NetworkMatch   `json:"matched"`
	MappingGaps     []MappingGap     `json:"mapping_gaps"`
	TargetOnly      []NetworkSummary `json:"target_only"`
	Differences     []Difference     `json:"differences"`
	Confidence      string           `json:"confidence"`
	CheckedContexts []string         `json:"checked_contexts"`
	Blind           []Blindness      `json:"blind,omitempty"`
	Ambiguous       bool             `json:"ambiguous,omitempty"`
	Candidates      []ClusterRef     `json:"candidates,omitempty"`
}

// Readiness is the migration verdict derived from a Comparison.
type Readiness struct {
	Comparison
	Verdict    string   `json:"verdict"`
	Blockers   []string `json:"blockers"`
	Advisories []string `json:"advisories"`
}
