package assessment

import (
	"database/sql"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// snapshotColumns are the snapshot_observations columns scanSnapshot reads.
const snapshotColumns = `moref,numeric_id,parent_moref,name,description,create_time,power_state,quiesced,current_snapshot,filename,data_bytes,total_bytes`

// scanSnapshot reads one row selected with snapshotColumns.
func scanSnapshot(rows *sql.Rows) (vsphere.VMSnapshot, error) {
	var snap vsphere.VMSnapshot
	var createTime int64
	var quiesced, current int
	var dataBytes, totalBytes sql.NullInt64
	if err := rows.Scan(&snap.ID, &snap.NumericID, &snap.ParentID, &snap.Name, &snap.Description, &createTime, &snap.PowerState, &quiesced, &current, &snap.Filename, &dataBytes, &totalBytes); err != nil {
		return vsphere.VMSnapshot{}, err
	}
	snap.CreateTime = time.UnixMilli(createTime).UTC()
	snap.Quiesced, snap.Current = quiesced != 0, current != 0
	if dataBytes.Valid {
		snap.DataBytes = &dataBytes.Int64
	}
	if totalBytes.Valid {
		snap.TotalBytes = &totalBytes.Int64
	}
	return snap, nil
}
