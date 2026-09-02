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
- **Per-context networking.** Direct, SOCKS5, HTTP CONNECT or HTTPS CONNECT,
  chosen per vCenter. SOCKS5 can resolve the vCenter hostname *at the proxy*
  for names that only exist inside the remote network; HTTP and HTTPS proxies
  always do. Any of the three proxy routes can require a username and
  password. Ambient `HTTP_PROXY`/`HTTPS_PROXY` is deliberately ignored: a
  route is configuration, not an accident of the environment.
- **Credentials that stay out of the config file.** The file holds a reference
  such as `keyring:customer-a`; the password lives in the macOS Keychain, the
  Linux Secret Service or the Windows Credential Manager. `prompt` is a
  first-class alternative for machines where nothing should be stored. Proxy
  passwords follow the exact same rule — a `keyring:customer-a-proxy`
  reference by default, never the password itself, in `config.toml`.
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
- **A terminal interface that opens by default.** Run `vctui` and it puts
  every configured vCenter in a sidebar, one resource kind per tab, and a
  detail pane on any row. With no contexts yet, it opens straight into adding
  one instead of sending you to read this file first. It shows nothing the
  command line cannot.
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
vctui
```

That's it. With no configuration file yet, this opens straight into adding
your first vCenter — endpoint, route, certificate policy, password — tests
the connection, and saves it. From there `n` adds another, `e` edits the
selected one, `x` removes it; nothing about context management needs a
separate trip to the command line.

The command line does the same work for scripting and provisioning:

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
  --transport socks5 --proxy-address 127.0.0.1:1080 --remote-dns \
  --tls thumbprint \
  --password-stdin < /run/secrets/customer-a
```

A proxy that itself needs a username and password takes the same shape as the
vCenter credential — a keyring reference, `prompt`, or piped on stdin. With
both `--password-stdin` and `--proxy-password-stdin` set, the vCenter
password is read first and the proxy password second — one per line:

```sh
printf '%s\n%s\n' "$VCENTER_PASSWORD" "$PROXY_PASSWORD" | \
vctui context add \
  --name customer-b \
  --endpoint https://vcsa.customer-b.internal \
  --username operator@vsphere.local \
  --credential keyring:customer-b \
  --transport https --proxy-address proxy.customer-b.internal:3128 \
  --proxy-username svc-proxy --proxy-credential keyring:customer-b-proxy \
  --password-stdin --proxy-password-stdin --tls thumbprint
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

vctui                               # the same estate, interactively
vctui ui --all-contexts             # an explicit alias, open with every vCenter merged
```

## The terminal interface

`vctui` — or, spelled out, `vctui ui` — opens the estate rather than one
vCenter. The sidebar lists every configured context whether or not it
answers, the tabs are the resource kinds, and `a` widens the table from the
selected vCenter to all of them at once.

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
| `s` | Cycle the sort order: name, then status (trouble first) |
| `r` / `R` | Reload what is in scope / every context |
| `d` | Diagnose the selected context, stage by stage |
| `n` / `e` / `x` | Add, edit, or delete the selected context |
| `?` | Keys |
| `q` | Quit |

Adding or editing a context opens a form over the same fields
`vctui context add` asks for — endpoint, username, credential, route, TLS
policy — with a **Test connection** row that runs the identical stage-by-stage
diagnosis `vctui doctor` prints, and a **Discover from the server** action next
to the thumbprint field for trust-on-first-use pinning. A test that fails
blocks saving once; pressing the now-relabelled **Save anyway** goes through
regardless, for an operator who wants to fix the fault after saving rather
than before. Deleting asks once, names the stored password explicitly, and
defaults to removing it along with the context.

The context, resource tab and sort order are remembered between runs — in
`~/.config/vctui/state.json` (`VCTUI_STATE` to override), never in
`config.toml`, which stays exactly what it says it is: contexts, not
scratch UI state. `--context` overrides the remembered context for one run
without changing what gets remembered next time.

Beyond that one small file, the interface holds no state of its own: what it
displays comes from `internal/session`, `internal/vsphere` and
`internal/config`, the same packages the commands use, reached through one
`Backend` interface so the whole model — including the form — runs in tests
against a fake, with no vCenter, proxy or certificate anywhere in sight. It
can be thrown away and rewritten without taking any behaviour with it, which
is why it was built last.

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

[[contexts]]
name = "customer-b"
endpoint = "https://vcsa.customer-b.internal"
username = "operator@vsphere.local"
credential = "keyring:customer-b"

[contexts.transport]
type = "https"
address = "proxy.customer-b.internal:3128"
username = "svc-proxy"
credential = "keyring:customer-b-proxy"

[contexts.tls]
mode = "thumbprint"
thumbprint = "4C:3D:58:C2:80:EA:08:A0:67:53:79:A8:D5:3B:7C:77:6A:8A:40:EE:D1:80:4E:17:26:39:5B:D7:07:23:D4:D8"
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
| `http` | Tunnel through `address` with an HTTP `CONNECT` request. The proxy always resolves the hostname; there is no `remote_dns` toggle for it |
| `https` | The same `CONNECT` tunnel, over a TLS connection to the proxy itself, verified against the system trust store — there is no thumbprint-pinning mode for the proxy's own certificate, unlike the vCenter's |

Any of the three proxy transports accepts an optional `username` and a
`credential` reference, resolved and sent as HTTP Basic auth (`http`/`https`)
or RFC 1929 username/password (`socks5`). A route with no `username` needs no
proxy authentication.

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
                ┌────────┬────────┬────────┐
             direct    socks5    http     https
