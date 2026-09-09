# Terminal UI

Run `vsfleet` to browse your estate interactively. The Bubble Tea interface
provides dense resource tables, immediate context switching, diagnostics, and
quiet background refresh.

## Browse screen

| Workflow | Key | Action |
|---|---|---|
| Resource switching | `1`–`7` | Jump to VMs, templates, hosts, clusters, datastores, networks, or vApps |
| Resource switching | `h` / `l`, `←` / `→` | Cycle between resource tabs |
| Row navigation | `k` / `j`, `↑` / `↓` | Move through rows |
| Row navigation | `Enter` | Open the highlighted detail inspector |
| Scope and contexts | `c` | Open context management |
| Scope and contexts | `a` | Toggle current context/all contexts |
| Search and filter | `/` | Filter the current table |
| Search and filter | `Tab` | Widen the filter to an estate-wide search |
| Search and filter | `Esc` | Clear the filter and restore the normal view |
| Operations | `r` / `R` | Reload the current/all contexts |
| Operations | `d` | Diagnose the selected row's vCenter |
| Help and exit | `?` / `q` | Show key reference / quit |

## Detail pane actions

Opening a row (`Enter`) puts a cursor on the detail pane itself: `↑`/`↓` move
between the object's own header and each field that has a value, skipping
any field the table already shows as `-`. Press `Enter` on the focused line
to act on it — a field with exactly one action runs it immediately, and one
with several opens a short list to choose from.

| Where the cursor is | What `Enter` offers |
|---|---|
| A VM's or host's own header | SSH, open in the vSphere/Host Client, copy the managed object reference |
| A VM with an IP address | Add it as a vCenter context, or switch to its existing context |
| A VM's IP address | SSH to it, copy an `ssh user@ip` command, copy the value |
| A host, datastore, network, or cluster's own header | "Show VMs on this …" — narrows the VM table to exactly what belongs to it |
| A datastore's own header | "Browse files" and "Find in datastore" — see below |
| A VM's Host or Cluster field | Jump straight to that host's or cluster's own row |
| Any other field | Copy the value |

An action that cannot run says why instead of doing nothing: a proxied
vCenter has no route for your own browser, so its "open in …" actions are
disabled with that reason while SSH through a supported unauthenticated
SOCKS5 or HTTP route can still work. `vsfleet demo` disables every action that would launch
a real process or browser, so the shape of the feature is visible without
touching your workstation.

