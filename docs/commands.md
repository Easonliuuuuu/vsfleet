# CLI reference

<span id="cli-guide"></span>

Run `vsfleet` to open the terminal UI, or add a subcommand for scripts.
[Persistent flags](#persistent-flags) set context, timeout, configuration,
history, and output.

A context is one vCenter, and it plays the part of a kubectl namespace:
commands act on the current context, `-n NAME` picks others, and `-A` spans
every context. The `config` verbs match kubectl's: `get-contexts`,
`current-context`, `use-context`, `set-context`, `delete-context` and
`view`. `ctx` is an alias of `config`.

## Common commands

| Command | Purpose |
|---|---|
| `vsfleet` / `vsfleet ui` | Open the terminal UI |
| `vsfleet demo` | Open the terminal UI on sample data, with no vCenter and nothing written |
| `vsfleet config set-context [name]` | Add a vCenter context via the wizard or flags, or change one |
| `vsfleet config get-contexts` | List configured contexts |
| `vsfleet config current-context` | Print the current context |
| `vsfleet config use-context <name>` | Select the current context |
| `vsfleet config view [--minify]` | Show the configuration; credentials appear as references |
| `vsfleet config delete-context <name>` | Delete a context and invalidate its session |
| `vsfleet cluster-info [dump]` | Show each context's connection; `dump` writes every live kind as one JSON document |
| `vsfleet version` | Print the vsfleet version |
| `vsfleet doctor [context...]` | Diagnose the connection stage by stage |
| `vsfleet analyze health` | Run the health rules against a capture and list the findings; see [health](health.md) |
| `vsfleet analyze readiness` | Return a migration-readiness verdict |
| `vsfleet analyze readiness network <source> <target>` | Return a cross-cluster network-readiness verdict; `-o wide` adds the full comparison |
| `vsfleet analyze sizing` | Test whether stored VMs fit a proposed destination (allocation based, offline) |
| `vsfleet analyze capacity [--trend]` | Attribute datastore growth and project free-space thresholds, or tabulate capacity per capture |
| `vsfleet analyze orphans` | Explain estate-wide browsed VMDK orphan evidence |
| `vsfleet analyze churn` | Show VM population and churn across captures |
| `vsfleet analyze snapshots [--trend]` | Show snapshot ages in a capture, or their trend across captures |
| `vsfleet analyze topology <kind>/<name>` | Show a subject's containment and attachments |
| `vsfleet analyze deps <kind>/<name>` | Show what a subject depends on |
| `vsfleet analyze blast-radius <kind>/<name>` | Show the subjects a failure would reach |
| `vsfleet analyze decommission vm/<name>` | Review stored evidence before decommissioning a VM |
| `vsfleet api-resources` | List every object kind with its short names and whether it is live, stored, or both |
| `vsfleet get <kind>[,<kind>...] [name...]` | List objects of any kind, or `all` inventory kinds, live or from a capture with `--run`; names may be globs, and `<kind>/<name>` also works |
| `vsfleet describe <kind> <name>` | Show one VM, template, host, cluster, vApp, datastore, or network in full detail, live or with `--run` |
| `vsfleet describe capture <run>` | Show what a stored capture recorded, per context and kind, and where it has gaps |
| `vsfleet describe vm <name-or-uuid> --history` | Show how a VM changed across every stored capture |
| `vsfleet events --for vm/<name> [--all]` | Show a VM's live vCenter event log: migrations, reconfigurations, snapshots and failures, with who and when; task results come from the VM's task history, and a deleted VM is found through stored history |
| `vsfleet top vm [name...]` | Show VM CPU and memory use from stored performance collections; see [performance history](performance.md) |
| `vsfleet explain <kind>[.<field>]` | Document a kind's columns and the RVTools worksheets it feeds |
| `vsfleet datastore ls <datastore> [path]` | List one datastore directory |
| `vsfleet datastore find <datastore> <pattern>` | Recursively search a datastore for a name or pattern |
| `vsfleet capture run [-A] [--label <name>]` | Take a capture of the estate into the local capture database; see [captures](#captures) |
| `vsfleet capture import rvtools <file.xlsx>` | Import an RVTools-compatible export as a new capture, offline |
| `vsfleet capture list` | List stored captures; the same as `vsfleet get captures` |
| `vsfleet capture diff [base] [target]` | Compare two captures, optionally failing on policy with `--fail-on` or `--require-complete` |
| `vsfleet capture update <run> \| delete <run> \| prune` | Label, note or pin a capture, or remove captures |
| `vsfleet capture perf --window 7d` | Collect a bounded window of VM performance history; `vsfleet get perfcollections` lists them |
| `vsfleet export rvtools [--run <run>] [--file <file.xlsx>]` | Export a capture as an RVTools-compatible workbook, or CSV with `--format csv`; see [exports](#exports-and-the-database) |
| `vsfleet export rvtools --profile <name> [--pseudonymize --pseudonymize-key-file <file>] [--preview]` | Export a scoped `sizing-summary` or `full-inventory` workbook, optionally pseudonymized; see [scoped sharing profiles](sharing.md) |
| `vsfleet export report [--run <run>]` | Write the `describe capture` report of a capture |
| `vsfleet export metadata [--kind ...] [--source tag\|custom] [--where ...] [--format csv]` | Export stored tags and custom attributes in a fixed long-format schema; see [metadata exports](metadata.md#export-metadata) |
| `vsfleet export metadata --definition <definition> [--base <run>]` | Run a saved tag/custom-attribute report against a capture, optionally listing membership changes since `--base` |
| `vsfleet db backup <file> \| restore <file> \| doctor` | Back up, restore or check the local capture database |
| `vsfleet explain rvtools[.<sheet>]` | Describe every worksheet and column the export writes, or one worksheet |

## Inventory and search

The inventory verbs follow kubectl: `get` lists, `describe` shows one object,
`events` reads a VM's log, `top` reads performance, and `explain` documents a
kind. `vsfleet api-resources` lists every kind, the way
`kubectl api-resources` does. Each kind accepts its singular, plural and short
names (`node` for `host`, `cl` for `cluster`, `ds` for `datastore`, `net` for
`network`) everywhere a kind is taken: in `get`, `describe` and `explain`, in
`--kind`, and as the `KIND` argument of `topology`, `dependencies` and
`blast-radius`. `portgroup` means standard-switch port groups only
(`vsfleet get portgroups`); use `network` for the vCenter network inventory.

vsfleet never changes a vCenter, so kubectl's mutating verbs (`apply`,
`delete`, `edit`, `scale`, `exec` and the rest) fail with a read-only error.

`get` takes the kubectl argument forms: `KIND`, `KIND NAME...`,
`KIND,KIND` and `KIND/NAME...`. `all` stands for every inventory kind (VMs,
templates, hosts, clusters, vApps, datastores, networks, distributed switches
and resource pools), so `get all NAME -A` finds an object without knowing its
kind or vCenter. A name is matched exactly, ignoring case, or as a shell glob
such as `'web*'`; quote a glob so the shell leaves it alone. A VM or template
may also be named by instance or BIOS UUID. An exact name that matches
nothing fails, once however many kinds `all` searched; a glob that matches
nothing prints `No resources found.` on stderr.

vCenter leaves out what an account may not read instead of failing. So listing
networks, distributed switches or their port groups also checks them against
the networks your VMs use, and a VM using one that was not returned prints
`possible permission-hidden inventory` on stderr in place of
`No resources found.`, for live reads and `--run` alike. Any rows still print,
and the exit code stays `0`, as when one of several contexts cannot answer.

```sh
vsfleet api-resources
vsfleet get hosts -n prod
vsfleet get ds 'nvme*' -A
vsfleet get vm/web-01 host/esxi-07
vsfleet get vms -l Environment=Production --field-selector 'cpu>=8' -o wide
vsfleet get datastores --field-selector 'free_percent<15' --sort-by .free_percent

vsfleet get all '*ubuntu*' -A
vsfleet get all -l migration-wave=2 -A -o wide
```

<span id="get-run"></span>

### Reading a capture

`get` and `describe` read live from vCenter. `--run` reads a stored
[capture](assessments.md) instead, offline and without contacting vCenter.
It takes a capture ID, a label, `latest`, `previous`, or a date:
`2026-03-31` picks the newest capture started that day or before, and
`2026-03-31T18:00` the newest started by that minute. A label wins over a
date that reads the same.

`--run` changes where objects come from, never which contexts are read: the
current context, `-n` or `-A` as for a live read, and `latest`, `previous`
and a date pick among the captures of those contexts. Every selector, sort and
output format works the same. A context whose capture did not record a kind,
or failed to, is reported after the rows the way an unreachable vCenter is.

A live read never falls back to stored data. When no vCenter answers, the
error suggests `--run latest`; `-w` does not combine with `--run`, because a
capture never changes. `events` reads only live data, and `top` reads
[performance collections](performance.md), which are stored apart from
captures.

```sh
vsfleet get vms -A --run nightly
vsfleet get hosts,datastores --run 2026-03-31 -n prod -o yaml
vsfleet describe vm web-01 --run pre-migration
vsfleet describe vm web-01 --history
vsfleet describe vm 5029c07a-1b3e-4d2f-9c11-8a7e6f0d4b52 --history --all-observations --include-runtime
```

`--history` shows a VM's change log across every capture: when it appeared,
what changed and when it went. Give it by instance UUID to follow it through
a rename. `--all-observations` includes captures in which nothing changed and
`--include-runtime` includes volatile fields such as power state.

<span id="get-selectors"></span>

### Selecting objects

`-l` selects by vSphere tag, the way kubectl selects by label: the tag
category is the key and the tag's name is the value. Requirements are
comma-separated and all must hold.

| Selector | Matches |
|---|---|
| `env=prod`, `env==prod` | a tag named `prod` in category `env` |
| `env!=prod` | no tag named `prod` in category `env` |
| `'env in (prod,dev)'` | a tag in `env` named `prod` or `dev` |
| `'env notin (prod,dev)'` | no tag in `env` named `prod` or `dev` |
| `env` | any tag in category `env` |
| `'!env'` | no tag in category `env` |

`--field-selector` selects by field: comma-separated predicates in the
`--where` language below, with `==` accepted for `=`. Tag predicates belong
to `-l`; `custom.NAME=VALUE` selects by custom attribute.
`vsfleet explain <kind>` lists a kind's fields. Both selectors apply to
inventory kinds; host subresources, distributed port groups and snapshots
narrow with `--host`, `--switch` and `--vm` instead. An object whose tags or
custom attributes could not be read matches no selector that reads them,
negative or not.

<span id="get-output"></span>

### Output formats

| `-o` | Writes |
|---|---|
| `table` (default) | One table per kind, a blank line apart |
| `wide` | The table plus `TAGS` and `CUSTOM ATTRIBUTES` for kinds that carry them |
| `json`, `yaml` | An array for one kind; with several, an object holding an array per kind, keyed by plural |
| `name` | `KIND/NAME` per object, ready for `vsfleet describe` |
| `csv` | The table's rows and columns, for one kind |
| `jsonpath=TEMPLATE` | A kubectl JSONPath template over `{"kind": "List", "items": [...]}`, or the object itself when one was named |
| `custom-columns=NAME:EXPR,...` | One column per JSONPath expression; `<none>` where it selects nothing |

Without `--sort-by`, objects are listed by name ignoring case, live and from
a capture alike. `--sort-by` takes a JSONPath expression such as `.name` or
`.cpu`: numbers sort numerically, and objects without the field sort last. `--no-headers`
drops the header row of table, wide, csv and custom-columns output.

```sh
vsfleet get vms -l env=prod -o name | xargs -n1 vsfleet describe
vsfleet get vms -o custom-columns=NAME:.name,HOST:.host,TAGS:.metadata.tags[*].name
vsfleet get vms -o jsonpath='{range .items[*]}{.name}{"\t"}{.ip_address}{"\n"}{end}'
vsfleet get datastores -o csv > datastores.csv
```

The JSONPath subset takes `.field`, `['field']`, `[n]`, `[a:b]`, `[*]`,
`{range}...{end}`, quoted literals such as `{"\n"}`, and filters such as
`[?(@.cpu>=8)]`. Recursive descent (`..`) and unions are not supported.

`-w` re-reads and re-renders on the terminal UI's refresh interval,
`--refresh` (default 20s), until interrupted: in place on a terminal, appended
when piped.

<span id="where-language"></span>

`--where` uses `field operator value`, with `=`, `!=`, `<`, `<=`, `>` and
`>=`. Repeat the flag to AND predicates. Values may be quoted when they contain
spaces. Built-in fields are case-insensitive; metadata values are exact and
case-sensitive. Use `tag=Production` for any category,
`tag.Environment=Production` for a category-qualified tag, and
`custom.environment=prod` (or `custom.#123=prod`) for custom attributes.
Numeric comparisons are numeric rather than lexical. Missing or unavailable
metadata never matches, including a negative predicate.

Stored captures are queried offline with the same evaluator:

```sh
vsfleet get vms --run latest --field-selector 'custom.environment=prod' -o wide
vsfleet analyze health -l compliance=PCI -o json
```

JSON includes normalized metadata, source status, and object/context
provenance. Scalar inventory remains usable when metadata is `unavailable`
(read failed), `denied` (permission/session rejected), or `unsupported`
(service absent). Older captures report `not_recorded`; only `available`
is complete. See [metadata exports and saved reports](metadata.md).

Results from healthy contexts remain available when another context fails. The
failure is reported separately with its context and diagnostic information.

## Object detail and infrastructure subresources

`vsfleet describe <kind> <name>` prints identity, provenance, and
configuration. Ambiguous names return candidates; use `-n` to narrow them:

```sh
vsfleet describe vm web-01
vsfleet describe vm/web-01
vsfleet describe vm 5029c07a-1b3e-4d2f-9c11-8a7e6f0d4b52   # by instance UUID
vsfleet describe host esxi-01 -n prod
vsfleet describe datastore nvme-01 -o json
```

Host HBAs, physical NICs, standard switches, port groups, VMkernel adapters,
and multipath LUNs are kinds of their own. `get` lists them across every host,
or `--host` selects one:

```sh
vsfleet get hbas --host esxi-01
vsfleet get vswitches
vsfleet get multipaths --host esxi-01 -o json
```

Distributed port groups and snapshots narrow the same way, with `--switch`
and `--vm`:

```sh
vsfleet get dvswitches
vsfleet get dvportgroups --switch dvs-prod
vsfleet get resourcepools -o wide
vsfleet get snapshots --vm web-01
```

`vsfleet explain <kind>` lists the columns `get` shows for a kind and the
RVTools worksheets the export writes from it; `vsfleet explain <kind>.<field>`
documents one column. `vsfleet cluster-info` shows each context's route,
state, latency and vCenter version, and `vsfleet cluster-info dump` writes
every live kind for the selected contexts as one JSON document for a bug
report.

### VM events

`vsfleet events --for vm/<name>` reads what vCenter logged for one VM, bounded
by what vCenter still keeps, usually 30 days. Routine power, guest, tools and
task events are hidden unless `--all` is given. A VM no longer in inventory is
found through stored history, and its events are read under the ID history
recorded, which shows who deleted it and when.

```sh
vsfleet events --for vm/web-01
vsfleet events --for vm/web-01 --all -o json
```

## Datastore file browsing

`datastore ls` reads one directory lazily; `datastore find` performs a bounded
recursive search and reports incomplete results. Both are read-only:

```sh
vsfleet datastore ls nvme-01
vsfleet datastore ls nvme-01 vm/web-01/
vsfleet datastore find nvme-01 '*.vmdk'
vsfleet datastore find nvme-01 orphan.vmdk --limit 20 -o json
```

Files are not a kind for `get`: a file path does not work as an object name.
`datastore files list` and `datastore files find` print where they went and
exit non-zero.

VMDK results include known VM/template references and confidence from the
latest assessment. Ambiguous datastore names are refused; narrow the context.
See [datastore browsing](tui-storage.md#datastore-file-browser) for relationship
and coverage limits.

## Importing RVTools exports

Import an RVTools-compatible XLSX file as a new capture without
configuration, credentials, or a vCenter connection:

```sh
# Preview what would be imported without writing anything
vsfleet capture import rvtools estate.xlsx --dry-run

# Import it, labelled, with an explicit capture time
vsfleet capture import rvtools estate.xlsx --label pre-migration --captured-at 2026-01-01T00:00:00Z

# Interpret timezone-free capture and snapshot timestamps from a Pacific collector
vsfleet capture import rvtools estate.xlsx --timezone America/Los_Angeles

# Once imported, every stored-evidence command works on it like a live capture
vsfleet capture list
vsfleet describe vm web-01 --history
vsfleet capture diff pre-migration wave-1
```

Imported captures show source `rvtools-import`. Their notes record filename,
SHA-256 fingerprint, capture time/source, parser profile `rvtools-v4`, and
recognized/ignored worksheets.

### What is read

Worksheets match by name and columns by header; extra or reordered columns
are allowed:

| Worksheet | Becomes |
|---|---|
| `vInfo` | VMs and their identity (required) |
| `vCPU`, `vMemory` | VM CPU and memory, when `vInfo` lacks the column; a disagreement with `vInfo` is warned about and `vInfo` wins |
| `vDisk`, `vNetwork`, `vTools`, `vPartition`, `vSnapshot` | per-VM disks, NICs, Tools state, guest filesystems and snapshots |
| `vHost`, `vCluster`, `vDatastore` | the host, cluster and datastore collections |
| `vSwitch`, `vPort`, `vHBA`, `vNIC`, `vSC+VMK` / `vSC_VMK` | host configuration, joined by `Object ID` or a unique host name scoped to the same vCenter in the workbook; a host whose `vNIC` or `vHBA` rows disagree with its `vHost` `# NICs` or `# HBAs` is warned about |
| `vMultiPath` | host multipaths, joined by a unique `Host` name scoped to the same vCenter in the workbook; `Object ID` is not used for the host join |
| `dvSwitch`, `dvPort` | distributed switches and their port groups |
| `vCD`, `vUSB` | per-VM CD-ROM and USB devices |
| `vMetaData` | capture time, when its timestamp can be interpreted |

Dry runs list ignored worksheets and recognized, ignored, and missing
columns. Mapped headers accept vsfleet and RVTools spellings, including
`Shared Bus`/`SharedBus`, `Label`/`SCSI label`, `Unit #`/`Unit number`,
`Raw Comp. Mode`/`Raw Compatibility Mode`, `Switch`/`DVS`, `Max Ports`/`# Max ports`,
`Port`/`Port group`, `Allow Promiscuous`/`Promiscuous mode`,
`Policy`/`Teaming policy`, and `Max MTU`/`MTU`. `Rolling Order` maps to `Failback`.

RVTools has no host thread count or port-group uplink flag, so both are
derived. A host's threads are `# Cores`, doubled when `HT Active` is true, and
unknown without that column. A port group without vsfleet's `Uplink` column is
an uplink when it keeps the name vCenter gives one, `<switch>-DVUplinks-<n>`;
a renamed uplink port group imports as a VM port group.

Number cells are read with RVTools' thousands separators, so `9,984` is
9984. Host CPU and memory usage come from `vHost`'s `CPU usage %` and
`Memory usage %` applied to the host's capacity. RVTools writes whole
percentages, so imported usage is accurate to about one percent.

In `vMultiPath`, RVTools' `Object ID` identifies the datastore. Hosts join
through `vHost` using `Host` and `VI SDK UUID`, narrowed by `Datacenter`
and `Cluster` if supplied. Ambiguous/missing hosts are warned and skipped.
This also applies to vsfleet exports. RVTools lists datastore-backed disks;
vsfleet exports every collected host/LUN, so counts may differ.

The LUN label uses `Display name`, falling back to `Disk`; `Disk` remains
the device ID. RVTools path counts cover only supplied `Path 1`–`Path 8`
slots and states. Locality and working-path count remain unknown. vsfleet's
own aggregate counts, locality, and working-path columns are preserved.

`vTools` installation status (`Tools`) and running status (`Tools Running Status`,
a vsfleet extension) are stored separately. Real RVTools workbooks supply neither
`Tools Running Status` nor `Tools Version Status`, so those values come from
`Tools` only where it implies them: `toolsNotInstalled` means not installed and
not running, and `toolsNotRunning` means not running. `toolsOk` and `toolsOld`
leave running status unknown, so `tools-not-running` reports unknown for those
VMs instead of passing. Older vsfleet
workbooks that put `guestToolsRunning` or `guestToolsNotRunning` in `Tools`
retain those values as running status.

### Missing evidence is never good news

Missing workbook evidence reduces confidence; it cannot improve a verdict.

- Absent worksheets are `unavailable`. Present worksheets with no rows are
  empty only when required columns exist: `vHost` needs `# Cores`,
  `# Memory`, `Speed`; `vCluster` needs `NumCpuCores`, `TotalCpu`,
  `TotalMemory`; `vDatastore` needs `Capacity MiB`, `Free MiB`.
- Missing VM `CPUs`, `Memory`, or `In Use MiB` is a coverage gap, never zero.
- Unsupported health evidence yields unknown/not-evaluated. Imports claim
  schema `2` by default, `3` with `vTools`, `4` with `vPartition`, and
  `5` with its `Disk Key` column.
- Resource pools and networks are always unavailable: `vRP` lacks VM
  membership, and networks lack managed-object IDs.
- Host devices require an ID or unique same-workbook host name in the same
  vCenter UUID. Ambiguous joins are skipped and reported.
- Without `VI SDK UUID`, the endpoint is the reported vCenter identity.

### Identity

VM matching uses managed-object ID, instance UUID, BIOS UUID, then display
name. Same-named VMs across vCenters stay distinct; renames preserve history
when stronger identity exists. Duplicate identities retain both rows but prevent
dependent rows from joining either. Repeated host, cluster, datastore, or
distributed-switch `Object ID` makes that context's collection unavailable.

Host networking joins by ID or a unique same-workbook host name/vCenter UUID.
Port groups join by context, datacenter, and switch name only when unique.

`--context-map KEY=NAME` renames a reconstructed context without touching
`config.toml`; an imported run's contexts are independent of it.

### Trends and partial runs

Imports are always `partial` because resource pools and networks are
unavailable. Diff, VM history, findings, and topology use them with coverage
warnings. Trends require `--include-partial`:

```sh
vsfleet analyze snapshots --trend --include-partial
vsfleet analyze capacity --trend --include-partial
```

### Capture time

Capture time comes from `--captured-at`, then usable `vMetaData`, workbook
properties, or import time. `--timezone` accepts an IANA zone for timezone-free
`vMetaData` and `vSnapshot` values; timezone-free metadata is skipped without
it. Stored cell values preserve seconds and honor the 1900/1904 date system.

History, diff, and trends use capture time. The run notes its source and import
time. Filenames are never parsed for dates.

### Repeating an import

Repeated fingerprints warn on stderr with the previous run ID but still
create a new run. `--allow-duplicate` silences the warning.

### Safety

Workbooks are size-limited before opening, with bounded decompression and row
counts. Formulas are never evaluated and external links never followed. Failed
imports leave no run.

<span id="analyze"></span>

## Analysis

`vsfleet analyze` answers questions from stored captures without contacting
vCenter. Every subcommand reads the capture `--run` names, the most recent by
default; `--run` takes an ID, a label, `latest`, `previous` or a date, as in
[`get`](#get-run). A subject is `KIND/NAME` or `KIND NAME`.

An analysis covers every context the capture recorded unless `-n` narrows
it. That differs from `get --run`, which reads the current context: analyses
follow objects across vCenters, as when a migration's source cluster is on
one and its target on another.

| Command | Answers |
|---|---|
| `health` | Which health rules fail, and on what; see [health](health.md) |
| `readiness` | Whether the estate is ready to migrate: `ready`, `blocked` or `unknown` |
| `readiness network SOURCE TARGET` | Whether two clusters' networks are ready, with the comparison under `-o wide` |
| `sizing` | Whether the scoped VMs fit a proposed destination |
| `capacity` | Which VMs drive datastore growth, and when each datastore crosses its floor |
| `orphans` | Which browsed VMDK files no VM claims |
| `churn` | How the VM population changed across captures |
| `snapshots` | How old each snapshot is |
| `topology`, `deps`, `blast-radius` | Where a subject sits, what it needs, and what a failure of it would reach |
| `decommission vm/NAME` | Whether anything argues against retiring a VM |

`churn`, `capacity --trend` and `snapshots --trend` read a window of captures:
from `--from`, or the start of history, up to `--run`, or the most recent,
capped at `--limit` complete captures. `health`, `readiness`, `readiness
network`, `capacity`, `orphans` and `decommission` exit `2` when their
`--fail-on-*` gate trips.

```sh
vsfleet analyze health --severity warning -o wide
vsfleet analyze readiness --run pre-migration --fail-on-blockers
vsfleet analyze readiness network cl/cluster-prod cl/cluster-dr -o json | jq '.differences, .mapping_gaps'
vsfleet analyze topology vm/app01
vsfleet analyze deps vm/app01 --depth 2
vsfleet analyze blast-radius datastore/ds-prod-01 -n prod
vsfleet analyze decommission vm/legacy-db01 --fail-on-blockers -o json
vsfleet analyze churn --from 2026-01-01
```

`topology` shows containment and attachments; `deps` follows what a subject
uses; `blast-radius` follows those edges backwards. Use `-n` to narrow an
ambiguous name. Results include confidence, provenance, unresolved
references, and coverage gaps. Older reconstructed network evidence is
partial. See [planning](planning.md) for readiness, decommission and
network joins, blockers and confidence rules.

`decommission` returns `no-blockers`, `blocked`, or `unknown`. It performs no
VM action; `no-blockers` is not permission to delete. Ownership, backup
policy, and application dependencies are `not_assessed`. See
[decommission review](planning.md#vm-decommission-review) for the gates.

For diagnostics, `doctor` walks the connection to a vCenter stage by stage,
`cluster-info` summarizes every context's connection, `db doctor` checks the
capture database, and `analyze health` checks stored estate evidence.

## Export compatibility

<span id="compatibility-report"></span>

`vsfleet explain rvtools` describes what the `rvtools` export profile writes:
every worksheet, every column, each cell's type and unit, and when a cell is
left empty.

```sh
vsfleet explain rvtools                 # every worksheet
vsfleet explain rvtools.vHBA            # one of them
vsfleet explain rvtools -o json | jq    # for a pipeline
vsfleet explain vm.annotation           # one column, on every VM worksheet
```

The report is generated from exporter definitions and runs without
configuration, keyrings, or vCenter. It describes vsfleet output and observed
differences from RVTools, not the complete RVTools schema. See
[interoperability limits](exports.md#rvtools-file-interoperability).

## Captures

A capture is a read-only snapshot of the estate, stored in the local history
database and read back offline. `vsfleet capture` takes, imports, lists,
labels, compares and removes them. Inside the group the capture is the object
acted on, so it is an argument (`capture delete 42`); every other command
picks one with `--run`.

| Command | Does |
| --- | --- |
| `capture run` | Read the selected contexts (`-n`, `-A`) and store one capture |
| `capture import rvtools FILE` | Store an RVTools export as a capture, offline |
| `capture list`, `get captures` | List captures, newest first |
| `describe capture RUN` | Show what one capture recorded and where it has gaps |
| `capture update RUN --label/--note/--pin/--unpin` | Change a capture's label, note or pin |
| `capture delete RUN --force` | Delete one capture |
| `capture prune --older-than 90d [--execute]` | Delete old captures and old performance collections; a dry run without `--execute` |
| `capture diff [BASE] [TARGET]` | Compare two captures; `--fail-on` and `--require-complete` gate a pipeline |
| `capture perf --window 7d` | Collect VM performance history from vCenter |
| `get perfcollections` | List stored performance collections |

`RUN` is an ID or label from `capture list`, `latest`, `previous`, or a date
(`2026-03-31`, or `2026-03-31T18:00`) for the newest capture started by then.
Performance collections are stored apart from captures: `capture list`,
`capture delete` and `--run` do not see them, `top vm` reads them, and
`capture prune` removes old ones. Only `capture run` and `capture perf`
contact a vCenter.

The commands these replaced (`assessment run`, `list`, `diff`, `update`,
`delete`, `prune`, `report` and `perf`, and `import rvtools`) print where they
went and exit non-zero.

Browsing datastores needs `Datastore.Browse`, adds collection time, and is
disabled by default:

```sh
vsfleet capture run --all-contexts --browse-datastores
vsfleet analyze health
vsfleet analyze orphans
vsfleet analyze capacity
```

Collection options are independent:

| Flag | Evidence | Extra privilege |
| --- | --- | --- |
| `--browse-datastores` | Bounded VM disk-file checks for zombie/orphan VMDKs | `Datastore.Browse` |
| `--datastore-file-inventory` | File names/sizes for `vFileInfo`; exports paths | `Datastore.Browse` |
| `--include-licenses` | Products, usage, expiration, assignments; keys excluded | `Global.Licenses` |

File inventory limits are `--file-inventory-max-files`,
`--file-inventory-max-total-files`, and `--file-inventory-timeout`. Limit
flags do not enable collection. See [export limits/privacy](exports.md#datastore-file-inventory-vfileinfo)
and [licenses](licensing.md).

Without browsing, zombie-VMDK health is `not-evaluated`. Orphan reports expose
coverage in JSON and name unbrowsed, failed, denied, or truncated datastores on
stderr. Use `--confidence`, `--min-size`, and `--fail-on-unknown`; see
[health and orphan checks](health.md) for confidence and exit semantics.

Capacity reports accept `--since 30d`, `--datastore`, `--top`, and
`--min-free-bytes`. The larger of the absolute and percentage free-space floors
binds. See [capacity attribution](assessments.md#capacity-attribution-and-projection)
for exact/inferred/split contributions and unknown projections.

## Exports and the database

`vsfleet export` writes a stored capture out; `vsfleet db` maintains the
database that holds the captures. Neither contacts a vCenter.

| Command | Does |
| --- | --- |
| `export rvtools` | Write an RVTools-compatible workbook, or one CSV per tab with `--format csv --file DIR`; see [exports](exports.md) |
| `export report` | Write the report `describe capture` shows; `-o json` writes it as JSON |
| `export metadata` | Write stored tags and custom attributes; see [metadata exports](metadata.md) |
| `export metadata --definition FILE` | Run a saved metadata report; `--base RUN` lists membership changes |
| `db backup FILE` | Write a consistent copy of the database |
| `db restore FILE --force` | Replace the database from a backup, keeping a safety copy |
| `db doctor` | Check schema, integrity and foreign keys; exits 1 on a warning |

Every export reads the capture `--run` names (`latest` by default), narrows
to contexts with `-n`, and writes to stdout unless `--file` is given. An
RVTools workbook is never written to a terminal, and its receipt goes to
stderr so a pipe carries only the workbook. `--format csv` writes several
files, so it needs `--file` naming a directory.

The database is `--db`, else `VSFLEET_DB`, else
`<user-config-dir>/vsfleet/history.db`. For one release vsfleet still reads
`VSFLEET_HISTORY_DB` when `VSFLEET_DB` is unset, with a warning on stderr;
rename it. `--history-db` is refused with a pointer to `--db`.

The commands these replaced (`assessment export`, `metadata`,
`metadata-report`, `backup`, `restore` and `doctor`) print where they went
and exit non-zero. The `assessment` group is gone.

## Exit codes

Use exit codes to gate scheduled jobs:

| Code | Meaning |
|---|---|
| `0` | The command did what was asked |
| `1` | vsfleet could not do its job: bad configuration, an unreachable estate, an unreadable database |
| `2` | The tool worked and the estate did not pass: `capture diff` policy violations, or an `analyze` gate such as `--fail-on-findings`, `--fail-on-blockers` or `--fail-on-projection` |
| `3` | A capture stored evidence from some contexts but not all; only with `capture run --fail-on-partial` |

Partial captures exit `0` unless `--fail-on-partial` is set.

## JSON output

Use `-o json` for automation:

```sh
vsfleet get vms --all-contexts -o json
vsfleet get datastores 'nvme*' -A -o json | jq
```

## Persistent flags

| Option | Description |
|---|---|
| `-n, --context <name>` | Scope a command to one or more named contexts; repeatable |
| `-A, --all-contexts` | Target every configured context |
| `--config <path>` | Override the TOML configuration path |
| `--db <path>` | Override the SQLite capture database (default `VSFLEET_DB`, then the user config directory) |
| `--timeout <duration>` | Per-vCenter request timeout (default `30s`) |
| `-o, --output table\|json` | Select human or machine-readable output |
| `--refresh <duration>` | Set or disable background TUI polling |

## Finding your way around

Use `-h` / `--help` at any level for the installed binary's complete command
reference and examples:

```sh
vsfleet --help                          # grouped by the job being done
vsfleet capture --help                  # what is under a command group
vsfleet capture diff --help             # flags, arguments, and examples
vsfleet analyze readiness network -h    # four levels deep, same thing
```

`vsfleet --help` groups the tree rather than listing it alphabetically:
getting started, inventory and search, captures and history, analysis, and
diagnostics.

Unknown commands exit non-zero and suggest close matches:

```console
$ vsfleet capture lst
vsfleet: unknown command "lst" for "vsfleet capture"

Did you mean this?
	list

Run 'vsfleet capture --help' for the available commands.
```

Shell completion covers commands, flags, and the closed argument vocabularies
such as the `KIND` accepted by `topology`, `dependencies`, and `blast-radius`:

```sh
vsfleet completion bash --help   # and zsh, fish, powershell
```
