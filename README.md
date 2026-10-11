# vsfleet

<p align="center">
  <a href="https://github.com/Easonliuuuuu/vsfleet/releases"><img src="https://img.shields.io/github/v/release/Easonliuuuuu/vsfleet?include_prereleases&amp;color=blue&amp;style=flat-square" alt="Latest release"></a>
  <a href="https://easonliuuuuu.github.io/vsfleet/getting-started/#release-status"><img src="https://img.shields.io/badge/status-alpha-orange?style=flat-square" alt="Status: alpha"></a>
  <a href="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/winget-status.yml"><img src="https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2FEasonliuuuuu%2Fvsfleet%2Fpackage-status%2Fwinget.json&amp;label=WinGet&amp;style=flat-square" alt="WinGet availability for the latest stable release"></a>
</p>

<p align="center">
  <strong>Read-only vSphere estate assessment and diagnostics across every vCenter.</strong><br>
  <sub>Inspect, assess migration readiness, and track what changed &mdash; from Linux, macOS, or Windows, with no GUI or .NET runtime.</sub>
</p>

<p align="center">
  <picture>
    <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/vsfleet.png">
    <img src="docs/assets/vsfleet.gif" alt="vsfleet demo command and startup animation, a connection form preview, VM details and timeline, network wiring and policies, vApp members, and estate-wide search" width="1200">
  </picture>
</p>

<p align="center"><sub>Explore a synthetic estate: connection setup, VM details and timeline, networks, vApps, and search.</sub></p>

<p align="center">
  <a href="#why-vsfleet">Why vsfleet?</a> &bull;
  <a href="#installation">Installation</a> &bull;
  <a href="#quick-start">Quick Start</a> &bull;
  <a href="#documentation">Documentation</a>
</p>

---

## Why vsfleet?

Managing multiple VMware vCenters traditionally requires juggling browser tabs, coping with slow web interfaces, or maintaining brittle scripts that fail completely when a single endpoint is unreachable.

**vsfleet** organizes each vCenter into a named **context**—similar to a `kubectl` context—keeping endpoints, credentials, network routes, and certificate policies strictly separated:

