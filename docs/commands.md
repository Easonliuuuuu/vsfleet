# CLI Guide

The bare `vsfleet` command opens the terminal UI. Add a subcommand for
scriptable operations. Every command accepts the persistent context, timeout,
configuration, history database, and output options shown below.

## Common commands

| Command | Purpose |
|---|---|
| `vsfleet` / `vsfleet ui` | Open the terminal UI |
| `vsfleet demo` | Open the terminal UI on sample data, with no vCenter and nothing written |
| `vsfleet context add` | Add a vCenter context via the wizard or flags |
| `vsfleet context list` | List configured contexts |
| `vsfleet context show [name]` | Show endpoint, route, and TLS settings |
| `vsfleet context use <name>` | Select the current context |
| `vsfleet context test <name>` | Test connectivity and authentication |
| `vsfleet context remove <name>` | Remove a context and invalidate its session |
| `vsfleet status` | Check selected contexts |
| `vsfleet doctor [context...]` | Diagnose the connection stage by stage |
| `vsfleet health [run]` | Assess the estate described by a stored assessment |
| `vsfleet topology <kind> <name> [run]` | Show containment and attachments for a stored subject |
| `vsfleet dependencies <kind> <name> [run]` | Show what a stored subject depends on |
| `vsfleet blast-radius <kind> <name> [run]` | Show stored subjects affected by a dependency |
| `vsfleet assessment findings [run]` | Show structured health findings for a stored assessment |
| `vsfleet assessment inventory [run]` | Query inventory stored in an assessment |
| `vsfleet assessment orphans [run]` | Explain estate-wide browsed VMDK orphan evidence |
| `vsfleet assessment capacity [run]` | Attribute datastore growth and project free-space thresholds |
| `vsfleet assessment readiness [run]` | Return a migration-readiness verdict |
| `vsfleet network compare <source-cluster> <target-cluster> [run]` | Compare stored network reachability and policy between clusters |
| `vsfleet assessment network-readiness --source <cluster> --target <cluster> [run]` | Return a cross-cluster network-readiness verdict |
| `vsfleet search <text>` | Search every vCenter at once |
| `vsfleet <kind> list` | List VMs, templates, hosts, clusters, vApps, datastores, or networks |
| `vsfleet datastore files list <datastore> [path]` | List one datastore directory |
| `vsfleet datastore files find <datastore> <pattern>` | Recursively search a datastore for a name or pattern |
| `vsfleet vm history <name-or-uuid>` | Show a VM's stored assessment timeline |
| `vsfleet vm decommission-check <name-or-uuid> [run]` | Review stored evidence before decommissioning a VM |
| `vsfleet assessment ...` | Capture and compare historical observations |
| `vsfleet import rvtools <file.xlsx>` | Import an RVTools-compatible export as a new offline assessment run |
| `vsfleet compatibility report` | Describe every worksheet and column the export writes |

## Inventory and search

Supported resource kinds are `vm`, `template`, `host`, `cluster`, `vapp`,
`datastore`, and `network`. Inventory commands accept `--filter` / `-f`,
repeatable `--where`, and opt-in `--wide` metadata columns:

```sh
vsfleet host list --context prod --filter esxi-07
vsfleet datastore list --all-contexts -f nvme
vsfleet vapp list --all-contexts
vsfleet vm list --where 'tag=Production' --where 'cpu>=8' --wide
vsfleet datastore list --where 'free_percent<15'

vsfleet search ubuntu --all-contexts
vsfleet search --tag migration-wave-2 --wide
vsfleet search nvme --kind datastore --limit 20
```

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
vsfleet assessment inventory latest --where 'custom.environment=prod' --wide
vsfleet assessment findings latest --where 'tag=PCI' -o json
```

JSON inventory, search, and findings output includes normalized metadata,
source status, and object/context provenance. A vCenter that does not expose a
metadata source remains usable; scalar inventory is retained and the affected
source is reported as unavailable.

Results from healthy contexts remain available when another context fails. The
failure is reported separately with its context and diagnostic information.

## Datastore file browsing

`vsfleet datastore files` exposes the same read-only datastore browser and
bounded recursive search the terminal UI offers. Listing is lazy and
non-recursive — one query per directory — while `find` searches the whole
datastore and reports when it stops short of a complete answer:

```sh
vsfleet datastore files list nvme-01
vsfleet datastore files list nvme-01 vm/web-01/
vsfleet datastore files find nvme-01 '*.vmdk'
vsfleet datastore files find nvme-01 orphan.vmdk --limit 20 -o json
```

A VMDK result includes relationship evidence where it is known: which VM or
template currently references it, and the confidence recorded by the most
recent stored assessment. A datastore name that matches more than one
datastore — across contexts, or, rarely, within one context spanning several
datacenters — is refused rather than guessed at; narrow it with `--context`.

## Importing RVTools exports

`vsfleet import rvtools` adapts an RVTools-compatible XLSX export into a new
stored assessment run, entirely offline — no configuration, credentials, or
vCenter connection is used:

```sh
# Preview what would be imported without writing anything
vsfleet import rvtools estate.xlsx --dry-run