```

Two rules hold the design together: `govmomi` types never leave
`internal/vsphere`, and nothing above `internal/transport` knows how a vCenter
is reached.

Adding, editing, testing and removing a context is `internal/contextops`,
sitting beside `internal/session` and `internal/search` in that middle layer:
it validates, optionally runs the same diagnosis `internal/vsphere` exposes
for `doctor`, stores the password through `internal/credentials`, and writes
through `internal/config` — one path in, called by both the CLI wizard and
the interface's form, so "what does saving a context do" has one answer. The
interface's own memory of where it was left — context, tab, sort order — is
smaller still: `internal/uistate`, a JSON file with no secrets in it and
nothing `internal/config` needs to know about.

## Testing

The whole product is testable without any VMware infrastructure. Integration
tests run against [vcsim](https://github.com/vmware/govmomi/tree/main/vcsim)
through real SOCKS5 and HTTP/HTTPS CONNECT proxies started inside the test
process, covering the parts that are otherwise hard to be confident about:

- routing one context through a proxy while another goes direct
- resolving the vCenter hostname *at the proxy*, verified by inspecting what the
  proxy was asked for
- a proxy that is offline, reported as the proxy rather than as the vCenter
- a proxy that requires username/password authentication, and one where the
  password is wrong — reported by `doctor` as its own "Proxy authentication"
  stage, not lumped in with a dead connection
- an HTTPS proxy whose own certificate is untrusted, rejected before the
  CONNECT tunnel is ever opened, and one that is simply offline
- a proxy credential resolved exactly once per diagnosis, not once per
  internal dialer, even when it comes from an interactive prompt
- a pinned thumbprint that no longer matches
- a self-signed certificate rejected under system trust
- one broken vCenter alongside a healthy one, with the healthy results intact

`doctor`'s own stage sequencing — skipping everything after the first
failure, and renaming a stage when the failure turns out to be a rejected
proxy credential rather than a dead connection — has fast unit tests in
`internal/vsphere` that need no vcsim and no network, alongside the slower
integration tests that prove the same rules hold end to end.

The interface is tested the same way, against a fake backend: tab switching,
context switching, filtering, the detail pane, the diagnosis panel, sorting,
the merged all-contexts table with one vCenter failing, and the add/edit
form — saving, a failed test blocking the save until "save anyway", editing
in place, deleting with and without confirmation, and certificate discovery.
`internal/contextops` has its own tests against a real simulated vCenter:
a save that passes, one a failed test blocks, and removal including the
stored credential.

```sh
go test ./...
```

## Releasing

Releases are driven by [Conventional Commits](https://www.conventionalcommits.org/).
After changes land on `main`, Release Please opens or updates a release pull
request. Merging that pull request creates the version tag and GitHub release;
the same workflow then builds and attaches the platform archives with
GoReleaser.

Use `feat:` for user-visible features, `fix:` for bug fixes, and `feat!:` (or
the `BREAKING CHANGE` footer) for breaking changes. Documentation, tests and
CI-only changes do not create a release by themselves. The initial release is
bootstrapped as `v0.1.0` from the existing feature history.

## Roadmap to 0.1.0

The goal is a fast, resilient, read-only multi-vCenter interface a new user
can operate without learning the CLI subcommand tree first.

| Area | Objective | Status |
|---|---|---|
| Entry experience | `vctui` opens the interface directly; add/edit/test/delete a context from inside it; remember the last context, tab and sort mode | **done** |
| Proxy support | Direct, SOCKS5, HTTP and HTTPS routes; SOCKS5 remote DNS; unauthenticated and basic-auth proxies; passwords only ever in the keyring; explicit TLS modes; never inherit `HTTP_PROXY`/`HTTPS_PROXY` | **done** |
| Diagnostics and test coverage | `doctor` distinguishing proxy reachability, proxy auth, DNS/routing, CONNECT, proxy TLS, vCenter TLS, vCenter auth and API access; integration tests per route; graceful behaviour against unreachable contexts and limited-permission accounts | proxy reachability (with proxy TLS folded in), proxy auth, DNS/routing, CONNECT, vCenter TLS, vCenter auth and API access all named as their own stage, with a rejected proxy password reported separately from a dead connection; every route has offline and, where applicable, auth-failure integration tests; one unreachable context among several is already isolated — a limited-permission account is not yet |
| Responsive inventory loading | A shared cache outside the interface; the selected context first, others concurrently, bounded; stale-while-revalidate; per-context refresh timestamps and errors; the keyboard never blocks on the network | not started |
| Global search | Search cached inventory across every configured vCenter from inside the interface; selecting a result switches context, tab and selection; partial results survive a failed context | the CLI has cross-vCenter search today; bringing the same query into the interface, over the inventory cache above, is next |
| Release hardening | Real vSphere 7/8, self-signed certs, enterprise CAs, thumbprints, restricted RBAC accounts; large-vcsim performance; Linux/macOS/Windows smoke tests; a demo GIF; GoReleaser binaries; tag v0.1.0 | not started — needs real VMware infrastructure and multi-platform hands, not just code |

### Definition of done

A new user can install vctui, run `vctui`, configure a direct or proxied
vCenter entirely inside the interface, inspect all supported inventory,
search across contexts, and keep using the contexts that are healthy or
cached when another one fails.

### Explicitly not planned for 0.1.0

VM power operations, snapshots, provisioning, cloning or migration;
performance graphs, alarms, tasks or events; tags, content libraries, vSAN,
NSX or distributed-switch management; SSH bastions, Vault, 1Password or
additional hypervisors; a web UI, an API server, an MCP server, or a plugin
system. They are all tempting and they are all scope traps.

After 0.1.0, feature development pauses long enough to hear from real
VMware operators, and repeated requests — not this list — decide what comes
next.

## License

MIT. See [LICENSE](LICENSE).
