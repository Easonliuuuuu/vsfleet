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
| `vsfleet search <text>` | Search every vCenter at once |
| `vsfleet <kind> list` | List VMs, templates, hosts, clusters, vApps, datastores, or networks |
| `vsfleet vm history <name-or-uuid>` | Show a VM's stored assessment timeline |
| `vsfleet assessment ...` | Capture and compare historical observations |

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
