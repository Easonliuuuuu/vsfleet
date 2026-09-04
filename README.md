# vsfleet

<p align="center">
  <a href="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/ci.yml"><img src="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/ci.yml/badge.svg" alt="CI Status"></a>
  <a href="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/docs.yml"><img src="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/docs.yml/badge.svg" alt="Documentation"></a>
  <a href="https://github.com/Easonliuuuuu/vsfleet/releases"><img src="https://img.shields.io/github/v/release/Easonliuuuuu/vsfleet?include_prereleases&color=blue" alt="Latest Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/Easonliuuuuu/vsfleet" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>

<p align="center">
  <strong>Operate multiple VMware vCenter servers from one terminal.</strong>
</p>

<p align="center">
  <picture>
    <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/vsfleet.png">
    <img src="docs/assets/vsfleet.gif" alt="vsfleet inspecting VM and vApp inventory across three vCenters, widening a filter into an estate-wide search, diagnosing an unavailable site, and reviewing assessment changes, trends, and runs" width="1200">
  </picture>
</p>

<p align="center"><sub>Healthy inventory stays usable even when another vCenter is offline.</sub></p>

---

## About

**vsfleet** is an open-source Go CLI and interactive terminal UI (TUI) for
VMware vSphere operators and site reliability engineers.

It organizes each vCenter into a named **context**—similar to a `kubectl`
context—keeping endpoints, credentials, network routes, and certificate policies
strictly separated. Query inventory across one vCenter or an entire estate
without juggling browser tabs or writing brittle scripts.

> [!IMPORTANT]
> **Strict read-only safety guarantee:** vsfleet is an inspection and diagnostic
> tool. It does not power on/off VMs, create or revert snapshots, modify
> networks, provision resources, or delete inventory objects.

## Why vsfleet?

- Query every configured vCenter with one command using `--all-contexts`.
- Search VMs, templates, hosts, clusters, vApps, datastores, and networks across
  the estate.
- Keep healthy results usable when another vCenter is offline or timing out.
- Route each context independently through direct TCP, SOCKS5, HTTP, or HTTPS
  CONNECT proxies.
- Keep passwords in the native OS keyring or an interactive prompt; never write
  them to `config.toml`.
- Pin TLS thumbprints for private or self-signed certificates.
- Capture immutable local SQLite assessments and explain drift over time.
- Use a responsive Bubble Tea TUI or stable JSON output for automation.

## Feature comparison

| Capability | `vsfleet` | `govc` | PowerCLI | vSphere Web Client |
|---|:---:|:---:|:---:|:---:|
| Multi-vCenter query in one command | **Yes** | No | Script loop | No |
| Estate-wide resource search | **Yes** | No | Custom script | Per vCenter |
| Partial results when one site fails | **Yes** | No | Custom error handling | Browser timeout |
| Per-context proxy routing | **Yes** | Global env | Global env | Browser proxy |
| Read-only safety guarantee | **Yes** | No | No | No |
| Historical drift and snapshot age | **Yes** | Export only | Custom script | Point-in-time |

## Install

### Homebrew (macOS and Linux)

```sh
brew install --cask easonliuuuuu/tap/vsfleet
```

### Pre-built binary

Download an archive for your operating system and CPU architecture from
[GitHub Releases](https://github.com/Easonliuuuuu/vsfleet/releases), or install
with Go 1.25 or newer:

```sh
go install github.com/easonliuuuuu/vsfleet/cmd/vsfleet@latest
```

See [Getting Started](https://easonliuuuuu.github.io/vsfleet/getting-started/)
for build-from-source instructions and shell completion.

## Quick start

```sh
# Launch the setup wizard (or open the TUI when contexts already exist)
vsfleet

# Test a context and inspect inventory
vsfleet context test prod
vsfleet vm list

# Search across every configured vCenter
vsfleet search ubuntu --all-contexts
```

## Documentation

The full operator guide is published at
[easonliuuuuu.github.io/vsfleet](https://easonliuuuuu.github.io/vsfleet/):

- [Getting Started](https://easonliuuuuu.github.io/vsfleet/getting-started/)
- [CLI Guide](https://easonliuuuuu.github.io/vsfleet/commands/)
- [Assessments and History](https://easonliuuuuu.github.io/vsfleet/assessments/)
- [Terminal UI](https://easonliuuuuu.github.io/vsfleet/tui/)
- [Configuration](https://easonliuuuuu.github.io/vsfleet/configuration/)
- [Operator Recipes](https://easonliuuuuu.github.io/vsfleet/recipes/)
- [Troubleshooting](https://easonliuuuuu.github.io/vsfleet/troubleshooting/)
- [Architecture](https://easonliuuuuu.github.io/vsfleet/architecture/)

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for development, testing, and synthetic
testbed instructions. See [SECURITY.md](SECURITY.md) for credential handling,
read-only guarantees, and vulnerability reporting.

## License

[MIT](LICENSE) © Eason Liu
