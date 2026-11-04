# CLI reference

<span id="cli-guide"></span>

Run `vsfleet` to open the terminal UI, or add a subcommand for scripts.
`vsfleet <command> --help` gives every flag and example for the installed
version.

A context is one vCenter, and it plays the part of a kubectl namespace:
commands act on the current context, `-n NAME` picks others, and `-A` spans
every context. vsfleet never changes a vCenter, so kubectl's mutating verbs
(`apply`, `delete`, `edit`, …) fail with a read-only error.

## Contexts and diagnostics

| Command | Does |
| --- | --- |
| `vsfleet` / `vsfleet ui` | Open the terminal UI |
| `vsfleet demo` | Open the UI on sample data, with no vCenter and nothing written |
| `config set-context [name]` | Add or change a context, by wizard or flags |
| `config get-contexts`, `current-context`, `use-context <name>` | List, show, or select contexts |
| `config view [--minify]` | Show the configuration; credentials appear as references |
| `config delete-context <name>` | Delete a context |
| `doctor [context...]` | Diagnose a connection stage by stage; see [troubleshooting](troubleshooting.md) |
| `cluster-info [dump]` | Show each context's route, state, latency and version; `dump` writes every live kind as JSON for a bug report |
| `version` | Print the version |

`ctx` is an alias of `config`.

## Inventory and search

| Command | Does |
| --- | --- |
| `get <kind>[,<kind>...] [name...]` | List objects, live or from a capture with `--run` |
| `describe <kind> <name>` | Show one object in full |
| `describe vm <name-or-uuid> --history` | Show how a VM changed across every capture |
| `events --for vm/<name> [--all]` | Show a VM's live vCenter event log |
| `top vm [name...]` | Show VM CPU and memory from [performance collections](performance.md) |
| `explain <kind>[.<field>]` | Document a kind's columns and the RVTools worksheets it feeds |
| `api-resources` | List every kind, its short names, and whether it is live, stored, or both |

Kinds accept singular, plural and short names (`node` for `host`, `cl`,
`ds`, `net`). `all` means every inventory kind, so `get all NAME -A` finds an
object without knowing its kind or vCenter. Names match exactly, ignoring
case, or as a quoted glob such as `'web*'`; a VM may also be named by UUID.
Host HBAs, NICs, switches, port groups, VMkernel adapters and multipaths are
kinds of their own: narrow them with `--host`, distributed port groups with
`--switch`, and snapshots with `--vm`.

```sh
vsfleet get hosts -n prod
vsfleet get ds 'nvme*' -A
vsfleet get vm/web-01 host/esxi-07
vsfleet get vms -l Environment=Production --field-selector 'cpu>=8' -o wide
vsfleet get all '*ubuntu*' -A
vsfleet get hbas --host esxi-01
vsfleet describe vm web-01
```

An exact name that matches nothing fails; a glob that matches nothing prints
`No resources found.` vCenter hides what an account may not read instead of
failing, so when a listing contradicts other evidence, such as VMs on a
network that is not listed, vsfleet prints `possible permission-hidden
inventory` instead.

When one context fails, the others' results still print and the failure is
reported separately.

<span id="get-run"></span>

### Reading a capture

`--run` reads a stored [capture](assessments.md) instead of vCenter,
offline. It takes a capture ID, a label, `latest`, `previous`, or a date:
`2026-03-31` is the newest capture started that day or before,
`2026-03-31T18:00` the newest by that minute. Selectors, sorting and output
work the same.

```sh
vsfleet get vms -A --run nightly
vsfleet get hosts,datastores --run 2026-03-31 -n prod -o yaml
vsfleet describe vm web-01 --run pre-migration
```

A live read never falls back to stored data; when no vCenter answers, the
error suggests `--run latest`. `--history` shows when a VM appeared, what
changed, and when it went; give a UUID to follow it through a rename.
`--all-observations` adds captures with no change and `--include-runtime`
adds volatile fields such as power state.

<span id="get-selectors"></span>

### Selecting objects

