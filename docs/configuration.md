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
