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

| Value | Behavior |
|---|---|
| `keyring:<name>` | Read the password from the native OS secret store |
| `prompt` | Prompt interactively on each run and store nothing on disk |

On systems without an active Secret Service, such as a headless server, SSH
bastion, or container, `context add` records `credential = "prompt"` with a
warning. Passwords never go into TOML, logs, or command history.

## Network routes

| Transport | Behavior |
|---|---|
| `direct` | Direct TCP connection with local DNS resolution |
| `socks5` | SOCKS5 proxy; `--remote-dns` resolves through the proxy |
| `http` | HTTP CONNECT forward proxy |
| `https` | HTTPS CONNECT forward proxy with TLS |

Proxy authentication can use a separate `keyring:<name>` reference. The
unattended setup flags are documented by `vsfleet context add --help`.

## SSH

```toml
[ssh]
user = "ubuntu"
```

Sets the default remote username the TUI's SSH handoff action (see
[Detail pane actions](tui.md#detail-pane-actions)) fills in ahead of a VM's
guest IP or an ESXi host's address. It is not per-context — the setting is
about who you are, not which vCenter the highlighted VM happens to live
behind. Omitting `[ssh]` entirely leaves `ssh` to resolve a user the usual
way, through `~/.ssh/config` and then your local username.

SSH through a proxied context follows the same route vsfleet itself uses —
an unauthenticated SOCKS5 or HTTP CONNECT proxy becomes an `ssh -o
ProxyCommand=...` argument automatically. An authenticated proxy is declined:
its stored password never reaches the command line of a launched process.

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
mutation. Without that privilege or without the flag, zombie-VMDK health is
reported as not evaluated.
