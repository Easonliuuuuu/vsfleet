package tui

import (
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDatastoreDetailShowsProvisionedSpace(t *testing.T) {
	bytes := func(gib int64) *int64 { v := gib << 30; return &v }
	for _, tc := range []struct {
		name        string
		uncommitted *int64
		want        string
		mark        rowStatus
	}{
		{"overcommitted", bytes(80), "140G (140% of capacity)", statusWarn},
		{"within capacity", bytes(10), "70.0G (70% of capacity)", statusNone},
		{"not read", nil, "-", statusNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := vsphere.Datastore{Name: "thin", Accessible: true, CapacityBytes: 100 << 30, FreeBytes: 40 << 30, UncommittedBytes: tc.uncommitted}
			r := datastoreRow(d, nil, false)
			if got, _ := fieldValue(r.detail, "Provisioned"); got != tc.want {
				t.Errorf("Provisioned = %q, want %q", got, tc.want)
			}
			if got := r.marks["Provisioned"].status; got != tc.mark {
				t.Errorf("Provisioned mark = %v, want %v", got, tc.mark)
			}
		})
	}
}
