# vctui

**Operate all your vCenters from one terminal.**

vctui treats every vCenter as a named context, the way `kubectl` treats clusters
and the AWS CLI treats profiles. Each context carries its own endpoint,
credential reference, network route and TLS policy — so a lab you reach
directly and a customer vCenter that only exists behind a SOCKS5 bastion work
side by side, in one process, without exporting anything.

```
$ vctui context list
    NAME         ENDPOINT                            USERNAME                      ROUTE                              TLS
*   prod         https://vcsa.prod.internal          svc-ops@vsphere.local         Direct                             thumbprint
    customer-a   https://vcsa.customer-a.internal    operator@vsphere.local        SOCKS5 -> 127.0.0.1:1080 (remote DNS)   thumbprint
    lab          https://10.20.30.10                 administrator@vsphere.local   Direct                             insecure

$ vctui context test customer-a
Context: customer-a
  Endpoint  https://vcsa.customer-a.internal
  Route     SOCKS5 -> 127.0.0.1:1080 (remote DNS)
  DNS       Remote
  TLS       Verified (Pinned thumbprint)
  Auth      OK
  vCenter   VMware vCenter Server 8.0.3 build-24022515
  Latency   67 ms
Connection successful.

$ vctui search ubuntu-24
VCENTER      TYPE       NAME                  DATACENTER   PATH
customer-a   template   ubuntu-24.04-golden   Frankfurt    /Frankfurt/vm/Templates/ubuntu-24.04-golden
lab          template   ubuntu-24-test        Lab          /Lab/vm/ubuntu-24-test
prod         template   ubuntu-24.04-v8       Taipei       /Taipei/vm/Templates/ubuntu-24.04-v8
prod         vm         ubuntu-24-build-17    Taipei       /Taipei/vm/CI/ubuntu-24-build-17

4 match(es) across 3 vCenter(s) in 812 ms
```

## Why

If you operate one vCenter, the web client is fine. The problem starts at three
or four — a lab, a staging environment, and a vCenter per customer, each with
its own SSO domain, its own credentials, its own certificate, and half of them
only reachable through a tunnel someone has to remember to start.

The usual workflow is: remember the address, remember the route, start the
tunnel, find the password, open a browser, click through the certificate
warning, log in, and only then start looking for the thing you came for.

vctui replaces that with one command per question.

## What it does today

- **Multiple independent vCenters.** Every context is self-contained. Nothing
  is global, so nothing leaks between environments.
- **Per-context networking.** Direct or SOCKS5, chosen per vCenter, with
  optional resolution of the vCenter hostname *at the proxy* for names that
  only exist inside the remote network. Ambient `HTTPS_PROXY` is deliberately
  ignored: a route is configuration, not an accident of the environment.
- **Credentials that stay out of the config file.** The file holds a reference
  such as `keyring:customer-a`; the password lives in the macOS Keychain, the
  Linux Secret Service or the Windows Credential Manager. `prompt` is a
  first-class alternative for machines where nothing should be stored.
- **Certificate policy you can actually explain.** Three named modes —
  `system`, `thumbprint`, `insecure` — instead of one `insecure = true` flag
  that grows on you. A rotated certificate is reported as a mismatch, with the
  fingerprint you pinned and the one you were served, and the connection stops.
- **Inventory across the estate.** VMs, templates, hosts, clusters, datastores
  and networks, listed per context or across all of them at once.
- **Cross-vCenter search.** One query, every vCenter, in parallel.
- **Failure isolation.** A dead proxy, an expired password or a rotated
  certificate in one environment costs one line of output. Everything else
  still answers.
- **Diagnostics that name the fault.** `vctui doctor` walks the connection one
  stage at a time and stops at the first real problem.
- **A terminal interface over the same layers.** `vctui ui` puts every
  configured vCenter in a sidebar, one resource kind per tab, and a detail
  pane on any row. It shows nothing the command line cannot.
- **Table output for people, JSON for scripts.** Every command takes `-o json`.

