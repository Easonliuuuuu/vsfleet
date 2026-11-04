package vsphere

// HAShortfall is one resource whose current HA failover capacity is below
// what the admission-control policy is configured to keep.
type HAShortfall struct {
	// Resource is "CPU", "memory" or "host failures".
	Resource string
	// Current and Configured are percentages for CPU and memory, and host
	// counts for host failures.
	Current, Configured int32
}

// HAFailover compares a cluster's current HA failover capacity with what
// its policy keeps. The TUI cluster pages and the ha-failover-capacity-low
// health rule both read it, so they never disagree about which clusters are
// short.
type HAFailover struct {
	// Known is false when the policy has no capacity to compare (dedicated
	// failover hosts, or no policy read) or vCenter reported none.
	Known bool
	Short []HAShortfall
	// Refusing reports that admission control is on, so vCenter refuses
	// power-ons and new VMs while the cluster is short.
	Refusing bool
}

// Failover compares the cluster's current failover capacity with its
// policy. A nil receiver, a cluster without HA read, is unknown.
//
// Under the resource-percent policy vCenter's current percentages are
// optional fields that serialize as zero when absent, so a cluster
// reporting zero for both is read as "not reported", not as "nothing left".
func (h *ClusterHA) Failover() HAFailover {
	if h == nil {
		return HAFailover{}
	}
	var out HAFailover
	switch h.Policy {
	case HAPolicyHostFailures:
		if h.FailoverLevel <= 0 {
			return HAFailover{}
		}
		out.Known = true
		if h.CurrentFailoverLevel < h.FailoverLevel {
			out.Short = append(out.Short, HAShortfall{Resource: "host failures", Current: h.CurrentFailoverLevel, Configured: h.FailoverLevel})
		}
	case HAPolicyResources:
		if h.CPUFailoverPct == 0 && h.MemFailoverPct == 0 {
			return HAFailover{}
		}
		out.Known = true
		if h.CPUFailoverPct < h.CPUReservePct {
			out.Short = append(out.Short, HAShortfall{Resource: "CPU", Current: h.CPUFailoverPct, Configured: h.CPUReservePct})
		}
		if h.MemFailoverPct < h.MemReservePct {
			out.Short = append(out.Short, HAShortfall{Resource: "memory", Current: h.MemFailoverPct, Configured: h.MemReservePct})
		}
	default:
		return HAFailover{}
	}
	out.Refusing = len(out.Short) > 0 && h.AdmissionControl
	return out
}
