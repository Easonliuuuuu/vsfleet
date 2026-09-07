# Assessments and History

Assessments are explicit, read-only captures of VM, host, cluster, datastore,
and snapshot state. They are stored locally in SQLite and never sent back to
vCenter.

## Capture and inspect

```sh
vsfleet assessment run --all-contexts
vsfleet assessment run --all-contexts --browse-datastores
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
`vHBA`, `vNIC`, `vSwitch`, `vPort`, `dvSwitch`, `dvPort`, `vSC+VMK`,
`vMultiPath`, `vCluster`, `vRP`, `vDatastore`, `vSnapshot`, `vHealth`, and
`vsfleetCoverage` sheets.

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

### Health findings

`vsfleet health [RUN]` evaluates the evidence in a stored assessment without
contacting vCenter. Each finding includes a stable rule ID, category, severity,
object and vCenter context, measured evidence, and a recommendation. Categories
are `migration`, `availability`, `security`, `capacity`, and `hygiene`.

The defaults are a 30-day maximum snapshot age and 10% minimum free space for
datastores and guest filesystems. Use `--max-snapshot-age`,
`--min-datastore-free`, `--min-guest-disk-free`, `--disable-rule`, and
`--severity` and `--category` to tune a run. `--wide` adds recommendations and
evidence to the table; JSON always includes them. `--fail-on-findings` returns
exit code 2 when a finding at or above the selected severity exists; invalid
selectors and other command errors return 1. `--list-rules` prints the rule
registry, including categories.

Snapshot age is measured from the assessment's own context finish time (falling
back to the run finish time), never from the current wall clock. Findings are
recomputed when read, but the thresholds are stamped into the `vHealth`
coverage message, so exporting unchanged evidence with the same options stays
reproducible. Rules that need inventory fields introduced after an older run
are marked `not-evaluated`, rather than making an empty tab look healthy. A
collector that failed, or a collection that was not recorded, makes the
affected rule `unknown`; a partially answered rule keeps real findings but
names its blind contexts. Incomplete evidence is never represented as a clean
pass.

Zombie-VMDK evidence is opt-in because it requires the vSphere
`Datastore.Browse` privilege and adds a bounded directory listing per
accessible datastore. Use `vsfleet assessment run --browse-datastores` and
then inspect `vsfleet assessment orphans [RUN]`. Orphan candidates are
estate-wide rather than name-matched within one vCenter: VMFS UUIDs, extents,
NFS exports, vVol IDs, and datastore URLs join observations where available.

Each candidate is classified as `verified-unreferenced`,
`suspected-unreferenced`, `referenced-other-context`, or
`unknown-incomplete-coverage`. Snapshot chains are matched in both directions,
and a truncated browse, failed relevant collection, or missing backing identity
prevents a verified verdict. `health --fail-on-findings --severity warning`
therefore fails only on verified orphans; low-confidence guesses are
informational. `assessment orphans -o json` exposes the paths, sizes,
timestamps, identity keys, references, and coverage reasons. Connected floppy
devices remain a named follow-up.

### Migration readiness

`vsfleet assessment findings [RUN]` is the assessment-prefixed equivalent of
`vsfleet health`. `vsfleet assessment readiness [RUN]` evaluates the same
stored evidence and returns `ready`, `blocked`, or `unknown`. Migration
findings at warning or critical severity are blockers; informational migration
findings are advisories. `--fail-on-blockers` returns exit code 2 when blockers
exist.

Readiness is deliberately conservative: a failed or missing collector yields
`unknown`, and a blind vCenter is named in the `Not evaluated` section. The
verdict can never say `ready` over evidence that was not collected.

### RVTools file interoperability

The `rvtools` export profile renders twenty-one worksheet layouts used by RVTools
exports, so a downstream tool that reads those worksheet names and columns can
consume the corresponding parts of a vsfleet export:

`vInfo` · `vCPU` · `vMemory` · `vDisk` · `vPartition` · `vNetwork` · `vTools` ·
`vHost` · `vHBA` · `vNIC` · `vSwitch` · `vPort` · `dvSwitch` · `dvPort` ·
`vSC+VMK` · `vMultiPath` · `vCluster` · `vRP` · `vDatastore` · `vSnapshot` ·
`vHealth`

Compatibility is limited to the listed worksheet names and columns. Other
worksheets are outside this export profile, so a downstream pipeline that
requires them is not supported. This is an interoperability export, not RVTools
and not a replacement for it.

`vsfleet compatibility report` prints the full column-level reference: every
worksheet, each column's type and unit, and when a cell is left empty. It is
generated from the same definitions the exporter writes from, so it cannot
drift from the workbook, and it needs no configuration, no keyring and no
vCenter:

```sh
vsfleet compatibility report --sheet vPartition
vsfleet compatibility report -o json | jq
```

It describes what vsfleet emits and what those values mean. It makes no claim
about any other tool's schema; compare it against what your pipeline
requires.

vsfleet is a personal open-source project, not an official Dell Technologies
product, and is not sponsored, endorsed, or supported by Dell Technologies. Its
export interoperability was independently implemented without RVTools source
code or non-public documentation. RVTools is a Dell Technologies product;
references here describe export-file interoperability only.

### Guest partitions need VMware Tools

`vPartition` reports what the guest sees: filesystem paths, capacity, consumed
and free space. Only VMware Tools inside the guest can measure that — vSphere
knows how large a virtual disk is, never how much of it the guest has used. A
powered-off VM, or one whose Tools are not running, therefore contributes no
`vPartition` rows at all.

Each row also carries the `Disk Key` of the virtual disk behind the
filesystem, which joins to the column of the same name in `vDisk` — that is
how a sizing tool ties consumed space to the disk it has to provision. VMware
Tools only reports the mapping on vSphere 7.0 and later, so the cell is empty
on older estates, and a volume spanning several disks lists each of them.

Because a short tab would otherwise be indistinguishable from a small estate,
`vsfleetCoverage` marks the tab `partial` and names the shortfall — for
example `18 of 40 VMs reported guest filesystems; the rest had no running
VMware Tools`. Captures taken before this tab existed are marked
`not recorded` rather than empty.

### Resource pools are export evidence

`vRP` includes every resource pool in the datacenter scope, including each
cluster or standalone host's root `Resources` pool. vApps are reported on their
own terms and are not duplicated as resource-pool rows. Resource pools are
captured for the export and assessment ledger, not made into a browsable TUI
tab or CLI inventory noun.

The tab covers resource-pool identity and CPU/memory allocation configuration.
This read-only capture does not request additional volatile or unavailable
runtime fields, so it leaves them out rather than guessing values.

### Host storage and network inventory

The host-scoped sheets add storage adapters (`vHBA`), one aggregate row per
host/LUN with path-state counts (`vMultiPath`), physical NICs (`vNIC`),
standard virtual switches (`vSwitch`), standard port groups (`vPort`), and
VMkernel or legacy service-console adapters (`vSC+VMK`). They are collected
from `HostSystem.config.storageDevice` and `HostSystem.config.network` during
assessment capture. Search, host listing, and the TUI keep their summary fetch;
the host configuration properties are deliberately not added to those paths.

### Distributed switch inventory

Assessment capture records distributed virtual switches and their distributed
port groups from the vSphere `config` and `summary` properties. The `dvSwitch`
sheet includes switch identity, MTU, host membership, uplinks, link discovery,
LACP and product information. The `dvPort` sheet has one row per distributed
port group and its default VLAN, teaming, security and shaping policy; it is
not one row per runtime distributed port. Per-port runtime state is outside
this read-only profile. Captures before inventory schema 10 mark both sheets
`not recorded` in `vsfleetCoverage`.

The network browsing path also enriches distributed port groups with their
parent switch and VLAN, so `vsfleet network list` answers that common join
without requiring an assessment capture.

The `vsfleetCoverage` sheet records every tab and vCenter in the run with its
collection status, item count, and any error. A partial estate is reported as
partial rather than handed over as if it were whole.

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
