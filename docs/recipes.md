# Operator Recipes

## Estate-wide search with `jq`

Find VMs matching an OS name and print their context, name, and IP address:

```sh
vsfleet vm list --all-contexts -o json | \
  jq -r '.[] | select(.Name | test("ubuntu"; "i")) | "\(.Context)\t\(.Name)\t\(.IPAddress)"'
```

## Unattended setup in CI

Pass the vCenter password through standard input. It is never written to the
configuration file:

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

For headless systems, prefer `--credential prompt` when no native keyring is
available and supply the prompt through an explicitly controlled automation
boundary.

## Customer enclave through SOCKS5

Resolve a hostname inside an isolated network through a bastion:

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

## Migration watcher

Use a short refresh interval to monitor VM placement changes interactively:

```sh
vsfleet --refresh 3s
```

For durable comparisons and scheduled checks, capture assessments and use the
[assessment policy workflow](assessments.md#compare-runs) instead.
