# Configuration

The default configuration file is:

```text
~/.config/vsfleet/config.toml
```

Override it with `--config <path>` or `VSFLEET_CONFIG`. The file is created with
`0600` permissions and contains no passwords.

## Example `config.toml`

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

[[contexts]]
name = "customer-enclave"
endpoint = "https://vcsa.enclave.internal"
username = "readonly@vsphere.local"
credential = "keyring:customer-enclave"
via = "prod"
via_moref = "vm-1234"

[contexts.transport]
type = "socks5"
proxy_address = "127.0.0.1:1080"
remote_dns = true

[contexts.tls]
mode = "thumbprint"
thumbprint = "1A:2B:3C:4D:5E:6F:..."
```

Each context keeps its endpoint, credentials, route, and TLS policy isolated.
Editing or removing a context invalidates its existing session and cache.

## Credentials

A credential reference names *where* the password lives. It never contains the
password, so it is safe in `config.toml`, in version control, and in anything
that prints your configuration.

| Value | Behavior |
|---|---|
| `keyring:<name>` | Read the password from the native OS secret store |
| `prompt` | Prompt interactively on each run and store nothing on disk |
| `env:<VAR>` | Read the password from an environment variable |
| `file:<path>` | Read the password from a file; one trailing newline is stripped |
| `exec:<program>` | Run a program and read the password from its standard output |

Passwords never go into TOML, logs, or command history.

### Unattended sources

`env`, `file` and `exec` resolve without a terminal, which is what makes cron,
systemd, containers and CI possible. They are read-only: they say where a
password is, and nothing stores one through them, so `--password-stdin` is
rejected when combined with them rather than accepted and silently discarded.

They also never fall back to the interactive prompt. A missing variable or an
unreadable file is an error naming exactly what is missing — not a password
prompt that would read whatever a scheduled job happened to have on standard
input.

`env` suits CI runners and container runtimes that inject secrets into the
process environment. On a shared host another process of the same user can
read `/proc/<pid>/environ`, so prefer `file` where that matters.

`file` is the shape a systemd `LoadCredential=` unit, a Kubernetes secret
mount, and a Docker secret all present. File permissions are not enforced,
because Kubernetes projects secret volumes world-readable inside the container
by default; protecting the file is yours to decide.

`exec` reaches a secret manager vsfleet does not integrate with. The reference
names a program and nothing else — no arguments and no shell — so a
configuration file cannot become a shell command, and no secret is ever placed
on a command line where `ps` would show it. A helper needing arguments is a
wrapper script; it is told which context it is answering for:

```sh
#!/bin/sh
# /usr/local/bin/vsfleet-credential
exec vault read -field=password "secret/vcenter/$VSFLEET_CONTEXT"
```

| Variable | Value |
|---|---|
| `VSFLEET_CONTEXT` | The context whose password is being resolved |
| `VSFLEET_CREDENTIAL_REF` | The reference being resolved, e.g. `exec:/usr/local/bin/vsfleet-credential` |

The helper inherits no standard input, and a helper that hangs fails its own
context's `--timeout` rather than holding up the rest of the estate.

On systems without an active Secret Service and without one of the unattended
sources configured, such as a headless server or SSH bastion, `context add`
records `credential = "prompt"` with a warning.

## Network routes

| Transport | Behavior |
|---|---|
| `direct` | Direct TCP connection with local DNS resolution |
| `socks5` | SOCKS5 proxy; `--remote-dns` resolves through the proxy |
| `http` | HTTP CONNECT forward proxy |
| `https` | HTTPS CONNECT forward proxy with TLS |

Proxy authentication can use a separate credential reference of any scheme
above, set with `--proxy-credential`. The unattended setup flags are documented
by `vsfleet context add --help`.

## SSH

```toml
[ssh]
user = "ubuntu"
# vm_user = "ubuntu"
# host_user = "root"
```

Sets the default remote username the TUI's SSH handoff action (see
[Detail pane actions](tui.md#detail-pane-actions)) fills in ahead of a target
address. `vm_user` applies to VM guest IPs and `host_user` applies to ESXi host
names. The older shared `user` setting remains the fallback for both kinds.
When the applicable setting is empty, `ssh` resolves a user through
`~/.ssh/config` and then the local username.

SSH through a proxied context follows the same route vsfleet itself uses —
an unauthenticated SOCKS5 or HTTP CONNECT proxy becomes an `ssh -o
ProxyCommand=...` argument automatically when a compatible `nc` is installed.
HTTPS and authenticated proxies are declined because their credentials or TLS
handshake cannot safely be represented by the generated command.

## TLS policies

| Policy | Behavior |
|---|---|
| `system` | Verify against the system trust store |
| `thumbprint` | Pin a SHA-256 or SHA-1 certificate fingerprint |
| `insecure` | Disable verification; use only when strictly necessary |

When `--tls thumbprint` has no explicit thumbprint, the setup wizard fetches the
remote certificate, displays its fingerprints, and pins the selected value.

## Assessment history path

Assessment history is separate from `config.toml` and defaults to
`<user-config-dir>/vsfleet/history.db`. Set `VSFLEET_HISTORY_DB` or pass
`--history-db` to override it. The private database contains inventory,
identifiers, paths, annotations, snapshot metadata, and coverage, but never
credentials or session cookies.

## vSphere permissions

Use a read-only vSphere account. Inventory and assessment collection do not
need write privileges. The optional `vsfleet assessment run
--browse-datastores` path additionally needs `Datastore.Browse` on the
datastores to inspect VM disk-file metadata; it still performs no inventory
mutation. The datastore backing identity properties are part of the same
read-only datastore inventory and require no additional privilege. Without
`Datastore.Browse` or without the flag, zombie-VMDK health is reported as not
evaluated.
