# Inventory exports

<span id="deterministic-exports"></span>

## Export a stored run

Exports read a stored capture offline, `latest` unless `--run` names
another. `rvtools` writes an XLSX workbook; `--format csv` writes one
`<tab>.csv` per worksheet.

```sh
vsfleet export rvtools --file ./estate.xlsx
vsfleet export rvtools --format csv --file ./estate-csv/
```

Without `--file` the workbook goes to stdout, never to a terminal. An
existing destination needs `--force`. The same capture always produces the
same bytes.

## Worksheets

The `rvtools` profile writes these 25 RVTools worksheets:

`vInfo` · `vCPU` · `vMemory` · `vDisk` · `vPartition` · `vNetwork` · `vCD` · `vUSB` ·
`vSnapshot` · `vTools` · `vSource` · `vRP` · `vCluster` · `vHost` · `vHBA` · `vNIC` · `vSwitch` ·
`vPort` · `dvSwitch` · `dvPort` · `vSC_VMK` · `vDatastore` · `vMultiPath` ·
`vFileInfo` · `vHealth`

vsfleet adds `vsfleetCoverage` and [`vsfleetPerformance`](performance.md#in-reports).
Opt-in sheets are `vLicense` ([licenses](licensing.md), keys redacted),
`vsfleetMetadata` ([metadata](metadata.md#workbook-sheet-and-sharing-profiles))
and the [sharing profiles](sharing.md). `vsfleet explain rvtools` lists every
column's type, unit, and when it is left empty.

`vsfleetCoverage` records, for every sheet and context, whether collection
succeeded, how many rows it has, and why it is incomplete. An empty sheet
means nothing was found only when its coverage says so. Captures taken by an
older vsfleet leave newer columns empty and mark them `not recorded`.

<span id="source-provenance-vsource"></span>
<span id="guest-partitions-need-vmware-tools"></span>
<span id="resource-pools-are-export-evidence"></span>
<span id="host-storage-and-network-inventory"></span>
<span id="distributed-switch-inventory"></span>

Notes on specific sheets:

- **`vSource`**: one row per context with the vCenter's version and build,
  plus a `vsfleet Context` column.
- **`vPartition`**: guest filesystems come from VMware Tools, so powered-off
  VMs and VMs without running Tools have no rows. `Disk Key` joins them to
  `vDisk` on vSphere 7.0 and later.
- **`vRP`**: resource pools, then vApps, with their configured CPU and memory
  allocation. Volatile usage figures are not included.
- **`vHost`**: density columns (`# VMs`, `# vCPUs`, `vRAM`, per-core ratios)
  count powered-on VMs only, as RVTools does. `Certificate Status` is not
  written.
- **`dvPort`**: one row per distributed port group, not per runtime port.

## Datastore file inventory (vFileInfo)

`vFileInfo` lists the files on each datastore. It is off by default because
browsing costs time and the `Datastore.Browse` privilege, and file paths can
reveal VM, project and customer names.

```sh
vsfleet capture run --all-contexts --datastore-file-inventory
vsfleet export rvtools --file ./estate.xlsx
```

File contents are never read, and the file list never changes health or
orphan verdicts. The limits are:

| Flag | Default | When reached |
| --- | --- | --- |
| `--file-inventory-max-files` | 100,000 per datastore | the datastore is `truncated` |
| `--file-inventory-max-total-files` | 500,000 per context | remaining datastores are `skipped` |
| `--file-inventory-timeout` | 10m per datastore | the datastore is `failed` |

Each datastore is `complete`, `truncated`, `denied`, `failed`, `skipped`, or
`unavailable`; only `complete` proves a file is absent. The status appears in
`vsfleetCoverage`, in a warning on stderr, and in JSON output. Without file
rows, `vFileInfo` holds one explanatory row, which does not mean the storage
is empty.

<span id="columns-and-compatibility"></span>

The first eight columns match RVTools `-GetFileInfo`; vsfleet adds
`Datastore`, `Datastore ID`, `Datacenter` and `vsfleet Context` so repeated
datastore names stay distinct. Folders are left out and a disk's size is on
its descriptor `.vmdk` row, as in RVTools. Rows sort by context, datastore and
path, so compare by key rather than position.

Treat a workbook or database holding file inventory as sensitive. It takes
roughly 100 to 200 bytes per file. XLSX holds at most 1,048,575 rows per
sheet; CSV has no limit, but keeps names starting with `=`, `+`, `-` or `@`,
which a spreadsheet may evaluate.

## Importing an RVTools export

`vsfleet capture import rvtools FILE` stores an RVTools workbook as a
capture, offline and without configuration. `diff`, `describe vm --history`,
`get --run` and `analyze` then work on it like a live capture.

```sh
vsfleet capture import rvtools estate.xlsx --dry-run
vsfleet capture import rvtools estate.xlsx --label pre-migration --captured-at 2026-01-01T00:00:00Z
```

`--dry-run` shows which worksheets and columns would be used and which
ignored. Worksheets match by name and columns by header, in any order.
`vInfo` is required; VM, host, cluster, datastore, switch, port, HBA, NIC,
multipath and device sheets are read when present.

What to expect:

- **Missing is unknown, never zero.** A missing sheet or column becomes a
  coverage gap. It can lower a verdict's confidence but never improve it.
- **Imports are always partial**, because RVTools has no resource pool
  membership or network IDs. Trends need `--include-partial`.
- **Diffs against a live capture** skip every field the import left empty
  and name them in a warning.
- **VMs are matched** by managed-object ID, then instance UUID, BIOS UUID,
  and name, so same-named VMs in different vCenters stay distinct. Host
  devices join only by ID or a host name unique in that vCenter; ambiguous
  rows are skipped with a warning.
- **Capture time** comes from `--captured-at`, then `vMetaData`, then the
  workbook properties, then import time. Pass `--timezone` with an IANA
  zone when the workbook's timestamps have none.
- **Some values are derived.** Host threads come from cores and
  hyperthreading; usage percentages are whole numbers, so imported usage is
  accurate to about 1%; VMware Tools running state is known only for
  `toolsNotInstalled` and `toolsNotRunning`.
- **Importing the same file twice** creates a second capture with a warning;
  `--allow-duplicate` silences it. `--context-map KEY=NAME` renames an
  imported context.

Workbooks are size-limited, formulas are never evaluated, external links are
never followed, and a failed import stores nothing.

## RVTools file interoperability

vsfleet's sheet names, column order and cell types matched RVTools 4.8.1.4
and 4.8.2.1 on a vCenter 8.0.3 lab, and pandas and openpyxl read both
workbooks. Compatibility covers the worksheets above only, and tags, custom
attributes and license values have not been compared.

Known value differences:

| Sheet / column | vsfleet | RVTools |
| --- | --- | --- |
| `vHost`, `Boot time`, `BIOS Date`, certificate dates; `vSnapshot`, `Date / time` | UTC | Collector-local time without a zone |
| `vHost`, `# VMs total` | Includes templates | Excludes templates |
| `vHost`, certificate issuer, subject and dates | Filled | Empty |
| `vHBA`, `Type` | vSphere type, such as `HostBlockHba` | Label, such as `Block SCSI` |
| `dvPort`, `VLAN` on an uplink port group | `trunk 0-4094` | Empty |
| `vCluster`, `DRS default VM behavior` with DRS off | Empty | The stored level |
| Capacity, free and usage MiB and `%` columns | Decimals | Integers |
| `vTools`, `Tools Running Status` | vsfleet extension | Not present |

Convert `vSnapshot` times to the collector's zone before comparing them with
RVTools.

vsfleet is a personal open-source project, not an official Dell Technologies
product, and is not sponsored, endorsed, or supported by Dell Technologies. Its
export interoperability was independently implemented without RVTools source
code or non-public documentation. RVTools is a Dell Technologies product;
references here describe export-file interoperability only.
