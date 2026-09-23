# Nested vSphere lab

This is the runbook for a personal, isolated nested-vSphere lab used to
validate real VMware API semantics that [`vcsim`](testing.md#out-of-process-vcsim-integration)
cannot faithfully reproduce: VMXNET3 UPT vs. actual passthrough, host CPU
capacity semantics, local vs. shared storage behavior, real
datastore-browser permissions, version-specific vCenter/ESXi property
behavior, and DVS/DVPG detail. It sits between `vcsim` and
production-derived anonymized fixtures in the verification ladder:

```text
unit tests
    ↓
vcsim
    ↓
personal nested vSphere lab      ← this document
    ↓
production-derived anonymized fixtures
```

It complements the real-vSphere acceptance work tracked in
[#155](https://github.com/Easonliuuuuu/vsfleet/issues/155); this document is
the concrete setup/runbook, tracked in
[#156](https://github.com/Easonliuuuuu/vsfleet/issues/156).

> [!IMPORTANT]
> **Licensing and media disclaimer.** Use only legitimately obtained,
> personally authorized VMware installation media and license/evaluation
> entitlements. Do not reuse employer credentials, licenses, private ISOs,
> or infrastructure for this lab unless explicitly authorized. This is not
> a production-like performance lab; it exists solely to expose real
> vSphere configuration metadata to VSFleet for read-only validation.

## Hardware assumptions

The reference host is a personal workstation, not shared or production
infrastructure:

- 64 GB physical RAM
- a CPU with Intel VT-x/EPT or AMD-V/RVI, enabled in firmware/BIOS
- VMware Workstation Pro (or an equivalent outer hypervisor capable of
  nested virtualization) with nested virtualization exposed to the ESXi VMs
- several hundred GB of free SSD/NVMe space, thin-provisioned
- legally obtained VMware installation media/evaluation entitlement

Confirm all of the above before starting Phase 1.

## Target architecture

Start with one vCenter and two nested ESXi hosts. Do not add a third host
unless a specific validation scenario requires it.

```text
Physical PC — 64 GB RAM
│
└── VMware Workstation Pro
    │
    ├── vcsa.lab.local
    │
    ├── esxi01.lab.local
    │   ├── tiny test VMs
    │   └── local datastore
    │
    ├── esxi02.lab.local
    │   ├── tiny test VMs
    │   └── local datastore
    │
    └── storage.lab.local
        └── NFS export mounted by both ESXi hosts
```

Inside vCenter:

```text
DC-Lab
└── Cluster-Lab
    ├── ESXi-01
    └── ESXi-02

Datastores
├── local-esxi01
├── local-esxi02
└── nfs-shared

Networking
└── dvSwitch-Lab
    ├── dvpg-vlan100
    ├── dvpg-vlan200
    └── dvpg-mgmt
```

## Resource allocation

Reference allocation for a 64 GB host. Optimize for correctness and API
behavior, not guest workload performance — most nested VMs exist only to
expose VMware configuration metadata to VSFleet.

| Component | RAM | vCPU | Notes |
| --- | ---: | ---: | --- |
| physical host reserve | 10–12 GB | — | OS + VSFleet/dev tools |
| VCSA Tiny | ~14 GB | 2–4 | smallest supported lab/test deployment |
| ESXi-01 | ~10 GB | 4 | nested virtualization enabled |
| ESXi-02 | ~10 GB | 4 | nested virtualization enabled |
| NFS/Linux VM | 2 GB | 1–2 | shared datastore fixture |
| nested test VMs | 8–12 GB total | overcommitted | mostly 512 MB–1 GB metadata fixtures |
| headroom | ~6–10 GB | — | avoid host swapping |

## Network diagram and IP plan

Create an isolated host-only network, for example `192.168.150.0/24`:

```text
192.168.150.1    physical host
192.168.150.10   vcsa.lab.local
192.168.150.11   esxi01.lab.local
192.168.150.12   esxi02.lab.local
192.168.150.20   storage.lab.local
```

- [ ] VSFleet on the physical host can reach vCenter
- [ ] ESXi hosts can reach vCenter and the shared-storage VM
- [ ] the lab is not exposed directly to the public Internet
- [ ] no inbound Internet port-forwarding to VCSA/ESXi
- [ ] local DNS/hosts-file entries used for the lab are recorded below

Record the hosts-file/DNS entries actually used here once the lab is built.
Optional later separation can add management/storage/vMotion-style networks
if a concrete test needs them; it is not required for the baseline.

## Nested virtualization settings

Before creating any ESXi VM, confirm the outer hypervisor exposes nested
virtualization (in VMware Workstation Pro: "Virtualize Intel VT-x/EPT or
AMD-V/RVI" enabled per-VM) and that the physical CPU/firmware settings from
[Hardware assumptions](#hardware-assumptions) are already in place.

## ESXi installation notes

Suggested starting shape per ESXi VM:

```text
4 vCPU
10 GB RAM
40+ GB thin system disk
1+ virtual NIC
nested virtualization enabled
```

- [ ] install ESXi-01
- [ ] install ESXi-02
- [ ] assign static management addresses
- [ ] configure hostnames/DNS
- [ ] verify both hosts are reachable from the physical host
- [ ] verify each host exposes a local datastore
- [ ] record ESXi build/version used (see [version/build matrix](#vmware-versionbuild-matrix))

Do not tune nested ESXi for benchmark performance.

## VCSA deployment notes

- [ ] deploy a small VCSA (lab/test sizing) on one nested ESXi host initially
- [ ] use `vcsa.lab.local` (or documented equivalent)
- [ ] create `DC-Lab`
- [ ] create `Cluster-Lab`
- [ ] add both nested ESXi hosts to vCenter
- [ ] verify VSFleet can authenticate to the VCSA `/sdk` endpoint
- [ ] record vCenter build/version (see [version/build matrix](#vmware-versionbuild-matrix))

### VSFleet read-only account/privilege setup

Create a dedicated read-only VSFleet account/role rather than using the
administrator account for routine validation.

- [ ] document required privileges here once the role is created
- [ ] verify normal VSFleet inventory works with that account
- [ ] keep administrator credentials out of repo/test fixtures

Reference the account from a documented lab context, without committing a
password:

```toml
[[contexts]]
name = "lab-vcenter"
endpoint = "https://vcsa.lab.local"
username = "vsfleet-readonly@..."
```

## NFS setup

Create a minimal Linux VM that exports NFS storage and mount it on both
ESXi hosts as the same shared datastore:

```text
storage.lab.local
└── /srv/vsphere
```

- [ ] create the NFS server VM
- [ ] export a dedicated lab path
- [ ] mount it on ESXi-01
- [ ] mount it on ESXi-02
- [ ] verify vCenter sees one shared datastore identity
- [ ] retain local datastore(s) on each host as contrast cases

This specifically supports regression validation that `local datastore !=
shared datastore`, that the shared datastore is visible from both hosts,
and that stable datastore identity survives display-name ambiguity.

## DVS/DVPG setup

Create a real vSphere Distributed Switch if available in the active
lab/evaluation feature set:

```text
dvSwitch-Lab
├── dvpg-vlan100
├── dvpg-vlan200
└── dvpg-mgmt
```

- [ ] attach both ESXi hosts
- [ ] create at least two distributed port groups
- [ ] use deliberately different VLAN IDs
- [ ] record MTU
- [ ] record teaming/uplink policy
- [ ] record security policy
- [ ] create at least one intentionally incomplete/missing host/PG mapping
      if safe and useful

Use this to validate `vsfleet network compare ...` and
`vsfleet assessment network-readiness ...`. No actual workload migration is
required.

## Test-VM catalogue

Create small VMs whose purpose is to expose distinct vSphere
configurations. At minimum:

| VM | Purpose |
| --- | --- |
| `vm-normal` | BIOS/default firmware, ordinary VMXNET3, generated MAC, normal VMDK |
| `vm-uefi-secure` | UEFI, Secure Boot, vTPM where available |
| `vm-snapshot` | one or more snapshots, delta VMDKs visible through real vSphere APIs |
| `vm-iso` | mounted CD/DVD ISO |
| `vm-manual-mac` | manually assigned MAC |
| `vm-reservations` | CPU and memory reservation/limit |
| `vm-cpu-topology` | multiple vCPUs, explicit sockets × cores topology |
| `vm-upt` | normal VMXNET3 with UPT compatibility configured where the platform allows it — must remain distinguishable from actual SR-IOV/PCI passthrough |

Optional fixtures when hardware/platform support exists: RDM, SR-IOV, PCI
passthrough, vGPU. Do not block the baseline lab on hardware-dependent
passthrough scenarios.

### Datastore browser fixtures

Populate the shared datastore with a small known directory tree through
normal VM/ISO usage or safe lab files, for example:

```text
nfs-shared/
├── ISO/
│   └── test.iso
├── vm-normal/
│   └── vm-normal.vmdk
└── nested/
    └── target-folder/
        └── target-file.txt
```

## Validation checklist

### VSFleet acceptance baseline

Run and record the output of at least:

```sh
vsfleet status
vsfleet doctor lab-vcenter
vsfleet vm list --context lab-vcenter
vsfleet host list --context lab-vcenter
vsfleet datastore list --context lab-vcenter
vsfleet network list --context lab-vcenter
vsfleet assessment run --context lab-vcenter --browse-datastores
vsfleet assessment findings latest --context lab-vcenter
vsfleet assessment readiness latest --context lab-vcenter
vsfleet assessment capacity latest --context lab-vcenter
```

Also validate the TUI against the real lab:

- [ ] inventory browsing
- [ ] context authentication
- [ ] datastore browser
- [ ] recursive datastore search
- [ ] VM/detail navigation
- [ ] History capture/readback

### Datastore browser/search validation against the real API

- [ ] root directory listing
- [ ] subdirectory navigation
- [ ] recursive file search
- [ ] recursive folder search where supported
- [ ] file metadata/modified time/size
- [ ] cancellation/timeout behavior
- [ ] permission-denied behavior using a restricted role if practical
- [ ] VMDK → VM relationship resolution
- [ ] orphan evidence remains explicit/uncertain when browse coverage is
      incomplete

### Semantic cross-checks

For each risky VMware field, compare `vSphere Client`/PowerCLI/`govc`
output (expected truth) against VSFleet output, and record the exact
vCenter/ESXi build when accepting a semantic behavior.

**VM:** firmware, Secure Boot, vTPM, VMXNET3/UPT, MAC assignment, CPU
topology, resource reservations/limits, CD/ISO, snapshots.

**Host:** physical core count, per-core MHz, normalized total CPU capacity,
local datastore/storage evidence, shared datastore evidence,
available path/multipath fields.

**Network:** DVS identity, DVPG identity, VLAN, MTU, teaming/security
fields, host attachment/coverage.

**Datastore:** identity, local vs. shared semantics, browse/list/search
behavior, same-name handling if the lab creates ambiguous names.

### Permission matrix

Use dedicated lab roles/users to create safe failure cases without
breaking infrastructure:

```text
full read-only inventory                  success
host inventory but limited host config    partial/UNKNOWN where appropriate
datastore visible but browse denied       explicit browse failure
one evidence class unavailable            must not become PASS/READY
```

- [ ] document roles/privileges used
- [ ] verify missing evidence reduces confidence
- [ ] verify denied evidence never becomes a clean result
- [ ] capture useful examples for [#155](https://github.com/Easonliuuuuu/vsfleet/issues/155) failure-injection/replay tests

## Lab limitations

Nested vSphere validates real VMware software/API semantics, but it is not
equivalent to physical enterprise hardware. This lab does **not** validate:

- physical FC/iSCSI HBA behavior
- real SAN multipathing
- physical NVMe/controller semantics
- production SR-IOV NIC behavior
- GPU/vGPU behavior without matching hardware
- hardware-vendor-specific quirks
- production performance/capacity

Those cases remain covered through anonymized production-derived fixtures
or other authorized real hardware.

## Teardown/rebuild notes

The lab is disposable by design. Record here, once built, whatever is
needed to destroy and recreate it from this document alone:

- [ ] outer-hypervisor VM inventory to delete (VCSA, ESXi-01, ESXi-02, NFS VM)
- [ ] host-only network/DNS entries to remove
- [ ] any deviations made from this runbook while building it, so the next
      rebuild matches reality

## VMware version/build matrix

Record the exact versions/builds used for the acceptance run so a semantic
finding can be tied to a specific release:

| Component | Version | Build | Notes |
| --- | --- | --- | --- |
| VMware Workstation Pro | | | outer hypervisor |
| ESXi-01 | | | |
| ESXi-02 | | | |
| VCSA | | | |
| VSFleet | | | commit/version used for the acceptance run |

## Optional automation after manual baseline works

Do not automate the entire lab before the manual design is proven. Once
stable, consider scripts for creating the outer ESXi VMs, generating
hosts-file/DNS entries, configuring the NFS fixture, creating predictable
test-VM metadata, and generating an acceptance report. Keep
destructive/rebuild automation isolated to the personal lab; VSFleet itself
remains read-only.