SSH picks a default VM or ESXi user from `config.toml`'s `[ssh]` table (see
[Configuration](configuration.md#ssh)) when set, otherwise it falls back to
`~/.ssh/config` and your local username the way `ssh` always does. Failed SSH
sessions retain the final diagnostic line from OpenSSH in the footer, rather
than reducing the cause to exit status 255. A jump
("Show VMs on this host") stays on the table until `Esc` clears it, which it
does before clearing anything else.

## Datastore file browser

"Browse files" on a datastore's header opens a read-only file browser. It is
inspection only: there is no delete, rename, move, upload, mkdir, or download,
and normal datastore inventory still makes no filesystem queries at all.

Navigation is lazy. Opening the browser reads the datastore's root directory
and nothing else; entering a folder reads that folder and nothing else. The
whole tree is never enumerated and never held in memory.

| Key | Action |
|---|---|
| `Enter` | Open the highlighted folder, or inspect a file |
| `Esc` | Go up one directory, close the browser at the root, or stop a request still running |
| `/` | Filter the current directory by name |
| `f` | Find in datastore — a recursive search |
| `y` | Copy the selected entry's `[datastore] path` |

`f` (or "Find in datastore" from the header) is the one recursive operation,
and it only ever runs because you asked for it. Type a name or a glob such as
`*.iso`. Progress is visible while it runs, `Esc` cancels it, and results are
capped — a search that hit the cap says so beside its results rather than
presenting a partial answer as a complete one. `Enter` on a result opens the
directory containing it.

Press `Enter` on a file to open its detail inspector. For VMDKs, vsfleet
checks the VMs and templates currently attached to the datastore on demand and
shows the matching backing path, disk label, and an exact jump target. The
lookup is cached only while that datastore workspace is open; ordinary
inventory and directory browsing do not retrieve VM device configuration.

When assessment history is available, the inspector also shows the newest
finished assessment for the current vCenter, including its run number and
timestamp. Referenced, verified-unreferenced, suspected, and
referenced-other-context states come from that point-in-time evidence. Missing,
stale, denied, or truncated evidence remains
`unknown-incomplete-coverage` and is never presented as proof that a current
file is orphaned. A live lookup with no match is likewise not an orphan verdict
when the VM scan is partial.

Enter on a listed reference jumps to the exact VM or template in its context;
if that context or object is no longer present, the reason remains visible.

Failures are shown as failures. A datastore no host can reach disables the
action with that reason, and a directory that cannot be read reports the
vCenter's own message rather than appearing to be empty. Browsing needs
`Datastore.Browse` (see
[Configuration](configuration.md#vsphere-permissions)); it is the same
read-only privilege `vsfleet assessment run --browse-datastores` uses, and the
two workflows are otherwise independent.

### Real-vSphere validation gate

Before marking relationship-aware browsing complete, validate it against a
real read-only vSphere account. Record the vCenter/ESXi versions and results
in the release PR or issue for a nested VMFS or NFS datastore. Cover a base
VMDK, snapshot-chain file, template disk, shared/multi-reference disk,
unreferenced descriptor, missing `Datastore.Browse`, and partially unreadable
VM configuration. Confirm path metadata, UNKNOWN/partial rendering,
cancellation, exact VM/template jumps, and that no datastore or VM mutation is
possible. Until this record exists, real-vSphere validation remains an open
manual acceptance item.

## Contexts and vApps

On the Contexts screen (`c`), use `Enter` to select a context, `a` for all
contexts, `n`/`e`/`x` to add/edit/remove, `d` to diagnose, and `Esc` to return.

Press `Enter` on a vApp to open its summary and expanded member hierarchy. Use
the arrow keys to select nested vApps, VMs, and resource pools; `Enter` opens a
VM detail inspector and `Esc` returns to the previous level. A VM's header has
the same SSH and copy actions as a regular VM, plus an action to seed a new
vCenter context from its IP. The parent context's route is copied, and the
saved context records the VM's managed object reference; once saved, the
member row is annotated with the context name.

## History workspace

Press `H` to open the History hub, which contains Changes, Trends, Runs, and
Health. Use `Tab`/`Shift+Tab` to switch panes. Health shows the migration
verdict, categories, and default read-only findings for the latest stored
assessment; `↑`/`↓` scroll the pane. Use `vsfleet health` when thresholds need
tuning.

Changes opens on the newest two assessments and puts the stored runs on a run
axis at the top of the pane, oldest to newest, with the two ends of the
comparison marked `b` and `t`:

| Action | Keys | Notes |
| --- | --- | --- |
| Choose which end moves | `b` / `t` | The active end is drawn in upper case |
| Move that end | `←` / `→` | One run older or newer; the other end is stepped over |
| Reach a distant run | `R` | Opens the full run list for the active end |
| Swap the ends | `s` | |
| Clip to shared coverage | `c` | Moves the baseline to the newest older run that reached the same vCenters as the target |
| Filter by impact | `1`-`4`, `0` clears | blocks, sizing, growth, churn |

Under the axis, a coverage matrix shows which vCenters each run actually
reached — `●` reached, `✕` not compared, `·` no record. It collapses to a
single line when every visible run saw the same vCenters, and appears in full
when they did not: a run that could not reach a site is why a comparison
quietly stops being estate-wide, and `c` is the one-key fix.

The change stream below is ordered by what a change means for a migration
rather than by object name. Anything that has to be dealt with before a
cutover — a vanished VM or host, a snapshot older than 30 days, a changed
migration configuration — is ranked first and is never rolled up. Identical
changes elsewhere are rolled into one line with the shared name prefix and a
count (`k8s-worker-0… 12 VMs`), and the inspector names every member. `Enter`
opens a change on a narrow terminal; on a wide one the inspector is already
beside the stream. From a change, `h` opens a VM timeline and `a` includes
unchanged observations.

Press `n` to capture the vCenter in scope. In Runs, `e` edits a label, `N` an
operator note, and `p` toggles a pin. Captures run in the background while
normal inventory remains available.

## Filtering and searching

1. Press `/` to filter the current view by name.
2. If matches exist outside the current view, the query line reports them:
   `/ubuntu   0 here · 2 in the estate — tab to widen`.
3. Press `Tab` to search every vCenter and resource kind in cached inventory.
4. Press `Tab` again or `Esc` to narrow back while preserving the query.

## Refresh and cache behavior

- The selected context refreshes every 20 seconds by default.
- Successfully visited inactive contexts refresh every 200 seconds.
- Unvisited contexts are never contacted until explicitly accessed.
- A context waiting for interactive credentials is retried only when selected
  or explicitly reloaded; timers never take over the terminal with a prompt.
- Failed refreshes retain cached inventory and show a visible warning.
- Use `--refresh 5s` to accelerate polling or `--refresh -1` to disable timers.
- A context that takes longer to read than the interval is polled less often,
  at roughly three times its last load, so a large estate is never asked
  again while its previous answer is still fresh.

## Large estates

Reading thousands of virtual machines takes longer than any fixed deadline can
usefully allow for, so the interface does not impose one. A load fails when it
stops making progress, not when it takes a while:

- Rows appear a page at a time as they arrive, rather than the tab staying
  empty until the whole estate has been read. The cursor keeps its place on
  the same machine as the list fills in around it.
- The interface retrieves only what it shows. Virtual disks, guest NIC
  bindings and snapshot trees — by far the most expensive part of reading a
  virtual machine — are fetched by `vsfleet assessment capture`, which needs
  them, and not by browsing, which does not.
- The inventory path index is reused for two minutes rather than rebuilt on
  every refresh. A machine created in that window is placed through its parent
  folder, so it appears with its datacenter and path intact.
- `--timeout` still bounds connecting and authenticating; it no longer bounds
  how long enumeration may take.

If a password is needed, the pane remains usable and displays `credentials
required`; select or reload that context to open the masked prompt.
