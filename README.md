# vsfleet

Operate multiple VMware vCenter servers from one terminal.

<p align="center">
  <picture>
    <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/vsfleet.png">
    <img src="docs/assets/vsfleet.gif" alt="vsfleet browsing virtual machines across three vCenters, filtering inventory, and diagnosing an unavailable site" width="1200">
  </picture>
</p>

<p align="center"><sub>Healthy inventory stays usable even when another vCenter is offline.</sub></p>

## About

vsfleet is an open-source Go CLI and terminal UI for VMware vSphere operators.
It keeps each vCenter in a named **context**, so endpoints, credentials,
network routes, and certificate policies stay separate. You can inspect VMs,
templates, hosts, clusters, datastores, and networks across one vCenter or an
entire estate without repeatedly switching between browser sessions.

vsfleet is currently read-only: it does not power on VMs, create snapshots,
provision resources, or delete inventory objects.

## Why use it?

It is useful when your vCenters have different access paths or security
requirements—for example, a lab reached directly and customer environments
reached through different proxies. A single context can use a direct route,
SOCKS5, HTTP CONNECT, or HTTPS CONNECT, with its own TLS policy.

The main benefits are:

- Query several vCenters with one command.
- Search inventory across all configured contexts.
- Keep passwords out of `config.toml` by using the operating system keyring or
  an interactive prompt.
- Pin vCenter certificates with SHA-256 or SHA-1 thumbprints.
- Continue showing results from healthy vCenters when another one is offline.
- Diagnose failures by stage instead of receiving one generic connection error.
- Use tables for people and JSON output for scripts.

## Install

### With Go

Requires Go 1.25 or newer.

```sh
go install github.com/easonliuuuuu/vsfleet/cmd/vsfleet@latest
```

### From a checkout

```sh
git clone https://github.com/easonliuuuuu/vsfleet.git
cd vsfleet
go build -o vsfleet ./cmd/vsfleet
```

## Quick start

The simplest first run is interactive:

```sh
vsfleet
```

With no contexts configured, vsfleet opens the setup form. You can also use
the command line:

```sh
vsfleet context add
```

The wizard asks for the vCenter endpoint, username, connection route,
certificate policy, and password. It tests the connection before saving the
context.

After adding a context:

```sh
vsfleet context list
vsfleet context test prod
vsfleet status
vsfleet vm list --context prod
vsfleet search ubuntu
```

Use `--all-contexts` to query every configured vCenter:

```sh
vsfleet vm list --all-contexts
vsfleet template list --all-contexts
vsfleet search ubuntu --all-contexts
```

## Common commands

| Command | Purpose |
|---|---|
| `vsfleet` | Open the terminal UI |
| `vsfleet ui` | Open the terminal UI explicitly |
| `vsfleet context add` | Add a vCenter context |
| `vsfleet context list` | List configured contexts |
| `vsfleet context show [name]` | Show a context’s settings |
| `vsfleet context use <name>` | Select the current context |
| `vsfleet context test <name>` | Test one connection |
| `vsfleet context remove <name>` | Remove a context |
| `vsfleet status` | Test and summarize every selected context |
| `vsfleet doctor [context...]` | Diagnose connection stages |
| `vsfleet search <text>` | Search inventory across vCenters |
| `vsfleet <kind> list` | List a resource kind |

Supported resource kinds are `vm`, `template`, `host`, `cluster`,
`datastore`, and `network`. Each kind also accepts `--filter` / `-f`:

```sh
vsfleet host list --context prod --filter esxi-07
vsfleet datastore list --all-contexts -f nvme
```

Use `-o json` with commands that produce data:

```sh
vsfleet vm list --all-contexts -o json
vsfleet search nvme --kind datastore -o json
```

Useful global options:

| Option | Purpose |
|---|---|
| `--context <name>` | Limit the command to a context; repeatable |
| `--all-contexts` | Act on every configured context |
| `--config <path>` | Use a specific configuration file |
| `--timeout <duration>` | Set the per-vCenter timeout; default is `30s` |
| `-o, --output json` | Emit machine-readable JSON instead of a table |

## Terminal UI

Run `vsfleet` or `vsfleet ui` to browse the estate interactively. The
sidebar shows contexts, the tabs show resource kinds, and a detail pane shows
the selected object. A failed context remains visible while results from
healthy contexts are still usable.