- <img src="docs/assets/icons/globe.svg" width="16" height="16" alt=""> **Estate-Wide Multi-vCenter Queries**: Query every configured vCenter in parallel with a single command using `--all-contexts`.
- <img src="docs/assets/icons/shield-check.svg" width="16" height="16" alt=""> **Partial Failure Resilience**: Unreachable or timing-out sites do not block results; healthy vCenters remain responsive and usable.
- <img src="docs/assets/icons/lock.svg" width="16" height="16" alt=""> **Strict Read-Only Safety**: Never powers VMs on or off, reverts snapshots, modifies networks, or alters inventory. See [SECURITY.md](SECURITY.md).
- <img src="docs/assets/icons/arrow-switch.svg" width="16" height="16" alt=""> **Independent Proxy Routing**: Route each context independently through direct TCP, SOCKS5, HTTP, or HTTPS CONNECT proxies, with optional TLS thumbprint pinning.
- <img src="docs/assets/icons/key.svg" width="16" height="16" alt=""> **Secure Credential Handling**: Zero plaintext passwords in `config.toml`. Resolves credentials via native OS keyrings, interactive prompts, or unattended sources.
- <img src="docs/assets/icons/graph.svg" width="16" height="16" alt=""> **Historical Drift & RVTools-Compatible Exports**: Capture immutable local SQLite snapshots, track drift over time, and export 25-sheet Excel workbooks for migration sizing ([details](docs/assessments.md#deterministic-exports)).
- <img src="docs/assets/icons/workflow.svg" width="16" height="16" alt=""> **Distributed-Network Readiness**: Compare cross-cluster VLAN mappings, policy, MTU, host coverage, and affected VMs before migration.
- <img src="docs/assets/icons/terminal.svg" width="16" height="16" alt=""> **Interactive TUI + Scriptable JSON**: Fast Bubble Tea terminal UI with local workstation handoffs (SSH, web browser, clipboard) alongside structured JSON for automation.

### Feature comparison

| Capability | `vsfleet` | `govc` | PowerCLI | vSphere Web Client |
|---|:---:|:---:|:---:|:---:|
| Multi-vCenter query in one command | **Yes** | No | Yes | No |
| Estate-wide resource search, every kind at once | **Yes** | No | Per cmdlet | Per vCenter |
| Partial results when one site fails | **Yes** | No | Custom error handling | Browser timeout |
| Per-context proxy routing | **Yes** | Global env | Global env | Browser proxy |
| Read-only safety guarantee | **Yes** | No | No | No |
| Historical drift and snapshot age | **Yes** | Export only | Custom script | Point-in-time |

<sub>`govc` and PowerCLI are full read-write toolkits; vsfleet deliberately is not.</sub>

---

## Installation

Choose the installation method for your operating system:

| Platform | Install |
| --- | --- |
| macOS or Linux with Homebrew | `brew install easonliuuuuu/tap/vsfleet` |
| Windows 10/11 | `winget install --id Easonliuuuuu.vsfleet --exact` |
| Debian or Ubuntu | [Download and install a `.deb` package](https://easonliuuuuu.github.io/vsfleet/getting-started/#linux-packages-debian-ubuntu-rhel-fedora) |
| RHEL or Fedora | [Download and install an `.rpm` package](https://easonliuuuuu.github.io/vsfleet/getting-started/#linux-packages-debian-ubuntu-rhel-fedora) |

Scoop, release archives, and `go install` are covered in [Getting Started](https://easonliuuuuu.github.io/vsfleet/getting-started/).

For unattended CLI commands and CI, use the container image (Linux amd64 and arm64):

```sh
docker run --rm ghcr.io/easonliuuuuu/vsfleet:latest compatibility report --sheet vInfo -o json
```

The interactive TUI needs the native binary; see the [Containers guide](https://easonliuuuuu.github.io/vsfleet/containers/) for running the image.

---

## Quick Start

### 1. Test drive without a vCenter

```sh
vsfleet --version
vsfleet demo
```

Check the installation, then open the TUI on a synthetic three-vCenter estate, with no configuration, credentials, or network access. Press `?` for help and `q` to quit.

### 2. Connect your first vCenter

Run `vsfleet`. With no contexts configured, it opens the interactive setup wizard, and you can add more vCenters from inside the TUI later.

Name the context `prod` to follow the examples below, or substitute your chosen name. Use a read-only account; see [Getting Started](https://easonliuuuuu.github.io/vsfleet/getting-started/#first-context) for certificate and credential choices.

```sh
# Open the TUI; the setup wizard starts when no contexts exist
vsfleet
```

To open the wizard directly, or to check a context from the command line:

```sh
# Launch the setup wizard
vsfleet context add

# Test connectivity, routes, and authentication
vsfleet context test prod
```

### 3. Search and inspect inventory

```sh
# Open the interactive terminal UI
vsfleet

# List specific resources
vsfleet vm list
vsfleet host list --context prod
vsfleet vm list --context prod -o json

# Search across every configured vCenter at once
vsfleet search ubuntu --all-contexts
```

### 4. Capture and export

```sh
vsfleet assessment run --all-contexts --label first-capture
vsfleet assessment report latest
vsfleet assessment export --format rvtools --file estate.xlsx
```

The report and workbook read the saved capture. After taking another capture, [compare runs and inspect trends](https://easonliuuuuu.github.io/vsfleet/assessments/#compare-runs).

> [!NOTE]
> vsfleet is a personal project, not affiliated with or endorsed by Dell Technologies; its RVTools-compatible export is an independent interoperability implementation.

---

## Documentation

The full operator guide is published at **[easonliuuuuu.github.io/vsfleet](https://easonliuuuuu.github.io/vsfleet/)**:

| Guide | Description |
|---|---|
| <img src="docs/assets/icons/rocket.svg" width="16" height="16" alt=""> **[Getting Started](https://easonliuuuuu.github.io/vsfleet/getting-started/)** | Installation, first configuration, shell completion |
| <img src="docs/assets/icons/code.svg" width="16" height="16" alt=""> **[CLI Guide](https://easonliuuuuu.github.io/vsfleet/commands/)** | Commands, flags, filters, JSON output |
| <img src="docs/assets/icons/terminal.svg" width="16" height="16" alt=""> **[Terminal UI](https://easonliuuuuu.github.io/vsfleet/tui/)** | Keybindings, filtering, workstation actions |
| <img src="docs/assets/icons/package.svg" width="16" height="16" alt=""> **[Containers](https://easonliuuuuu.github.io/vsfleet/containers/)** | Docker, Kubernetes, CI automation |
| <img src="docs/assets/icons/graph.svg" width="16" height="16" alt=""> **[Assessments & History](https://easonliuuuuu.github.io/vsfleet/assessments/)** | Captures, diffs, trends, export formats |
| <img src="docs/assets/icons/gear.svg" width="16" height="16" alt=""> **[Configuration](https://easonliuuuuu.github.io/vsfleet/configuration/)** | Proxies, TLS thumbprints, credential sources |
| <img src="docs/assets/icons/book.svg" width="16" height="16" alt=""> **[Operator Recipes](https://easonliuuuuu.github.io/vsfleet/recipes/)** | Real-world workflows and pipelines |
| <img src="docs/assets/icons/tools.svg" width="16" height="16" alt=""> **[Troubleshooting](https://easonliuuuuu.github.io/vsfleet/troubleshooting/)** | `vsfleet doctor` and common fixes |
| <img src="docs/assets/icons/stack.svg" width="16" height="16" alt=""> **[Architecture](https://easonliuuuuu.github.io/vsfleet/architecture/)** | Concurrency, session caching, security invariants |

---

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and testing, and the [Synthetic Testbed](https://easonliuuuuu.github.io/vsfleet/testbed/) guide for scenarios and PTY journeys. See [SECURITY.md](SECURITY.md) for credential handling, read-only guarantees, and vulnerability reporting.

---

## License

[MIT](LICENSE) © Eason Liu
