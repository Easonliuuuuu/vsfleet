# Datastore browser

Use a datastore's [detail actions](tui-actions.md#detail-pane-actions) to
browse its files or search by name. Browsing needs
[`Datastore.Browse`](configuration.md#vsphere-permissions).

## Datastore file browser

"Browse files" opens a read-only browser: nothing can be deleted, renamed,
moved, uploaded or downloaded. Each folder is read when you open it.

| Key | Action |
|---|---|
| `Enter` | Open folder or inspect file |
| `Esc` | Go up; close at root; cancel a running request |
| `/` | Filter this directory by name |
| `f` | Search recursively for a name or glob such as `*.iso` |
| `y` | Copy the entry's `[datastore] path` |

A search shows progress, marks capped results incomplete, and `Enter` on a
result opens its folder.

Inspecting a VMDK shows which VMs or templates use it; `Enter` jumps to one.
It also shows what the latest capture recorded about the file. When that
evidence is missing, old, denied or truncated, the file reads
`unknown-incomplete-coverage`. Neither that nor a search with no match proves
a file is orphaned.

An unreachable datastore cannot be browsed, and an unreadable folder shows
vCenter's error instead of an empty list.

This view has not yet been checked against a real vSphere datastore; see
[datastore browser acceptance](testing.md#datastore-browser-acceptance).

<span id="real-vsphere-validation-gate"></span>