Everything in this release is read-only. There are no power operations, no
snapshots, and no deletes: the tool should earn trust before it is allowed to
change anything.

## Install

```sh
go install github.com/easonliuuuuu/vc-tui/cmd/vctui@latest
```

Requires Go 1.25 or newer to build.

## Getting started

```sh
# Interactive: asks for the endpoint, route, certificate policy and password,
# tests the connection, and only then saves.
vctui context add

# Unattended, for a provisioning script.
vctui context add \
  --name customer-a \
  --endpoint https://vcsa.customer-a.internal \
  --username operator@vsphere.local \
  --credential keyring:customer-a \
  --transport socks5 --socks-address 127.0.0.1:1080 --remote-dns \
  --tls thumbprint \
  --password-stdin < /run/secrets/customer-a
```

Passing `--tls thumbprint` without `--thumbprint` fetches the certificate the
server presents and shows it to you before pinning it.

Then:

```sh
vctui context list
vctui context test customer-a
vctui status                       # every context, connection state and latency
vctui doctor customer-a            # stage-by-stage diagnosis

vctui vm list --context prod
vctui template list --all-contexts
vctui datastore list --all-contexts
vctui host list --context prod -f esxi-07
vctui search ubuntu-24
vctui search nvme --kind datastore -o json

vctui ui                           # the same estate, interactively
vctui ui --all-contexts            # open with every vCenter merged
```

## The terminal interface

`vctui ui` opens the estate rather than one vCenter. The sidebar lists every
configured context whether or not it answers, the tabs are the resource kinds,
and `a` widens the table from the selected vCenter to all of them at once.

```
vctui  all 3 vCenters  ·  2 connected · 1 failed
────────────────────────────────────────────────────────────────────────────────
CONTEXTS                   VMs 214   Templates 18   Hosts 22   Clusters 4   ...
 ✕ customer-a              ─────────────────────────────────────────────────────
   socks5 proxy 127.0.0.…  VCENTER      NAME               POWER   CPU   MEM
▸● prod                    prod         app-01             on        4   16G
   Direct · 12 ms          prod         build-runner-3     off       8   32G
 ● lab                     lab          ubuntu-24-test     on        2    4G
   Direct · 4 ms           ✕ customer-a: socks5 proxy 127.0.0.1:1080 unreachable
────────────────────────────────────────────────────────────────────────────────
→/l next tab  tab switch pane  enter open  / filter  a all vCenters  d diagnose
```

A vCenter that will not answer keeps its row and states why, under a table that
still holds every result from the vCenters that did. That is the same failure
isolation the CLI has, made visible.

| Key | Action |
|---|---|
| `←` `→` / `h` `l` | Previous, next resource tab |
| `tab` | Move focus between the context sidebar and the table |
| `↑` `↓` / `k` `j` | Move within the focused pane |
| `enter` | Open the row, or switch to the selected vCenter |
| `/` | Filter by name; `esc` clears it |
| `a` | Toggle between one vCenter and all of them |
| `r` / `R` | Reload what is in scope / every context |
| `d` | Diagnose the selected context, stage by stage |
| `?` | Keys |
| `q` | Quit |

The interface holds no state of its own beyond selection and scroll: everything
it displays comes from `internal/session`, `internal/vsphere` and
`internal/config`, the same three packages the commands use. It can be thrown
away and rewritten without taking any behaviour with it, which is why it was
built last.

## Configuration

`vctui context add` writes the file for you; this is what it looks like. The
location is the platform user configuration directory
(`~/.config/vctui/config.toml` on Linux), overridable with `--config` or
`VCTUI_CONFIG`. It is written atomically with `0600` permissions.

