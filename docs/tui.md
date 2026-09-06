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
| A VM's Host or Cluster field | Jump straight to that host's or cluster's own row |
| Any other field | Copy the value |

An action that cannot run says why instead of doing nothing: a proxied
vCenter has no route for your own browser, so its "open in …" actions are
disabled with that reason while SSH — which can route through the same
proxy — still works. `vsfleet demo` disables every action that would launch
a real process or browser, so the shape of the feature is visible without
touching your workstation.

SSH picks a default user from `config.toml`'s `[ssh]` table (see
[Configuration](configuration.md#ssh)) when set, otherwise it falls back to
`~/.ssh/config` and your local username the way `ssh` always does. A jump
("Show VMs on this host") stays on the table until `Esc` clears it, which it
does before clearing anything else.

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
Health. Use `←`/`→` to switch panes. Health shows the default read-only health
findings for the latest stored assessment; use `vsfleet health` when thresholds
need tuning. Changes compares the newest two assessments; `b`
and `t` choose a different baseline or target, and `Enter` opens a change. From
change detail, `h` opens a VM timeline and `a` includes unchanged observations.

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
