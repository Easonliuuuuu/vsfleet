# Migration and capacity planning

## Migration readiness

`vsfleet analyze readiness` returns `ready`, `blocked`, or `unknown` from the
[health rules](health.md) over a stored capture, the most recent unless `--run`
names another. Warning/critical migration findings block;
informational findings are advisories. `--fail-on-blockers` exits 2 for blockers.

Failed or missing collectors force `unknown`, with blind vCenters named in
`Not evaluated`.

What each kind of VM configuration means for readiness:

| Evidence | Finding severity |
| --- | --- |
| RDM, shared disks, vTPM, PCI/SR-IOV/vGPU passthrough | Blocking warning |
| Firmware/Secure Boot, CPU socket/core topology, CPU/memory reservations and limits, manual MACs, floppies, extension ownership, enabled DRS rules naming the VM | Informational advisory |

Ordinary VMXNET3 adapters do not count as passthrough. VMs whose
configuration vCenter did not return, such as orphaned VMs, are counted under
`Not evaluated` and the rest are still checked; a context with no VM
configuration at all, such as an RVTools import, keeps the verdict
`unknown`.

## VM decommission review

`vsfleet analyze decommission vm/NAME` combines VM safety gates
and stored topology. It reports only and performs no decommissioning action.

- `blocked`: powered-on/suspended VM, snapshots, connected CD-ROM/USB, or
  inaccessible, orphaned, disconnected, or invalid connection state.
- `unknown`: missing evidence, or an unresolved reference of the VM's own: a
  datastore, network, host or cluster it uses, or the LUN behind a raw device
  mapping. The message names the reference. A LUN on the VM's host that no
  datastore maps to does not count.
- `no-blockers`: every collected check passed. This does not authorize
  deletion.

An enabled DRS rule that names the VM is an advisory: removing the VM
changes what the rule enforces for its other VMs. It never blocks.

Ownership, backup policy, and application dependencies are not assessed;
check them yourself. `--fail-on-blockers` exits 2 only for `blocked`.

## Topology and dependency queries

`analyze topology`, `analyze deps`, and `analyze blast-radius` query stored
evidence offline. Each takes a subject as `KIND/NAME` or `KIND NAME`, and
`deps` and `blast-radius` take `--depth` (1 to 5). Objects in different
vCenters are treated as the same only when their IDs match, such as a VM's
UUID or a datastore's backing; a match by name alone is marked `inferred`.

| Confidence | Meaning |
| --- | --- |
| `complete` | Required collections answered; all relationships resolved |
| `partial` | Blind collection, unresolved reference, or reconstructed network |
| `unknown` | The subject could not be resolved, or no context had the evidence |

An ambiguous name returns every match; narrow it with `-n`. Captures from
older vsfleet versions give at most `partial` network results.

## Cross-cluster network readiness

`vsfleet analyze readiness network SOURCE TARGET` compares cluster-reachable
networks offline, with each cluster given as `cl/NAME` or `NAME`: VLAN, parent switch, MTU, teaming, security, uplinks, and
host coverage. Matching uses VLAN, then name. JSON separates matched pairs,
source-only gaps, target-only networks, and field differences. Source-only
gaps list affected VMs using topology attachment evidence and confidence.

The table output is the verdict; `-o wide` adds the comparison behind it, and
`-o json` always carries both:

| Verdict | Meaning |
| --- | --- |
| `ready` | No attached VM loses a source network; no hard mismatch |
| `blocked` | Attached mapping gap or VLAN, MTU, security, or host-coverage mismatch |
| `unknown` | Unresolved cluster or blind relevant collection |

Confidence follows the [topology rules](#topology-and-dependency-queries).
`--fail-on-blockers` exits 2 for blocked results.

## Destination sizing scenarios

`vsfleet analyze sizing` checks whether stored VMs fit a proposed
destination. It reads one assessment offline and changes nothing. The result
is `allocation-based`, without a compatibility claim for any hypervisor or
cloud platform.

```sh
vsfleet analyze sizing --run pre-migration --cluster cluster-a --cluster cluster-b \
  --hosts 3 --host-cores 48 --host-ram-gib 768 --datastore-capacity 60TiB \
  --rdm-capacity 2TiB --cpu-ratio 4 --growth-pct 20 --ha-host-failures 1
```

### Inputs

Required: `--hosts`, `--host-cores` (usable per host), `--host-ram-gib`,
and `--datastore-capacity` (usable, e.g. `40TiB`). Scope containing RDMs
also requires `--rdm-capacity`.

| Option | Default |
| --- | --- |
| `--cpu-ratio` (vCPU per usable core) | 1 |
| `--memory-ratio` | 1 |
| `--growth-pct` | 0 |
| `--ha-host-failures` | 1 (N-1); 0 disables |

Scope uses `-n`/`-A` and repeatable `--cluster NAME|CONTEXT/NAME`.
Templates are excluded; powered-off VMs need `--include-powered-off`. Output
lists the excluded VMs and every input, and whether it was given or
defaulted.

### Calculations

Output prints each formula with its numbers:

| Dimension | Required | Supply |
| --- | --- | --- |
| `cpu` (vCPU) | sum(vCPU) x (1 + growth/100) | (hosts - failures) x cores x cpu-ratio |
| `memory` (GiB) | sum(configured memory) x (1 + growth/100) / memory-ratio | (hosts - failures) x RAM per host |
| `memory-reservation` (GiB) | sum(VM memory reservations) | (hosts - failures) x RAM per host |
| `datastore-capacity` (GiB) | sum(provisioned datastore-backed disk) x (1 + growth/100) | target datastore capacity |
| `rdm-capacity` (GiB) | sum(RDM LUN capacity) x (1 + growth/100) | target RDM capacity |

Verdicts use provisioned size. Shared disks and datastores or LUNs seen from
several vCenters count once. Committed and guest-filesystem use are shown for
reference, including whether committed use alone would fit.

### Verdicts

Each dimension is `fit`, `insufficient`, or `unknown`. Overall precedence is
insufficient > unknown > fit.

A dimension is `unknown` when an input is missing, the capture is not
complete, or some VM's allocation, disks or datastore were not recorded.
Missing evidence never turns `insufficient` into something better. `--fail-unless-fit` returns exit code 2
unless every dimension is `fit`.

Output also shows oversubscription, the HA allowance, and migration
blockers in scope. Storage gets no thin-provisioning or deduplication credit,
and CPU reservations are listed but not compared, because target core speed
is not an input. Check networks separately with
[network readiness](#cross-cluster-network-readiness).

### Limits

Sizing uses configured allocations without performance history or rightsizing.
It assumes homogeneous hosts and uniform growth, including RDM LUN size. It
omits per-host placement, NUMA, fragmentation, storage performance, licensing,
and overhead.