`-l` selects by vSphere tag: the category is the key and the tag the value.
Requirements are comma-separated and all must hold.

| Selector | Matches |
|---|---|
| `env=prod` | a tag `prod` in category `env` |
| `env!=prod` | no tag `prod` in `env` |
| `'env in (prod,dev)'`, `'env notin (prod,dev)'` | a tag in `env` that is, or is not, one of these |
| `env`, `'!env'` | any tag, or no tag, in `env` |

<span id="where-language"></span>

`--field-selector` and `--where` take `field operator value` with `=`, `!=`,
`<`, `<=`, `>`, `>=`; numbers compare numerically. `vsfleet explain <kind>`
lists the fields. Use `tag.Environment=Production` for a tag and
`custom.environment=prod` for a custom attribute. An object whose tags or
attributes could not be read matches no selector that reads them, negative or
not. See [metadata](metadata.md) for how metadata coverage is reported.

<span id="get-output"></span>

### Output formats

| `-o` | Writes |
|---|---|
| `table` (default), `wide` | One table per kind; `wide` adds tags and custom attributes |
| `json`, `yaml` | An array for one kind, an object of arrays for several |
| `name` | `KIND/NAME` per object, ready for `describe` |
| `csv` | The table, for one kind |
| `jsonpath=TEMPLATE` | A kubectl JSONPath template |
| `custom-columns=NAME:EXPR,...` | One column per JSONPath expression |

`--sort-by .field` sorts, numbers numerically; the default is by name.
`--no-headers` drops the header row. `-w` re-renders every `--refresh`
(default 20s) until interrupted. JSONPath supports filters such as
`[?(@.cpu>=8)]` but not `..` or unions.

```sh
vsfleet get vms -l env=prod -o name | xargs -n1 vsfleet describe
vsfleet get vms -o custom-columns=NAME:.name,HOST:.host
vsfleet get vms -o jsonpath='{range .items[*]}{.name}{"\t"}{.ip_address}{"\n"}{end}'
vsfleet get datastores --field-selector 'free_percent<15' --sort-by .free_percent
```

### VM events

`events --for vm/<name>` reads what vCenter logged for one VM, as far back
as vCenter keeps events (usually 30 days). Routine power, guest and task
events are hidden without `--all`. A deleted VM is found through stored
history, so its log still shows who deleted it.

## Datastore browsing

```sh
vsfleet datastore ls nvme-01 vm/web-01/
vsfleet datastore find nvme-01 '*.vmdk' --limit 20 -o json
```

