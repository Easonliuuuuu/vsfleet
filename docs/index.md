# Operate every vCenter from one terminal

vsfleet is an open-source Go CLI and terminal UI for VMware vSphere operators
and site reliability engineers. It organizes each vCenter into a named context,
then lets you inspect inventory, search the estate, diagnose connectivity, and
compare historical observations without juggling browser tabs or scripts.

![vsfleet inspecting inventory across three vCenters](assets/vsfleet.gif){ width="1200" }

> [!IMPORTANT]
> **Strict read-only safety guarantee:** vsfleet never powers on or off VMs,
> changes networks, provisions resources, manages snapshots, or deletes
> inventory objects.

## Why vsfleet?

- Query every configured vCenter with one command using `--all-contexts`.
- Search VMs, templates, hosts, clusters, vApps, datastores, and networks
  across the estate.
- Keep healthy results usable when another vCenter is offline or timing out.
- Route each context independently through direct TCP, SOCKS5, HTTP, or HTTPS
  CONNECT proxies.
- Keep passwords in the operating system keyring or an interactive prompt;
  they never go into `config.toml`.
- Pin TLS thumbprints for private or self-signed vCenters.
- Capture immutable, local SQLite assessments and explain drift over time.
- Use a responsive Bubble Tea TUI or stable JSON output for automation.

## Start here

| Goal | Guide |
|---|---|
| Install and connect the first vCenter | [Getting Started](getting-started.md) |
| Find the right command or JSON output | [CLI Guide](commands.md) |
| Capture, compare, and export inventory history | [Assessments](assessments.md) |
| Learn the interactive terminal UI | [Terminal UI](tui.md) |
| Configure credentials, routes, and TLS | [Configuration](configuration.md) |
| Diagnose a failed connection | [Troubleshooting](troubleshooting.md) |
| Understand the system design | [Architecture](architecture.md) |

## Feature comparison

| Capability | `vsfleet` | `govc` | PowerCLI | vSphere Web Client |
|---|:---:|:---:|:---:|:---:|
| Multi-vCenter query in one command | **Yes** | No | Script loop | No |
| Estate-wide resource search | **Yes** | No | Custom script | Per vCenter |
| Partial results when one site fails | **Yes** | No | Custom error handling | Browser timeout |
| Per-context proxy routing | **Yes** | Global env | Global env | Browser proxy |
| Read-only safety guarantee | **Yes** | No | No | No |
| Historical drift and snapshot age | **Yes** | Export only | Custom script | Point-in-time |

## Community and project links

- [Releases](https://github.com/Easonliuuuuu/vsfleet/releases)
- [Contributing guide](https://github.com/Easonliuuuuu/vsfleet/blob/main/CONTRIBUTING.md)
- [Security policy](https://github.com/Easonliuuuuu/vsfleet/blob/main/SECURITY.md)
- [Source code and issues](https://github.com/Easonliuuuuu/vsfleet)