```toml
version = 1
current_context = "prod"

[[contexts]]
name = "prod"
endpoint = "https://vcsa.prod.internal"
username = "svc-ops@vsphere.local"
credential = "keyring:prod"

[contexts.transport]
type = "direct"

[contexts.tls]
mode = "thumbprint"
thumbprint = "4C:3D:58:C2:80:EA:08:A0:67:53:79:A8:D5:3B:7C:77:6A:8A:40:EE:D1:80:4E:17:26:39:5B:D7:07:23:D4:D8"

[[contexts]]
name = "customer-a"
endpoint = "https://vcsa.customer-a.internal"
username = "operator@vsphere.local"
credential = "keyring:customer-a"

[contexts.transport]
type = "socks5"
address = "127.0.0.1:1080"
remote_dns = true

[contexts.tls]
mode = "system"
```

### Credential references

| Reference | Meaning |
|---|---|
| `keyring:<key>` | Read from the OS secret store under service `vctui` |
| `prompt` | Ask on each run; store nothing |

No form of this file ever contains a password.

### Transports

| `type` | Behaviour |
|---|---|
| `direct` | Connect from this host, resolving names locally |
| `socks5` | Connect through `address`. With `remote_dns = true` the hostname is handed to the proxy; otherwise it is resolved here first |

### TLS modes

| `mode` | Behaviour |
|---|---|
| `system` | Ordinary chain validation against the system trust store |
| `thumbprint` | Pin one certificate by SHA-256 or SHA-1 fingerprint |
| `insecure` | Verification disabled — a choice you make by name, never a side effect |

## Architecture

The terminal interface is deliberately not the first thing built. Everything
below it exists as an ordinary Go API and an ordinary CLI, so the interface is
a rendering layer rather than the only way to reach a vCenter. It reaches the
rest of the program through one small interface, which is also how its model is
driven in tests with no vCenter present.

```
                       cmd/vctui  ──►  internal/cli  ──►  internal/tui
                                            │                  │
                                            └────────┬─────────┘
                                                     │
                  ┌─────────────────────────┼─────────────────────────┐
                  │                         │                         │
          internal/session          internal/search           internal/config
       (one connection per          (all vCenters at         (contexts; never
        context, with state)         once, in parallel)        any secrets)
                  │                         │                         │
                  └────────────┬────────────┘            internal/credentials
                               │                          (keyring | prompt)
                      internal/vsphere
              (connect, TLS policy, inventory as
               domain objects; govmomi stops here)
                               │
                     internal/transport
                     (per-context dialer)
                        ┌──────┴──────┐
                     direct        socks5
```

Two rules hold the design together: `govmomi` types never leave
`internal/vsphere`, and nothing above `internal/transport` knows how a vCenter
is reached.

## Testing

The whole product is testable without any VMware infrastructure. Integration
tests run against [vcsim](https://github.com/vmware/govmomi/tree/main/vcsim)
through a real SOCKS5 proxy started inside the test process, covering the parts
that are otherwise hard to be confident about:

- routing one context through a proxy while another goes direct
- resolving the vCenter hostname *at the proxy*, verified by inspecting what the
  proxy was asked for
- a proxy that is offline, reported as the proxy rather than as the vCenter
- a pinned thumbprint that no longer matches
- a self-signed certificate rejected under system trust
- one broken vCenter alongside a healthy one, with the healthy results intact

The interface is tested the same way, against a fake backend: tab switching,
context switching, filtering, the detail pane, the diagnosis panel and the
merged all-contexts table with one vCenter failing.

```sh
go test ./...
```

## Roadmap

| Version | Objective |
|---|---|
| 0.0.1–0.0.5 | Contexts, SOCKS5, keyring, inventory API — **done** |
| 0.0.6 | Terminal UI: context switcher, resource tabs, detail view — **done** |
| 0.0.7 | Inventory cache with concurrent background refresh |
| 0.0.8 | Global search in the UI |
| 0.1.0 | First public release |

Explicitly **not** planned for 0.1.0: provisioning, vMotion, NSX, vSAN, alarms,
performance graphs, tag management, a web UI, or a plugin system. They are all
tempting and they are all scope traps.

## License

MIT. See [LICENSE](LICENSE).