| Key | Action |
|---|---|
| `←` / `→` or `h` / `l` | Change resource tab |
| `↑` / `↓` or `k` / `j` | Move through rows |
| `tab` | Switch between the sidebar and table |
| `enter` | Open the selected row or context |
| `/` | Filter by name; `esc` clears the filter |
| `a` | Toggle the all-contexts view |
| `r` / `R` | Reload the current scope / all contexts |
| `d` | Diagnose the selected context |
| `n` / `e` / `x` | Add / edit / remove a context |
| `?` | Show the key reference |
| `q` | Quit |

## Configuration

The normal configuration path is:

```text
~/.config/vsfleet/config.toml
```

Use `--config` or the `VSFLEET_CONFIG` environment variable to override it.
The file is created with restricted permissions and contains no passwords.

Most users should create contexts with `vsfleet context add`. A minimal
configuration looks like this:

```toml
version = 1
current_context = "prod"

[[contexts]]
name = "prod"
endpoint = "https://vcsa.example.internal"
username = "administrator@vsphere.local"
credential = "keyring:prod"

[contexts.transport]
type = "direct"

[contexts.tls]
mode = "system"
```

### Credentials

The `credential` field is a reference, not a password:

| Value | Behavior |
|---|---|
| `keyring:<name>` | Read the password from the OS secret store |
| `prompt` | Ask for the password on each run and store nothing |

The keyring uses the platform’s native secret storage where available:
macOS Keychain, Linux Secret Service, or Windows Credential Manager. Proxy
credentials use the same mechanism.

For unattended setup, provide the password through standard input:

```sh
printf '%s\n' "$VCENTER_PASSWORD" | \
  vsfleet context add \
    --name prod \
    --endpoint https://vcsa.example.internal \
    --username administrator@vsphere.local \
    --credential keyring:prod \
    --password-stdin \
    --tls system
```

### Network routes

Set `--transport` when adding a context:

| Transport | Behavior |
|---|---|
| `direct` | Connect directly and resolve names locally |
| `socks5` | Connect through a SOCKS5 proxy |
| `http` | Connect through an HTTP CONNECT proxy |
| `https` | Connect through an HTTPS CONNECT proxy |

Proxy transports require `--proxy-address host:port`. SOCKS5 supports
`--remote-dns` when the vCenter hostname can only be resolved by the proxy;
HTTP and HTTPS CONNECT always resolve the target through the proxy.

Example:

```sh
vsfleet context add \
  --name customer-a \
  --endpoint https://vcsa.customer-a.internal \
  --username operator@vsphere.local \
  --credential keyring:customer-a \
  --transport socks5 \
  --proxy-address 127.0.0.1:1080 \
  --remote-dns \
  --tls thumbprint
```

### TLS policies

| Policy | Behavior |
|---|---|
| `system` | Verify the certificate using the system trust store |
| `thumbprint` | Pin a SHA-256 or SHA-1 certificate fingerprint |
| `insecure` | Disable certificate verification; use only when intentional |

With `--tls thumbprint` and no `--thumbprint`, the add wizard fetches the
presented certificate and shows its fingerprints before pinning it. A later
certificate change is reported as a mismatch and the connection is stopped.

## Troubleshooting

Start with the command that best matches the symptom:

```sh
vsfleet context show prod       # inspect endpoint, route, and TLS settings
vsfleet context test prod       # test the complete connection
vsfleet doctor prod             # identify the failing stage
vsfleet status                  # compare all selected contexts
```

`doctor` checks configuration, credentials, routing, proxy access, DNS, TCP,
TLS, authentication, and API access in order. If one context fails during a
multi-context query, the error is reported separately and successful results
are retained.

## Development

Run the test suite and build locally with:

```sh
go test ./...
go vet ./...
go build ./...
```

The integration tests use VMware’s vSphere simulator and in-process proxy
servers, so they do not require access to a real vCenter.

### Regenerate the README demo

The presentation uses deterministic sample inventory and never reads your
configuration, credentials, or network. With
[VHS](https://github.com/charmbracelet/vhs) installed, regenerate both the
animated demo and its reduced-motion screenshot from the repository root:

```sh
vhs docs/demo.tape
```

The tape builds `cmd/vsfleet-demo` as a temporary `vsfleet` binary, records the
same command users run, and writes the assets in `docs/assets/`.

## License

MIT. See [LICENSE](LICENSE).
