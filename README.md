# vsfleet

<p align="center">
  <a href="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/ci.yml"><img src="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/ci.yml/badge.svg" alt="CI Status"></a>
  <a href="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/docs.yml"><img src="https://github.com/Easonliuuuuu/vsfleet/actions/workflows/docs.yml/badge.svg" alt="Documentation"></a>
  <a href="https://github.com/Easonliuuuuu/vsfleet/releases"><img src="https://img.shields.io/github/v/release/Easonliuuuuu/vsfleet?include_prereleases&color=blue" alt="Latest Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/Easonliuuuuu/vsfleet" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>

<p align="center">
  <strong>RVTools-compatible vSphere exports across every vCenter at once.</strong><br>
  <sub>From Linux, macOS, or Windows &mdash; no GUI, no .NET. Read-only by construction, and it remembers what changed.</sub>
</p>

<p align="center">
  <picture>
    <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/vsfleet.png">
    <img src="docs/assets/vsfleet.gif" alt="vsfleet inspecting VM and vApp inventory across three vCenters, widening a filter into an estate-wide search, diagnosing an unavailable site, and reviewing assessment changes, trends, and runs" width="1200">
  </picture>
</p>

<p align="center"><sub>Healthy inventory stays usable even when another vCenter is offline.</sub></p>

---

## Try it without a vCenter

```sh
brew install --cask easonliuuuuu/tap/vsfleet
vsfleet demo
```

`vsfleet demo` opens the interface on a synthetic three-vCenter estate: two
healthy sites on different routes and one whose proxy refuses the connection,
because staying useful when a site is down is the point. It reads no
configuration, opens no keyring, dials nothing, and writes nothing back. Every
screen is marked `DEMO · SAMPLE DATA`.

> [!IMPORTANT]
> **Strict read-only safety guarantee:** vsfleet is an inspection and diagnostic
> tool. It does not power on/off VMs, create or revert snapshots, modify
> networks, provision resources, or delete inventory objects.

## Estate-wide RVTools exports

RVTools is the format most migration and sizing tools read. vsfleet writes it
for your whole estate in one run, from any operating system, with no GUI and no
.NET runtime:

```sh
vsfleet assessment run --all-contexts --label q3-audit
vsfleet assessment export --format rvtools --file estate.xlsx
```

`--format csv` writes one file per tab instead, for pipelines that would rather
have text than a workbook. Exporting the same stored assessment twice is
byte-identical, and every export prints a SHA256 receipt.

### Supported tabs

vsfleet renders **eleven** RVTools tabs, using RVTools' own column names:

`vInfo` · `vCPU` · `vMemory` · `vDisk` · `vPartition` · `vNetwork` · `vTools` ·
`vHost` · `vCluster` · `vDatastore` · `vSnapshot`

RVTools itself ships roughly thirty. If your pipeline needs a tab that is not
in that list — `vHealth` and `vRP` are the common ones — vsfleet is not yet a
drop-in for it. This is a **compatible** export, not a replacement for
RVTools.

`vPartition` reports guest filesystem usage, which only VMware Tools inside
the guest can measure. VMs with no running Tools contribute no rows, and
`vsfleetCoverage` says how many of them answered rather than leaving a short
tab to look like a small estate.

Every export also carries a `vsfleetCoverage` sheet naming what collected, what
failed, and why, per vCenter and per tab. A partial estate is reported as
partial rather than handed over as if it were whole.

## What changed since the last audit

Assessments are immutable local SQLite captures, so the estate has a history
rather than a series of disconnected spreadsheets:

```sh
vsfleet assessment diff q3-audit latest   # what changed between two captures
vsfleet assessment trends capacity        # compute and storage over time
vsfleet assessment trends churn           # VMs created and destroyed
vsfleet assessment snapshots              # snapshot ages, oldest first
```

## About

**vsfleet** is an open-source Go CLI and interactive terminal UI (TUI) for
VMware vSphere operators and site reliability engineers.

It organizes each vCenter into a named **context**—similar to a `kubectl`
context—keeping endpoints, credentials, network routes, and certificate policies
strictly separated. Query inventory across one vCenter or an entire estate
without juggling browser tabs or writing brittle scripts.

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
| Multi-vCenter query in one command | **Yes** | No | Yes | No |
| Estate-wide resource search, every kind at once | **Yes** | No | Per cmdlet | Per vCenter |
| Partial results when one site fails | **Yes** | No | Custom error handling | Browser timeout |
| Per-context proxy routing | **Yes** | Global env | Global env | Browser proxy |
| Read-only safety guarantee | **Yes** | No | No | No |
| Historical drift and snapshot age | **Yes** | Export only | Custom script | Point-in-time |

PowerCLI connects to several vCenters at once and fans most cmdlets out across
them; what it does not give you is one query spanning every resource kind, or
partial results by default when a site is unreachable. `govc` and PowerCLI are
both full read-write toolkits — that is a capability vsfleet deliberately does
not have, not a gap in theirs.

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
# Look around first: sample data, no vCenter, nothing written
vsfleet demo

# Add your first vCenter, then confirm the path to it
vsfleet context add
vsfleet context test prod

# Inspect inventory, and search across every configured vCenter
vsfleet vm list
vsfleet search ubuntu --all-contexts

# Capture the estate and export it for a migration or sizing tool
vsfleet assessment run --all-contexts
vsfleet assessment export --format rvtools --file estate.xlsx
```

A bare `vsfleet` opens the terminal interface, or the setup form when no
contexts exist yet.

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
