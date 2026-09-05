# Assessments and History

Assessments are explicit, read-only captures of VM, host, cluster, datastore,
and snapshot state. They are stored locally in SQLite and never sent back to
vCenter.

## Capture and inspect

```sh
vsfleet assessment run --all-contexts
vsfleet assessment run --all-contexts \
  --label nightly --note "pre-change baseline" --pin
vsfleet assessment list
vsfleet assessment report latest
vsfleet vm history billing --all-observations
```

Use labels and notes to make recurring baselines easy to select. Pin a baseline
to protect it from retention cleanup; use `assessment update <run> --unpin`
before removing it.

## Compare runs

```sh
vsfleet assessment diff previous latest
vsfleet assessment diff nightly latest \
  --fail-on moved,vanished \
  --max-snapshot-age 30d \
  --require-complete
```

Diffs compare only vCenters collected successfully in both runs. An outage
therefore cannot masquerade as mass VM deletion. `assessment diff` returns exit
code `2` for a requested drift-policy violation; execution or selector errors
continue to return `1`.

The policy flags are command-line only so scheduled jobs are auditable from
their invocation:

```sh
vsfleet assessment run --all-contexts --label nightly
vsfleet assessment diff nightly latest \
  --fail-on appeared,vanished,moved \
  --fail-on snapshot-created,snapshot-removed \
  --max-snapshot-age 30d --require-complete -o json > drift.json
```

Use `--include-runtime` when volatile power, guest, IP, VMware Tools, or storage
fields should participate in a diff.

## Snapshots, trends, and reports

```sh
vsfleet assessment snapshots --older-than 30d
vsfleet assessment trends churn
vsfleet assessment trends snapshots --older-than 30d
vsfleet assessment trends capacity --kind all
vsfleet assessment report latest
```

Trends aggregate estate totals before context and resource drill-downs. By
default they use complete assessments; use `--include-partial` when partial
runs are intentionally part of the analysis.

## Deterministic exports

Exports read one persisted run and do not contact vCenter or open a live
session. The `rvtools` format is an XLSX workbook containing `vInfo`, `vCPU`,
`vMemory`, per-VM `vDisk`, `vPartition` and `vNetwork`, `vTools`, `vHost`,
`vCluster`, `vDatastore`, `vSnapshot`, and `vsfleetCoverage` sheets.

```sh
vsfleet assessment export latest --format rvtools --file ./estate.xlsx
vsfleet assessment export latest --format csv --file ./estate-csv/
```

The destination is required: a `.xlsx` file for `rvtools` or a directory for
`csv`. An existing destination needs `--force`. Re-exporting unchanged evidence
produces byte-identical output in either format. CSV creates one `<tab>.csv`
file per sheet for `jq`, `awk`, `pandas`, and source-control diffing.

Runs captured before VMware Tools version collection still populate the running
status column in `vTools`; version columns remain blank and the gap is recorded
on `vsfleetCoverage`.

### What "RVTools-compatible" means here

vsfleet renders eleven RVTools tabs and uses RVTools' own column names, so a
tool that reads those tabs by name can read a vsfleet export:

`vInfo` · `vCPU` · `vMemory` · `vDisk` · `vPartition` · `vNetwork` · `vTools` ·
`vHost` · `vCluster` · `vDatastore` · `vSnapshot`

RVTools itself ships roughly thirty tabs. `vHealth`, `vRP`, `vHBA`, `vNIC`,
`vSwitch` and `vLicense` are among those vsfleet does not render today. If a downstream sizing or migration tool requires one of them,
vsfleet is not yet a drop-in for that pipeline. This is a **compatible**
export, not a replacement for RVTools.

### Guest partitions need VMware Tools

`vPartition` reports what the guest sees: filesystem paths, capacity, consumed
and free space. Only VMware Tools inside the guest can measure that — vSphere
knows how large a virtual disk is, never how much of it the guest has used. A
powered-off VM, or one whose Tools are not running, therefore contributes no
`vPartition` rows at all.

Because a short tab would otherwise be indistinguishable from a small estate,
`vsfleetCoverage` marks the tab `partial` and names the shortfall — for
example `18 of 40 VMs reported guest filesystems; the rest had no running
VMware Tools`. Captures taken before this tab existed are marked
`not recorded` rather than empty.

The `vsfleetCoverage` sheet is the part RVTools has no equivalent for: it names
every tab and vCenter in the run with its collection status, item count, and
the error where one occurred. A partial estate is reported as partial rather
than handed over as if it were whole.

## Retention and recovery

```sh
vsfleet assessment prune --older-than 90d       # dry-run
vsfleet assessment prune --older-than 90d --execute
vsfleet assessment backup ./history-backup.db
vsfleet assessment restore ./history-backup.db --force
vsfleet assessment doctor
vsfleet assessment delete <run> --force
```

Only one mutating operation may write the history database at a time. Capture,
prune, backup, and restore use a fenced lease; listing, diffing, and opening the
TUI never take that lease. Backups are consistent SQLite snapshots. Restore
creates an automatic pre-restore safety copy before replacing the active
database.

The database defaults to `<user-config-dir>/vsfleet/history.db`. Override it
with `--history-db <path>` or `VSFLEET_HISTORY_DB`. It may contain operational
inventory, so protect it using normal host disk and backup controls.
