# Operator Recipes

## Estate-wide search with `jq`

Find VMs matching an OS name and print their context, name, and IP address:

```sh
vsfleet vm list --all-contexts -o json | \
  jq -r '.[] | select(.Name | test("ubuntu"; "i")) | "\(.Context)\t\(.Name)\t\(.IPAddress)"'
```

## Unattended collection

There is no keyring and no terminal in a container, a systemd unit or a CI job,
so point the context at where the password actually lives. The reference is
written to `config.toml`; the password is not.

### CI, with the secret in the environment

```sh
vsfleet context add \
  --name prod \
  --endpoint https://vcsa.example.internal \
  --username administrator@vsphere.local \
  --credential env:VCENTER_PASSWORD \
  --tls system

VCENTER_PASSWORD="$CI_VCENTER_SECRET" \
  vsfleet assessment run --all-contexts --fail-on-partial
```

### systemd, with the secret in a file

`LoadCredential=` puts the secret in a file only this unit can read, and
`$CREDENTIALS_DIRECTORY` is where systemd mounts it:

```ini
[Service]
Type=oneshot
LoadCredential=vcenter:/etc/vsfleet/vcenter.password
ExecStart=/usr/local/bin/vsfleet assessment run --all-contexts --fail-on-partial
```

```sh
vsfleet context add --name prod \
  --endpoint https://vcsa.example.internal \
  --username administrator@vsphere.local \
  --credential file:%d/vcenter \
  --tls system
```

The same shape works for a Kubernetes secret mount (`file:/var/run/secrets/...`)
and a Docker secret (`file:/run/secrets/...`).

### A secret manager, through a helper

`exec:` runs a program and reads the password from its standard output. It is
given the context name, so one helper serves the whole estate:

```sh
#!/bin/sh
# /usr/local/bin/vsfleet-credential
exec vault read -field=password "secret/vcenter/$VSFLEET_CONTEXT"
```

```sh
vsfleet context add --name prod \
  --endpoint https://vcsa.example.internal \
  --username administrator@vsphere.local \
  --credential exec:/usr/local/bin/vsfleet-credential \
  --tls system
```

The reference names a program and nothing else — no arguments, no shell — so
put any arguments in the helper. See
[Configuration](configuration.md#unattended-sources) for the details.

### Reacting to a partial estate

A scheduled capture should be able to tell "the whole estate answered" from
"one site was down". `--fail-on-partial` makes that a distinct exit code:

```sh
vsfleet assessment run --all-contexts --fail-on-partial
case $? in
  0) ;;                                  # complete
  3) echo "some vCenters did not answer" ;;
  *) echo "the capture could not run" ; exit 1 ;;
esac
```

Without the flag a partial capture exits 0, so an existing job keeps its
behavior. See [the exit-code table](commands.md#exit-codes) for the contract.

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