# Import it, labelled, with an explicit capture time
vsfleet import rvtools estate.xlsx --label pre-migration --captured-at 2026-01-01T00:00:00Z

# Once imported, every stored-evidence command works on it like a live capture
vsfleet assessment list
vsfleet vm history web-01
vsfleet assessment diff pre-migration wave-1
```

This first profile reads `vInfo`, `vCPU`, `vMemory`, `vDisk`, `vNetwork`,
`vHost`, `vCluster` and `vDatastore` by column name, not position, so a
workbook with extra or reordered columns still imports; anything else is
reported as an ignored worksheet or column rather than silently dropped.
RVTools has no standalone worksheet for resource pools, distributed switches,
or networks, so those three collections are always recorded as not
collected — visible in `vsfleet assessment list` as a `partial` run and in
`vsfleet assessment findings` as rules not evaluated, the same honest gap a
live capture records for a denied query. A field the workbook does not carry
is left absent, never defaulted to a value that would read as confirmed
evidence; an imported run's `assessment list` row shows `rvtools-import` as
its source, so it is never mistaken for a live capture.

## Topology and dependencies

Topology queries use the immutable assessment ledger and never contact a
vCenter. They join managed-object references, UUIDs, datastore backing
identity, distributed port-group keys, and context-scoped names across the
estate:

```sh
vsfleet topology vm app01 latest
vsfleet dependencies vm app01 --depth 2
vsfleet dependencies network prod-vlan-210 -o json | jq
vsfleet blast-radius datastore ds-prod-01 --all-contexts
vsfleet network compare cluster-prod cluster-dr -o json | jq '.differences, .mapping_gaps'
vsfleet assessment network-readiness --source cluster-prod --target cluster-dr --fail-on-blockers
```

`topology` shows containment and direct attachments. `dependencies` follows
objects a subject uses; `blast-radius` follows that relationship backwards.
Use `--context` when a name is ambiguous. Each result reports `complete`,
`partial`, or `unknown`, the checked contexts, the evidence basis for every
edge, unresolved references, and any collection that was blind. Strong joins
are confirmed; context-scoped display-name joins are inferred. Network
relationships reconstructed from runs captured before inventory schema 12 are
always partial.

`vsfleet doctor` diagnoses whether vsfleet can reach a vCenter, while
`vsfleet assessment doctor` checks the local history database. `vsfleet health`
and `vsfleet assessment findings` assess the estate described by stored
evidence. `assessment readiness` reports `unknown`, never `ready`, when a
required collection is blind.

`network compare` and `assessment network-readiness` compare the networks
reachable from two clusters using the same stored assessment ledger. Networks
match by VLAN first and by name when VLAN evidence is unavailable. The result
separates matched networks, source-only mapping gaps (including attached VMs),
target-only networks, and field-level VLAN, MTU, teaming, security, uplink, and
host-coverage differences. `network-readiness` reports `ready`, `blocked`, or
`unknown`; a missing network used by a VM or a hard policy/coverage mismatch is
a blocker. Blind collection evidence always produces `unknown`, and older or
partially reconstructable network evidence is marked with reduced confidence.

### VM decommission checks

`vsfleet vm decommission-check NAME_OR_UUID [RUN]` is an offline, read-only
review of one VM's stored assessment evidence. It never powers off, deletes,
unregisters, or otherwise changes a VM. The command searches every selected
context, joins observations by VM UUID when available, and returns every
distinct match when a name is ambiguous; use `--context` to narrow it.

The verdict is `no-blockers`, `blocked`, or `unknown` (schema version 2; earlier
releases used `ready` for `no-blockers`). `no-blockers` means every collected
technical gate passed; it is not authorization to delete the VM. Powered-on or
suspended VMs, snapshots, connected CD-ROM/USB devices, and inaccessible,
orphaned, disconnected, or invalid connection states are blockers. Missing
power, connection, hardware, dependency, or collection evidence is unknown.
Disk backing paths, datastore dependencies, network relationships, unresolved
references, and blind contexts are included as evidence. Ownership, backup
policy, and application-dependency evidence are reported as `not_assessed`
advisories because those fields are not part of the stored inventory yet; the
verdict never accounts for them.

The default run is `latest`; pass an explicit run ID or label for a reproducible
offline result. JSON is intended for automation, and `--fail-on-blockers`
returns exit code `2` only when a blocker is present:

```sh
vsfleet vm decommission-check legacy-db01 latest
vsfleet vm decommission-check legacy-db01 --context prod -o json
vsfleet vm decommission-check legacy-db01 --fail-on-blockers
```

## Export compatibility

`vsfleet compatibility report` describes what the `rvtools` export profile
writes: every worksheet, every column, each cell's type and unit, and when a
cell is left empty.

```sh
vsfleet compatibility report                      # every worksheet
vsfleet compatibility report --sheet vHBA         # one of them
vsfleet compatibility report -o json | jq         # for a pipeline
```

It reads no configuration, opens no keyring and contacts no vCenter, so it can
be used to decide whether an export fits a downstream pipeline before setting
vsfleet up at all. It is generated from the same definitions the exporter
writes from, so it cannot drift from the workbook.

It describes what vsfleet emits. It makes no claim about any other tool's
schema — compare it against what your pipeline requires.

## Assessments

Assessment captures are read-only and store their evidence locally. Add
`--browse-datastores` when the account has the vSphere `Datastore.Browse`
privilege and you want zombie-VMDK checks; the bounded file listing adds time on
large estates and is disabled by default.

```sh
vsfleet assessment run --all-contexts --browse-datastores
vsfleet health latest
vsfleet assessment orphans latest
vsfleet assessment capacity latest
```

Without the flag, `datastore-zombie-vmdk` is reported as `not-evaluated`, not as
a clean result. Orphan confidence is estate-aware: `--fail-on-findings
--severity warning` fails only on verified-unreferenced VMDKs; suspected and
cross-context evidence is informational, while incomplete coverage is unknown.
Use `assessment orphans [RUN]` for the evidence drill-down, with
`--confidence` and `--min-size` filters or `-o json` for automation. When a
datastore was not browsed (no `--browse-datastores`), failed, was denied, or was
truncated, the command prints `NOT EVALUATED` instead of a clean result, names
the datastores on stderr, and reports the same state under `coverage` in the
JSON output; `--fail-on-unknown` turns incomplete coverage into a non-zero exit.

`vsfleet assessment capacity [RUN]` is an offline growth drill-down. It joins
the first and last usable datastore observations in the selected window, names
VM, template, and file contributors, and projects when the configured free-space
floor will be crossed. `--min-free-bytes` adds an absolute floor to the default
percentage floor; the larger floor binds. Use `--since 30d`, `--datastore`, and
`--top` to narrow the report. Exact attribution requires complete datastore
browse listings in both endpoint runs; inferred attribution uses a
single-datastore VM's committed-storage change, and split attribution uses
per-disk provisioned capacity for VMs spanning datastores. A synthetic
`unattributed` contributor keeps the reported contributors equal to used-byte
growth. Projections are `unknown` when coverage or history is insufficient.

## Exit codes

A scheduled collection reacts to the exit code, so it is a contract rather than
an implementation detail:

| Code | Meaning |
|---|---|
| `0` | The command did what was asked |
| `1` | vsfleet could not do its job: bad configuration, an unreachable estate, an unreadable database |
| `2` | The tool worked and the estate did not pass: `assessment diff` policy violations, `health --fail-on-findings`, or `assessment readiness --fail-on-blockers` |
| `3` | A capture stored evidence from some contexts but not all — only with `assessment run --fail-on-partial` |

A partial capture exits `0` unless `--fail-on-partial` is passed, so upgrading
does not change what an existing scheduled job does.

## JSON output

Use `-o json` for automation. The output is designed for `jq`, `awk`, and other
pipeline consumers:

```sh
vsfleet vm list --all-contexts -o json
vsfleet search nvme --kind datastore -o json | jq
```

## Persistent flags

| Option | Description |
|---|---|
| `--context <name>` | Scope a command to one or more named contexts |
| `--all-contexts` | Target every configured context |
| `--config <path>` | Override the TOML configuration path |
| `--history-db <path>` | Override the SQLite assessment database |
| `--timeout <duration>` | Per-vCenter request timeout (default `30s`) |
| `-o, --output table\|json` | Select human or machine-readable output |
| `--refresh <duration>` | Set or disable background TUI polling |

## Finding your way around

The built-in help is the exhaustive reference and always reflects the
installed binary. `-h` / `--help` works at every level of the tree, and every
command carries worked examples:

```sh
vsfleet --help                          # grouped by the job being done
vsfleet assessment --help               # what is under a command group
vsfleet assessment diff --help          # flags, arguments, and examples
vsfleet assessment trends capacity -h   # four levels deep, same thing
```

`vsfleet --help` groups the tree rather than listing it alphabetically:
getting started, inventory and search, assessment and history, analysis, and
diagnostics.

A mistyped command fails with a non-zero exit status and suggests the near
miss, so a typo in a scheduled job is never mistaken for success:

```console
$ vsfleet assessment lst
vsfleet: unknown command "lst" for "vsfleet assessment"

Did you mean this?
	list

Run 'vsfleet assessment --help' for the available commands.
```

Shell completion covers commands, flags, and the closed argument vocabularies
such as the `KIND` accepted by `topology`, `dependencies`, and `blast-radius`:

```sh
vsfleet completion bash --help   # and zsh, fish, powershell
```
