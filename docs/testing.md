# Testing

vsfleet has three complementary test tiers. Each tier answers a different
question, so passing one is not evidence that the others are unnecessary.

## Unit and package tests

The untagged suite covers pure domain behavior, persistence, topology joins,
health/readiness rules, CLI contracts, transport, and the read-only vSphere
inventory layer:

```sh
go test -race ./...
go vet ./...
```

These tests are fast and run on every operating system in the normal CI
`build` job.

## In-process simulator tests

The existing `tests/` package starts `govmomi/simulator` in the test process.
It drives the real CLI through direct, SOCKS5, and HTTP proxy routes and is
the right tier for command behavior, credentials, timeout handling, and
read-only SOAP auditing without external dependencies.

## Out-of-process vcsim integration

The tagged suite starts one independent `cmd/vsfleet-vcsim` process per
endpoint. The launcher is built against the govmomi version in `go.mod` because
govmomi v0.56.0 ships the simulator library but not an installable
`github.com/vmware/govmomi/vcsim` module. Each process has a kernel-assigned
loopback port, TLS readiness is verified by fetching its certificate
thumbprint, and stdout/stderr are captured for CI failure artifacts.

Run it locally with:

```sh
go build -o /tmp/vsfleet-vcsim ./cmd/vsfleet-vcsim
VSFLEET_VCSIM_BIN=/tmp/vsfleet-vcsim \
VSFLEET_VCSIM_REQUIRED=1 \
VSFLEET_VCSIM_LOG_DIR=/tmp/vsfleet-vcsim-logs \
go test -tags integration -race ./tests/... -timeout 20m
```

### Fixture catalogue

The fixtures are deterministic flag sets; endpoint processes still receive
independent vCenter and VM identities.

| Fixture | Topology and purpose |
| --- | --- |
| `basic-multivcenter` | `vc-prod`: two datacenters, one cluster per datacenter, two hosts per cluster, three VMs per resource pool, three datastores, three port groups, and one vApp per cluster. `vc-edge`: one datacenter, one cluster, two hosts, one VM pool, one datastore, one port group, and one vApp. |
| `duplicate-names` | Two identical one-datacenter estates. Clustered VM names such as `DC0_C0_RP0_VM0` and local datastore names such as `LocalDS_0` collide deliberately across contexts. |
| `partial-failure` | Healthy and killable external endpoints plus a closed-port context from the existing failure helper. |
| `topology` | One datacenter, one cluster, two hosts, two VMs, two datastores, two port groups, and one vApp for exact hierarchy/attachment assertions. |
| `history` | The basic small shape, captured three times with power and naming mutations between captures. |

## What vcsim does not prove

vcsim proves process isolation, CLI behavior, inventory collection, topology
correlation, historical persistence, partial-coverage semantics, and the
certificate/credential plumbing exercised by these fixtures. It is not a
replacement for validation against real vSphere.

In particular, it does not prove physical storage paths, ESXi
kernel/storage semantics, real VMXNET3 or UPT behavior, SR-IOV/vGPU/RDM,
patch-release quirks, or actual VM migration. k3s remains deferred until a
concrete test requirement justifies adding it; containers are not a goal of
this integration tier.
