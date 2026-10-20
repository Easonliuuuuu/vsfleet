package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// TestHistoryInspectorNamesFieldsAndUnits checks that the change inspector
// shows field changes in operator names and units, with their values in one
// column, rather than as stored paths and raw numbers (issue #472).
func TestHistoryInspectorNamesFieldsAndUnits(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	m.changeDiff = &assessment.Diff{Target: assessment.Run{ID: 2}}
	v := assessment.VMChange{After: &assessment.Observation{VM: vsphere.VM{CPU: 4, MemoryMB: 8192}}, Fields: []assessment.FieldChange{
		{Field: "cpu", Before: "2", After: "4"},
		{Field: "memory", Before: "4096", After: "8192"},
		{Field: "migration_configuration.cpu_sockets", Before: "2", After: "4"},
		{Field: "migration_configuration.firmware", Before: `"bios"`, After: `"efi"`},
		{Field: "migration_configuration.secure_boot_enabled", Before: "false", After: "true"},
		{Field: "migration_configuration.disks[2000].capacity_bytes", Before: "42949672960", After: "85899345920"},
		{Field: "migration_configuration.memory_allocation.limit", Before: "-1", After: "4096"},
	}}
	view := ansi.Strip(strings.Join(m.vmInspectorFields(v), "\n"))
	// Only the field changes: the State block above also says "vCPU".
	var changes []string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, " → ") {
			changes = append(changes, line)
		}
	}
	fields := strings.Join(changes, "\n")
	for _, tc := range []struct{ name, want string }{
		{"cpu", "  vCPU "},
		{"memory in the State block's units", "4G → 8G"},
		{"migration field by its name", "CPU sockets"},
		{"JSON string unquoted", "bios → efi"},
		{"boolean as a word", "no → yes"},
		{"disk by key and size", "disk 2000 size"},
		{"disk size in bytes units", "40.0G → 80.0G"},
		{"unset limit", "memory limit"},
		{"unset limit value", "unlimited → 4G"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(fields, tc.want) {
				t.Fatalf("inspector is missing %q:\n%s", tc.want, view)
			}
		})
	}
	t.Run("no stored paths", func(t *testing.T) {
		if strings.Contains(view, "migration_configuration") || strings.Contains(view, "capacity_bytes") {
			t.Fatalf("inspector shows a stored field path:\n%s", view)
		}
	})
	t.Run("values line up", func(t *testing.T) {
		// Each field's before value, in the order the fields were given.
		befores := []string{"2", "4G", "2", "bios", "no", "40.0G", "unlimited"}
		if len(changes) != len(befores) {
			t.Fatalf("want %d field changes, got %d:\n%s", len(befores), len(changes), view)
		}
		column := strings.Index(changes[0], " "+befores[0]+" → ")
		for i, line := range changes {
			if at := strings.Index(line, " "+befores[i]+" → "); at != column {
				t.Fatalf("field values start at %d and %d:\n%s", column, at, view)
			}
		}
	})
}

// TestSnapshotChangeUsesTheHubDateFormat checks that a snapshot row dates
// itself as the rest of the hub does, not in RFC 3339 (issue #472).
func TestSnapshotChangeUsesTheHubDateFormat(t *testing.T) {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	d := &assessment.Diff{Snapshots: []assessment.SnapshotChange{{Kind: "created", VMName: "web-05", Context: "prod", Name: "pre-patch", After: at.Format(time.RFC3339), CreateTime: at.Format(time.RFC3339)}}}
	rows := classifyDiff(d)
	if len(rows) != 1 {
		t.Fatalf("rows=%+v", rows)
	}
	want := "snapshot created · " + at.Local().Format("2006-01-02 15:04")
	if got := rowDetail(rows[0].row); got != want {
		t.Fatalf("snapshot row reads %q, want %q", got, want)
	}
}

// TestTimelineNamesEventKindsInWords checks that the timeline list and its
// detail spell a stored event kind as words (issue #473).
func TestTimelineNamesEventKindsInWords(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	m.width, m.height = 140, 40
	obs := &assessment.Observation{Context: "prod", VM: vsphere.VM{Name: "old-api-22", CPU: 2, MemoryMB: 4096}}
	m.timeline = []assessment.VMHistoryEvent{
		{Kind: "first_seen", Run: assessment.Run{ID: 1}, Context: "prod", Name: "old-api-22", Observation: obs},
		{Kind: "snapshot-created", Run: assessment.Run{ID: 2}, Context: "prod", Name: "old-api-22", Observation: obs, Changes: []assessment.FieldChange{{Field: "snapshot", After: "x"}}},
		{Kind: "vanished", Run: assessment.Run{ID: 3}, Context: "prod", Name: "old-api-22", Observation: obs},
	}
	lines, _ := m.viewTimelineChanges()
	list := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"first seen", "snapshot created", "vanished"} {
		if !strings.Contains(list, want) {
			t.Fatalf("timeline list is missing %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, "first_seen") || strings.Contains(list, "snapshot-created") {
		t.Fatalf("timeline list shows a stored event kind:\n%s", list)
	}
	m.timelineCursor = 0
	detail := ansi.Strip(strings.Join(m.viewHistoryTimelineDetail(), "\n"))
	if !strings.Contains(detail, "first seen") {
		t.Fatalf("timeline detail does not name the event in words:\n%s", detail)
	}
}
