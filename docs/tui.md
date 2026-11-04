# Terminal UI

Run `vsfleet` to browse inventory, switch vCenters, and inspect resources.
Press `?` on any screen for its keys.

## Browse screen

| Key | Action |
|---|---|
| `1`–`7` | VMs, templates, hosts, clusters, datastores, networks, vApps |
| `h` / `l`, `←` / `→` | Previous/next resource tab |
| `k` / `j`, `↑` / `↓` | Previous/next row |
| `Enter` | Open selected resource |
| `c` | Manage contexts |
| `a` | Toggle current/all contexts |
| `/` | Filter table |
| `Tab` | Widen filter to estate search |
| `Esc` | Clear filter, restore view, or go up a folder |
| `s` / `S` | Open the [sort menu](#sorting) / reverse the sort |
| `r` / `R` | Reload current/all contexts |
| `d` | Diagnose selected row's vCenter |
| `space` / `t` on Networks | Fold switch / toggle tree and plain list |
| `t` on VMs or Templates | Toggle [folders](#browsing-by-folder) and plain list |
| `v` on Networks | Open [VLAN map](tui-network.md#vlan-map) |
| `H` | Open History |
| `?` / `q` | Help / quit |

The selected row is marked `▸`, so it stays visible under `NO_COLOR`.

<span id="vm-detail-dashboard"></span>
<span id="detail-pane-actions"></span>
<span id="openssh-alias-discovery"></span>
<span id="destination-and-route"></span>
<span id="datastore-file-browser"></span>
<span id="real-vsphere-validation-gate"></span>
<span id="networks-and-switches"></span>
<span id="vlan-map"></span>
<span id="host-network-page"></span>
<span id="cluster-workspace"></span>

Other screens have their own pages: [VM dashboard](tui-vm.md),
[detail actions and SSH](tui-actions.md), [datastore browser](tui-storage.md),
and [networks and clusters](tui-network.md).

## Filtering and searching

Press `/` to filter the current view by name. The query line also counts
matches elsewhere: `/ubuntu   0 VMs in prod · 2 in the estate — tab to widen`.
`Tab` searches every kind and vCenter in cached inventory; `Tab` again or
`Esc` narrows back. `↑`/`↓` move the selection while typing, and `Enter`
keeps it.

## Sorting

Press `s` to choose the order of the current tab. The menu lists the tab's
columns with a letter each: `s` then `m` sorts VMs by memory. Choosing the
current order again, or `r` in the menu, reverses it; `S` reverses it without
opening the menu.

| Tab | Sorts by |
|---|---|
| VMs | name, power, CPU, memory, IP address, host, datacenter |
| Templates | name, guest OS, CPU, memory, disk, datacenter |
| Hosts | name, state, cluster, CPU used, memory used, VMs, version |
| Clusters | name, status, hosts, cores, memory, DRS, HA, datacenter |
| Datastores | name, status, type, capacity, free, used %, datacenter |
| Networks, vApps | name, status |

Numbers start largest first and text A to Z. Values not reported sort last
either way, and ties fall back to name. Names and versions compare their
numbers, so `web-2` comes before `web-10`. The header shows the order, such as
`sort: memory ↓`, and the sorted column's heading carries the arrow. Each tab
keeps its own order, and vsfleet remembers it for the next run. Search results
sort by name or status (`s` toggles).

## Browsing by folder

Press `t` on the VMs or Templates tab to browse folders one level at a time,
as in the vSphere client. `Enter` opens a folder, `Esc` goes up, and the
message line shows where you are: `in Taipei › Prod › Web`. Folder rows show
how many VMs they hold and their vCPU and memory totals. A filter searches
the open folder and everything below it. vApps appear in their folder marked
`▣`; `Enter` opens the [vApp page](#vapp-workspace).

## Contexts and vApps

On Contexts (`c`): `Enter` selects a context, `a` selects all, `n`/`e`/`x`
add, edit or remove, `o` logs out, and `d` diagnoses. Logging out keeps
cached inventory and stops refreshing that context.

Browse-screen `a` shows only vCenters that are loaded. The others are listed
below the table with their state:

```text
○ not connected: edge-vc, dr-vc · R connects all · c then enter connects one, d diagnoses
◐ credentials required: site-3 · R asks for them · c then enter picks one
✕ old-vc: connection failed · c select, d diagnose
```

In `vsfleet demo`, `n` opens a read-only preview of the setup form.

### A password source that is gone

`env:`, `file:` and `exec:` passwords are read on every connection, so a
missing source shows its cause:

```text
✕ lab-vc: LAB_VC_PW is not set · d diagnose
✕ lab-vc: password helper vsfleet-credential failed: Vault is sealed · d diagnose
```

`d` explains how to recover: export the variable and restart vsfleet, restore
the file and press `r`, or fix the helper and press `r`. Press `e` there to
switch to another source; the form tests it before saving. See
[credentials](configuration.md#credentials).

### vApp workspace

`Enter` on a vApp shows its CPU and memory allocation, start order, and
nested members. A vApp's limit is shared by its members and can throttle VMs
that have no limit of their own. `Enter` on a member opens it; `Esc` goes
back.

Live charts sum member CPU and active memory against the vApp's limit, or
its members' total when unlimited; `▲` marks samples near the limit. Members
show peak CPU, CPU ready and CPU limited time, and `s` sorts by start order
or busiest. `<`/`>` changes the range. Members that cannot be read are named
and left out of the totals.

## History workspace

Press `H` for four panes, switched with `Tab`: Changes, Trends, Runs, and
Health. `n` takes a capture of the current scope in the background.

- **Changes** compares two captures, newest two by default. Migration
  blockers come first (vanished VMs or hosts, old snapshots, changed
  firmware or devices), then sizing, growth and churn.
- **Trends** leads with datastores projected to fill within 30 days and
  growing stale snapshots, then CPU, memory, storage, and VM counts across up
  to 30 runs.
- **Runs** lists captures with their coverage, duration, label, pin and note.
- **Health** shows the latest migration verdict and findings, worst first.
  `Enter` opens a finding, `/` filters. Use
  [`vsfleet analyze health`](health.md) to tune thresholds.

| Keys | Action |
|---|---|
| `b` / `t` | Choose which end of the comparison to move: baseline or target |
| `←` / `→` | Move that end one run |
| `R` | Pick a run from a list |
| `c` | Set the baseline to the newest older run covering the same vCenters |
| `1`–`4`, `0` | Filter blocks / sizing / growth / churn; clear |
| `h` | Open the VM timeline from a change |
| `e` / `N` / `p` in Runs | Edit label / note / pin |
| `Enter` in Runs | Compare with the run before |

The coverage line marks each vCenter `●` reached, `✕` not compared, or `·`
no record; `c` limits the comparison to what both runs covered.
`vsfleet demo` stores nothing, so it refuses `n`, `e`, `N` and `p`. See
[assessments](assessments.md) for captures and comparisons.

### VM timeline

Press `h` on a VM, a vApp member, or a Changes row for that VM's timeline:

| Tab | Shows |
|---|---|
| `1 Changes` | What changed between stored captures, as far back as your history |
| `2 vCenter events` | Who did what and when, read live, including failed tasks and changes undone before the next capture; as far back as vCenter keeps events |
| `3 Combined` | Each stored change beside the event that caused it |

On Combined, `←` marks the event that caused a change, `‹` an event from
before vsfleet first saw the VM, and `·` an event that left no stored
change, for example because it failed or was undone.

Events are read only when you open an events tab or press `r`. The header
says `LIVE` and names the vCenter; a vCenter that could not be read is
named, not left out. Task rows show `✓` or `✕` with vCenter's error. `a`
shows unchanged runs or routine events. The same log is available as
`vsfleet events --for vm/<name>`.

## Refresh and cache behavior

| Context | Background refresh |
|---|---|
| Selected | Every 20 seconds |
| Visited before, not selected | Every 200 seconds |
| Never visited or logged out | None |
| Waiting for a password | Only when selected or reloaded |
| Refresh failed | Keeps cached inventory, with a warning |

`--refresh 5s` polls faster and `--refresh -1` turns timers off. Slow
vCenters are polled less often.

## Large estates

Rows arrive in pages while the cursor stays put, and a load fails only when
it stops making progress; `--timeout` bounds connecting and logging in.
Browsing skips virtual disks, guest NICs and snapshot trees, which a
[capture](assessments.md) collects.

## Welcome animation

On first run and after an upgrade, a short animation plays while the last
vCenter loads; any key skips it. Upgrades also show release notes. Set
`VSFLEET_NO_WELCOME` or `CI` to turn it off.

## Upgrade prompt

When a newer release is out, vsfleet asks at launch:

| Key | Action |
|---|---|
| `y` | Upgrade and restart (Homebrew or `go install`, macOS and Linux) |
| `c` | Copy the upgrade command or release link (other installs) |
| `n` / `Esc` | Ask again in a week |
| `s` | Skip this release |

`VSFLEET_NO_UPDATE_NOTIFIER=1` turns off checks, prompts and notices.