Both are read-only and report incomplete results. VMDK results name the VMs
or templates that reference them. See
[datastore browsing](tui-storage.md#datastore-file-browser).

## Captures

A capture is a read-only snapshot of the estate stored in the local
database. Only `capture run` and `capture perf` contact vCenter.

| Command | Does |
| --- | --- |
| `capture run [-A] [--label NAME]` | Read the selected contexts and store one capture |
| `capture import rvtools FILE` | Store an RVTools export as a capture, offline |
| `capture list`, `get captures` | List captures, newest first |
| `describe capture RUN` | Show what a capture recorded and where it has gaps |
| `capture update RUN --label/--note/--pin/--unpin` | Change a capture's label, note or pin |
| `capture delete RUN --force` | Delete one capture |
| `capture prune --older-than 90d [--execute]` | Delete old captures and performance collections; a dry run without `--execute` |
| `capture diff [BASE] [TARGET]` | Compare two captures; `--fail-on` and `--require-complete` gate a pipeline |
| `capture perf --window 7d` | Collect VM performance history; `get perfcollections` lists them |

`RUN` takes the same values as [`--run`](#get-run).

Optional evidence needs extra privileges and is off by default:

| Flag | Adds | Privilege |
| --- | --- | --- |
| `--browse-datastores` | Orphan and zombie VMDK checks | `Datastore.Browse` |
| `--datastore-file-inventory` | The `vFileInfo` file list; see [limits and privacy](exports.md#datastore-file-inventory-vfileinfo) | `Datastore.Browse` |
| `--include-licenses` | License usage, keys excluded; see [licenses](licensing.md) | `Global.Licenses` |

<span id="importing-rvtools-exports"></span>

### Importing RVTools exports

```sh
vsfleet capture import rvtools estate.xlsx --dry-run
vsfleet capture import rvtools estate.xlsx --label pre-migration --timezone America/Los_Angeles
vsfleet capture diff pre-migration wave-1
```

An import becomes a capture every other command can read. See
[importing an RVTools export](exports.md#importing-an-rvtools-export) for
what is read and its limits.

<span id="analyze"></span>

## Analysis

`vsfleet analyze` answers questions from a stored capture, the latest unless
`--run` names another, across every context it recorded unless `-n`
narrows it. A subject is `KIND/NAME`.

| Command | Answers |
|---|---|
| `health` | Which health rules fail, and on what; see [health](health.md) |
| `readiness` | Whether the estate is ready to migrate: `ready`, `blocked` or `unknown` |
| `readiness network SOURCE TARGET` | Whether two clusters' networks match; `-o wide` adds the comparison |
| `sizing` | Whether the VMs fit a proposed destination |
| `capacity [--trend]` | Which VMs drive datastore growth, and when each datastore crosses its floor |
| `orphans` | Which browsed VMDK files no VM claims |
| `churn` | How the VM population changed across captures |
| `snapshots [--trend]` | How old each snapshot is |
| `topology`, `deps`, `blast-radius` | Where a subject sits, what it needs, and what its failure would reach |
| `decommission vm/NAME` | Whether anything argues against retiring a VM; it changes nothing |

Trends read captures from `--from` (or the start) to `--run`, up to
`--limit`. A `--fail-on-*` gate exits `2` when it trips. See
[planning](planning.md) for readiness, sizing and decommission rules.

```sh
vsfleet analyze health --severity warning -o wide
vsfleet analyze readiness --run pre-migration --fail-on-blockers
vsfleet analyze blast-radius datastore/ds-prod-01 -n prod
vsfleet analyze churn --from 2026-01-01
```

## Exports and the database

| Command | Does |
| --- | --- |
| `export rvtools` | Write an RVTools-compatible workbook, or CSV per tab with `--format csv --file DIR`; see [exports](exports.md) |
| `export rvtools --profile NAME [--pseudonymize]` | Write a scoped workbook for sharing; see [sharing](sharing.md) |
| `export report` | Write the `describe capture` report |
| `export metadata [--definition FILE]` | Write tags and custom attributes, or a saved report; see [metadata](metadata.md) |
| `db backup FILE`, `db restore FILE --force` | Back up or replace the capture database |
| `db doctor` | Check the database; exits 1 on a warning |

Exports read `--run` (default `latest`) and write to stdout unless `--file`
is given; a workbook is never written to a terminal.

<span id="compatibility-report"></span>

`vsfleet explain rvtools[.<sheet>]` describes every worksheet and column the
export writes, offline. See [interoperability](exports.md#rvtools-file-interoperability).

The database is `--db`, else `VSFLEET_DB`, else
`<user-config-dir>/vsfleet/history.db`.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The command did what was asked |
| `1` | vsfleet could not do its job: bad configuration, unreachable estate, unreadable database |
| `2` | The estate did not pass a gate: `capture diff --fail-on`, or an `analyze --fail-on-*` |
| `3` | `capture run --fail-on-partial` stored some contexts but not all |

## Persistent flags

| Option | Description |
|---|---|
| `-n, --context <name>` | Scope to one or more contexts; repeatable |
| `-A, --all-contexts` | Every configured context |
| `--config <path>` | Configuration file |
| `--db <path>` | Capture database |
| `--timeout <duration>` | Per-vCenter request timeout (default `30s`) |
| `-o, --output` | Output format |
| `--refresh <duration>` | TUI and `-w` polling interval |

Shell completion covers commands, flags and kinds: see
`vsfleet completion --help`.
