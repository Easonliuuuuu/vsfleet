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
username = "svc-ro@vsphere.local"
credential = "keyring:prod"

[contexts.transport]
type = "direct"

[contexts.tls]
mode = "system"

[[contexts]]
name = "customer-enclave"
endpoint = "https://vcsa.enclave.internal"
username = "svc-ro@vsphere.local"
credential = "keyring:customer-enclave"
via = "prod"
via_moref = "vm-1234"

[contexts.transport]
type = "socks5"
address = "127.0.0.1:1080"
remote_dns = true

[contexts.tls]
mode = "thumbprint"
thumbprint = "1A:2B:3C:4D:5E:6F:..."
```

Each context keeps its endpoint, credentials, route, and TLS policy isolated.
Editing or removing a context invalidates its existing session and cache.

## Credentials

A credential reference names the password source, never the password itself.
It can be stored in `config.toml` or version control.

| Value | Behavior |
|---|---|
| `keyring:<name>` | Read the password from the native OS secret store |
| `prompt` | Prompt interactively on each run and store nothing on disk |
| `env:<VAR>` | Read the password from an environment variable |
| `file:<path>` | Read the password from a file; one trailing newline is stripped |
| `exec:<program>` | Run a program and read the password from its standard output |

vsfleet never writes passwords to TOML or logs. Avoid putting them in shell
commands that would save them in command history.

### Unattended sources

Use `env`, `file`, or `exec` for cron, systemd, containers, and CI. These
sources are read-only: `--password-stdin` is rejected with them. A missing or
failing source produces a specific error and never falls back to a prompt.

- `env`: useful for injected secrets. Another process running as the same
  user may read `/proc/<pid>/environ`; prefer `file` when that matters.
- `file`: works with systemd `LoadCredential=`, Kubernetes secret mounts,
  and Docker secrets. vsfleet does not enforce file permissions; protect the
  file using your platform's controls.
- `exec`: runs one program with no arguments or shell. Use a wrapper when
  your secret-manager helper needs arguments. Secrets stay off the command line.

The helper receives the context name, for example:

```sh
#!/bin/sh
# /usr/local/bin/vsfleet-credential
exec vault read -field=password "secret/vcenter/$VSFLEET_CONTEXT"
```

| Variable | Value |
|---|---|
| `VSFLEET_CONTEXT` | The context whose password is being resolved |
| `VSFLEET_CREDENTIAL_REF` | The reference being resolved, e.g. `exec:/usr/local/bin/vsfleet-credential` |

The helper receives no standard input. A hung helper is bounded by its
context's `--timeout`.

### Without an OS keyring

Headless servers, SSH sessions, WSL, and containers may lack an OS keyring.
The setup wizard and the TUI form detect this and offer `prompt`, `env`,
`file`, and `exec` instead. `config set-context --password-stdin` falls back
to `credential = "prompt"` with a warning when the keyring cannot be
written.

### When a source stops answering

A missing variable, file, or helper leaves the context configured but unable
to connect, with the cause in the error, such as
`environment variable LAB_VC_PW is not set (env:LAB_VC_PW)`. Restore the
source or edit the reference; in the TUI,
[diagnosis](tui.md#a-password-source-that-is-gone) offers both.

## Network routes

| Transport | Behavior |
|---|---|
| `direct` | Direct TCP connection with local DNS resolution |
| `socks5` | SOCKS5 proxy; `--remote-dns` resolves through the proxy |
| `http` | HTTP CONNECT forward proxy |
| `https` | HTTPS CONNECT forward proxy with TLS |

Proxy authentication can use a separate credential reference of any scheme
above, set with `--proxy-credential`. The unattended setup flags are documented
by `vsfleet config set-context --help`.

## SSH

<span id="ssh-routes"></span>
<span id="three-complementary-routing-sources"></span>

The TUI's [SSH action](tui-actions.md#ssh) runs plain `ssh <address>`; set
users, keys and routes in `~/.ssh/config`. Older releases read an `[ssh]`
table here. It is now ignored, and the TUI says so at startup until you
remove it.

## TLS policies

| Policy | Behavior |
|---|---|
| `system` | Verify against the system trust store |
| `thumbprint` | Pin a SHA-256 or SHA-1 certificate fingerprint |
| `insecure` | Disable verification; use only when strictly necessary |

When `--tls thumbprint` has no explicit thumbprint, the setup wizard fetches the
remote certificate, displays its fingerprints, and pins the selected value.

## Capture database path

<span id="assessment-history-path"></span>

The capture database is separate from `config.toml` and defaults to
`<user-config-dir>/vsfleet/history.db`. Set `VSFLEET_DB` or pass `--db` to
override it. The database contains inventory,
identifiers, paths, annotations, snapshot metadata, and coverage, but never
credentials or session cookies.

## vSphere permissions

Give vsfleet its own account, such as `svc-ro@vsphere.local`, with the
built-in ReadOnly role assigned at the vCenter root and propagated to
children. vsfleet never changes anything, but only the role makes vCenter
enforce that; do not use an administrator account.

ReadOnly covers connection tests and standard inventory collection, including
datastore backing identity. Objects the role cannot see are left out by
vCenter, not reported as errors, so assign it at the root. Optional reads
need extra privileges:

| Feature | Additional privilege |
| --- | --- |
| `capture run --browse-datastores`, `--datastore-file-inventory`, interactive datastore browser | `Datastore.Browse` on the inspected datastores |
| `capture run --include-licenses` | `Global.Licenses`, absent from the built-in ReadOnly role |

These paths remain read-only. Without browsing or its privilege, zombie-VMDK
health is not evaluated. Denied license collection reports `unavailable`,
never zero licenses; see [license metadata](licensing.md). File inventory
exports paths and filenames; review [its limits and privacy notes](exports.md#datastore-file-inventory-vfileinfo)
before enabling it.
