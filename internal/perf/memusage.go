package perf

// FromMemUsage marks a reading whose active memory came from mem.usage.
const FromMemUsage = "active memory from " + string(MemUsage) + "; " + string(MemActive) + " is not kept"

// UsesMemUsage reports whether active memory is judged from mem.usage
// rather than mem.active. That happens only when mem.active could not be
// read at all, usually because the interval keeps statistics level 1, and
// mem.usage could. A sparse mem.active is never replaced: its gaps are
// evidence about the VM, not about the statistics level.
//
// On vCenter 8.0.3 a VM's mem.usage equals mem.active over configured
// memory to within 0.01 points, despite its "Host consumed %" label.
func UsesMemUsage(summaries []Summary) bool {
	var active, usage *Summary
	for i := range summaries {
		switch summaries[i].Metric {
		case MemActive:
			active = &summaries[i]
		case MemUsage:
			usage = &summaries[i]
		}
	}
	return (active == nil || active.Status == StatusUnavailable) && usage != nil && usage.Status != StatusUnavailable
}
