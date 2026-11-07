# Health and orphan checks

## Health findings

`vsfleet analyze health` evaluates a stored capture offline, the most recent
unless `--run` names another. Findings include
a stable rule ID, category, severity, object, context, measured evidence, and
recommendation. Categories are `migration`, `availability`, `security`,
`capacity`, and `hygiene`.

Defaults are 30 days for maximum snapshot age and 10% minimum free space for
guest filesystems. Tune them with `--max-snapshot-age`,
`--warn-datastore-free`, `--min-datastore-free`, `--min-datastore-free-bytes`,
`--min-guest-disk-free`, `--disable-rule`, `--severity`, and `--category`.

`datastore-space-low` has two levels:

| Level | Default | Flag | Lines up with |
| --- | --- | --- | --- |
| Warning | below 15% free | `--warn-datastore-free` | vCenter's "Datastore usage on disk" alarm turning red at 85% used |
| Critical | below 10% free | `--min-datastore-free` | |

vCenter's alarm turns yellow at 75% used, and Storage DRS stops placing VMs
on a datastore at 80% used by default; both come before the warning level.
`--min-datastore-free-bytes` adds a critical floor in bytes.
`--warn-datastore-free 0` turns the warning off.

`storage-drs-space-threshold` warns when every datastore in a datastore
cluster with Storage DRS on is past the cluster's space threshold (80% used
unless set otherwise) or inaccessible, since Storage DRS avoids such a
datastore when it places a new disk.
`-o wide` shows recommendations, evidence, tags and custom attributes; JSON
always includes them. `--field-selector` and `-l` keep the findings whose
object matches, in the language of [`vsfleet get`](commands.md#get-selectors).
`--list-rules` lists rules and categories. `--fail-on-findings` exits 2 for
findings at or above the selected severity; command errors exit 1.

Snapshot age uses the context's capture finish time, falling back to the run
finish time. Reading recomputes findings; the `vHealth` coverage message
records thresholds, so identical evidence and options reproduce the export.

| Rule state | Meaning |
| --- | --- |
| `not-evaluated` | The run predates required inventory fields |
| `unknown` | A required collection failed or was not recorded |
| Partial answer | Findings remain visible, with blind contexts named |
| Partly evaluated | The rule judged every VM it had evidence for; `partial_contexts` counts the VMs left out per context |

## Orphan-disk evidence

Capture with `vsfleet capture run --browse-datastores`, then inspect
`vsfleet analyze orphans`. Browsing requires `Datastore.Browse`
and adds a bounded directory listing per accessible datastore.

Candidates are matched estate-wide using VMFS UUIDs, extents, NFS exports,
vVol IDs, and datastore URLs where available. Verdicts are
`verified-unreferenced`, `suspected-unreferenced`,
`referenced-other-context`, or `unknown-incomplete-coverage`. Snapshot
chains match in both directions. Truncated browse, failed relevant collection,
or missing backing identity prevents verification.

`analyze health --fail-on-findings --severity warning` fails only on verified
orphans; lower-confidence candidates are informational. `analyze orphans
-o json` includes paths, sizes, timestamps, identity keys, references, and
coverage reasons.

## Check scan coverage

An empty list is clean only if every datastore was fully browsed. Missing,
denied, failed, or truncated browse prints `NOT EVALUATED`, names affected
datastores on stderr, and sets JSON `coverage` even when `entries` is empty.
`--fail-on-unknown` exits non-zero for any datastore not fully browsed.

Follow the reported cause: capture with `--browse-datastores` if it was omitted,
grant `Datastore.Browse` if denied, or resolve the reported failure.
For example, `browse denied on 3/3 datastores: grant Datastore.Browse`
and `browse failed on 1/1 datastores: ...` call for different actions.

The capture summary also reports the shortfall, such as `1/1 contexts
successful, datastore browse denied on 3/3 datastores`; JSON adds a
`datastore_browse` object.
