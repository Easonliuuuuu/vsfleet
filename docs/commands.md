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
| `vsfleet assessment findings [run]` | Show structured health findings for a stored assessment |
| `vsfleet assessment orphans [run]` | Explain estate-wide browsed VMDK orphan evidence |
| `vsfleet assessment readiness [run]` | Return a migration-readiness verdict |
| `vsfleet search <text>` | Search every vCenter at once |
| `vsfleet <kind> list` | List VMs, templates, hosts, clusters, vApps, datastores, or networks |
| `vsfleet vm history <name-or-uuid>` | Show a VM's stored assessment timeline |
| `vsfleet assessment ...` | Capture and compare historical observations |
| `vsfleet compatibility report` | Describe every worksheet and column the export writes |

## Inventory and search

Supported resource kinds are `vm`, `template`, `host`, `cluster`, `vapp`,
`datastore`, and `network`. Inventory commands accept `--filter` / `-f`:

```sh
vsfleet host list --context prod --filter esxi-07
vsfleet datastore list --all-contexts -f nvme
vsfleet vapp list --all-contexts

vsfleet search ubuntu --all-contexts
vsfleet search nvme --kind datastore --limit 20
```

Results from healthy contexts remain available when another context fails. The
failure is reported separately with its context and diagnostic information.

`vsfleet doctor` diagnoses whether vsfleet can reach a vCenter, while
`vsfleet assessment doctor` checks the local history database. `vsfleet health`
and `vsfleet assessment findings` assess the estate described by stored
evidence. `assessment readiness` reports `unknown`, never `ready`, when a
required collection is blind.

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
```

Without the flag, `datastore-zombie-vmdk` is reported as `not-evaluated`, not as
a clean result. Orphan confidence is estate-aware: `--fail-on-findings
--severity warning` fails only on verified-unreferenced VMDKs; suspected and
cross-context evidence is informational, while incomplete coverage is unknown.
Use `assessment orphans [RUN]` for the evidence drill-down, with
`--confidence` and `--min-size` filters or `-o json` for automation.

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

The built-in help is the exhaustive flag-level reference and always reflects
the installed binary:

```sh
vsfleet --help
vsfleet context add --help
vsfleet assessment diff --help
```
