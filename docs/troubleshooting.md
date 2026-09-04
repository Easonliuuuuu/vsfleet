# Troubleshooting

Start with the context details, then run a complete test or the staged doctor:

```sh
vsfleet context show prod
vsfleet context test prod
vsfleet doctor prod
vsfleet status
```

## The eight-stage diagnostic pipeline

`vsfleet doctor` verifies the path to a vCenter in strict order:

1. **Configuration:** Validate TOML properties and context settings.
2. **Credentials:** Resolve a keyring reference or confirm prompt mode.
3. **Routing and proxy:** Confirm proxy reachability and authentication.
4. **DNS resolution:** Resolve locally or through the configured proxy.
5. **TCP handshake:** Establish transport to port 443.
6. **TLS negotiation:** Validate the trust chain or pinned thumbprint.
7. **SSO authentication:** Authenticate the configured vSphere user.
8. **API handshake:** Probe vSphere ServiceContent with read access.

The first failed stage identifies the boundary to investigate. Use the context
configuration guide to correct routes, credentials, or TLS policies.

## Partial failures

Estate-wide commands intentionally keep data from healthy contexts when another
vCenter is offline or times out. A failed context is reported independently;
cached TUI inventory remains visible with a stale-data warning.

If a context needs an interactive password, select it or reload it explicitly.
Background refresh never interrupts the terminal with a password prompt.

## Assessment database checks

If history operations fail, inspect the local ledger without contacting vCenter:

```sh
vsfleet assessment doctor
vsfleet assessment backup ./history-backup.db
```

The database is local and separate from configuration. Check its permissions and
available disk space, then use a consistent backup before any restore or cleanup.
