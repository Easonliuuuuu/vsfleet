# Synthetic estate contract

Read this reference when changing fixtures, validating the UI interactively, or explaining what the default testbed simulates.

## Boundaries

The default testbed is an in-process implementation of the TUI backend. It does not listen on a port or implement VMware's SOAP/REST APIs. Its `.example` endpoints are display values and must not be described as connectable servers.

It is intentionally:

- offline and independent of DNS;
- read-only;
- deterministic across launches;
- isolated from the normal VSFleet config, UI state, credential prompt, and operating-system keyring.

## Contexts

| Context | Site | Displayed route | State | Purpose |
|---|---|---|---|---|
| `prod-vc` | Taipei | direct | healthy, 84 ms | primary healthy inventory |
| `edge-vc` | Hsinchu | SOCKS5 with remote DNS | healthy, 127 ms | alternate-route inventory |
| `dr-site` | DR | HTTP CONNECT | failed | partial failure and diagnosis UI |

The failed site reports a refused proxy connection and skips the later DNS, TCP, TLS, authentication, and API checks.

## Inventory per healthy context

- Three VMs: `api-01`, `postgres-01`, and powered-off `build-runner-03`.
- Two templates: `ubuntu-24.04-golden` and `windows-2025-core`.
- Two hosts: `esxi-01` and `esxi-02`.
- Two clusters: `compute-a` and `compute-b`.
- Two datastores: `nvme-01` (8 TiB, 3 TiB free) and `san-01` (24 TiB, 9 TiB free).
- Two networks: `frontend-vlan-120` and `backend-vlan-240`.

The two healthy contexts use different location and subnet values while retaining the same resource shape, making all-context aggregation predictable.

## Fixture-change invariants

- Object IDs must remain unique within a context and stable between runs.
- Every object's `Location.Context` must match its inventory context.
- Paths must identify the displayed datacenter, resource kind, and object.
- VM references to hosts, clusters, and datastores should resolve to fixture names unless a broken-reference scenario is intentional and documented.
- Capacity and free-space values must be plausible, with free space no greater than capacity.
- At least one powered-off VM and one failed context must remain available for UI coverage.
- Use reserved/example names and non-sensitive data only.

## Interactive acceptance checks

After `make-testbed.sh --mode verify`, launch in a terminal and check only what the change can affect:

1. The sidebar lists all three contexts and the initial `prod-vc` inventory loads.
2. `a` aggregates the two healthy contexts while the failed site remains visible.
3. VM, template, host, cluster, datastore, and network tabs render non-empty rows.
4. Filtering for `postgres` selects the expected VM.
5. Diagnosing `dr-site` shows the proxy failure and skipped downstream stages.
6. Add, edit, and remove remain visibly unavailable/read-only.

For non-interactive validation, verify that the shell hook resolves `command -v vsfleet` to the isolated testbed launcher. Do not automate the full-screen TUI through ordinary stdin; it requires a real pseudo-terminal.
