// Package topology builds deterministic, offline relationship queries over a
// stored assessment. It intentionally knows nothing about credentials,
// sessions, or a live vCenter.
package topology

import (
	"fmt"
	"strings"
)

type Kind string

const (
	KindVM           Kind = "vm"
	KindTemplate     Kind = "template"
	KindHost         Kind = "host"
	KindCluster      Kind = "cluster"
	KindDatastore    Kind = "datastore"
	KindNetwork      Kind = "network"
	KindDVSwitch     Kind = "dvswitch"
	KindResourcePool Kind = "resourcepool"
)

// ParseKind accepts the query vocabulary, including capture-only kinds and
// their common plurals.
func ParseKind(value string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "vm", "vms", "virtualmachine", "virtualmachines":
		return KindVM, nil
	case "template", "templates", "tpl":
		return KindTemplate, nil
	case "host", "hosts", "esxi":
		return KindHost, nil
	case "cluster", "clusters":
		return KindCluster, nil
	case "datastore", "datastores", "ds":
		return KindDatastore, nil
	case "network", "networks", "portgroup", "portgroups":
		return KindNetwork, nil
	case "dvswitch", "dvswitches", "distributed-switch", "distributed-switches":
		return KindDVSwitch, nil
	case "resourcepool", "resourcepools", "resource-pool", "resource-pools", "pool", "pools":
		return KindResourcePool, nil
	default:
		return "", fmt.Errorf("unknown kind %q (supported: vm, template, host, cluster, datastore, network, dvswitch, resourcepool)", value)
	}
}

type Relation string

const (
	RelationContains   Relation = "contains"
	RelationRunsOn     Relation = "runs-on"
	RelationStoredOn   Relation = "stored-on"
	RelationAttachedTo Relation = "attached-to"
	RelationMemberOf   Relation = "member-of"
	RelationCarriedBy  Relation = "carried-by"
	RelationPresents   Relation = "presents"
)

type Basis string

const (
	BasisMoref           Basis = "moref"
	BasisInstanceUUID    Basis = "instance-uuid"
	BasisPortGroupKey    Basis = "portgroup-key"
	BasisSwitchUUID      Basis = "switch-uuid"
	BasisBackingIdentity Basis = "backing-identity"
	BasisVMRef           Basis = "vm-ref"
	BasisDiskPath        Basis = "disk-path"
	BasisName            Basis = "name"
)

type EdgeConfidence string

const (
	EdgeConfirmed  EdgeConfidence = "confirmed"
	EdgeInferred   EdgeConfidence = "inferred"
	EdgeUnresolved EdgeConfidence = "unresolved"
)

type ResultConfidence string

const (
	ConfidenceComplete ResultConfidence = "complete"
	ConfidencePartial  ResultConfidence = "partial"
	ConfidenceUnknown  ResultConfidence = "unknown"
)

type Node struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ID         string `json:"id,omitempty"`
	Context    string `json:"context,omitempty"`
	VCenterID  string `json:"vcenter_id,omitempty"`
	Datacenter string `json:"datacenter,omitempty"`
	Path       string `json:"path,omitempty"`
}

type Edge struct {
	From       Node           `json:"from"`
	To         Node           `json:"to"`
	Relation   Relation       `json:"relation"`
	Basis      Basis          `json:"basis"`
	Confidence EdgeConfidence `json:"confidence"`
	Detail     string         `json:"detail,omitempty"`
}

type Subject struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Members  []Node   `json:"members"`
	Identity []string `json:"identity,omitempty"`
	Basis    Basis    `json:"basis"`
}

type Blindness struct {
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

type Result struct {
	SchemaVersion   int             `json:"schema_version"`
	RunID           int64           `json:"run_id"`
	Query           string          `json:"query"`
	Kind            string          `json:"kind"`
	Direction       string          `json:"direction"`
	Ambiguous       bool            `json:"ambiguous,omitempty"`
	Subjects        []SubjectResult `json:"subjects"`
	CheckedContexts []string        `json:"checked_contexts,omitempty"`
	Blind           []Blindness     `json:"blind,omitempty"`
}

type SubjectResult struct {
	Subject    Subject          `json:"subject"`
	Ancestors  []Edge           `json:"ancestors,omitempty"`
	Edges      []Edge           `json:"edges,omitempty"`
	Confidence ResultConfidence `json:"confidence"`
	Blind      []Blindness      `json:"blind,omitempty"`
	Unresolved []string         `json:"unresolved,omitempty"`
}
