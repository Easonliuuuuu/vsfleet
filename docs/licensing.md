# License metadata (opt-in)

Add `--include-licenses` to a capture to record vSphere license products,
usage, expiry and assignments for a licensing review. It is read-only and
off by default, and license keys are never stored.

```sh
vsfleet capture run --all-contexts --include-licenses
vsfleet export rvtools --file estate.xlsx               # adds vLicense
```

Without the flag, vsfleet reads no license data and the export has no
`vLicense` sheet.

## What is collected

For each license: name, edition, cost unit, total, used, expiry and
features. For each assignment: the host, cluster or vCenter holding it.
Free-text labels are not collected. Records get IDs such as `license-001`
that are only meaningful within one capture.

## License keys are never exposed

Keys are used in memory to match assignments, then discarded. They cannot
appear in the database, CLI output, exports, or sharing profiles. `vLicense`
keeps a `Key` column for RVTools alignment, always `[redacted]`.

## Coverage: unavailable is not zero

An account without license privileges may get an empty answer rather than an
error. Every vCenter holds at least an evaluation license, so vsfleet reports
zero records as `unavailable`, not empty.

| Status | Meaning |
| --- | --- |
| `success` | Licenses and assignments were read |
| `partial` | Licenses were read but assignments were not |
| `unavailable` | Access denied, unsupported, or no records |
| `failed` | The context could not be reached |

The status appears in `vsfleetCoverage`, `describe capture`, and on stderr.
An incomplete license collection makes the capture `partial`.

## Privileges

Grant `Global.Licenses` at the vCenter root. The built-in ReadOnly role
lacks it, so use ReadOnly plus that privilege.

## The export

`vLicense` follows RVTools' column order: `Name` · `Key` · `Labels` ·
`Cost Unit` · `Total` · `Used` · `Expiration Date` · `Features` ·
`VI SDK Server` · `VI SDK UUID` · `vsfleet Context`. Expiry is a UTC date,
and features are joined with `; `. `vsfleetLicenseAssignment` lists which
entity holds which license.

## Interpreting the numbers

`Used` and `Total` count in the license's cost unit, such as CPUs or VMs,
not hosts. Do not add assignments to usage. This is what vCenter reported at
capture time, not a compliance verdict: compare it with your entitlement
records.

## Review checklist

1. Capture with `--include-licenses` using an account with `Global.Licenses`.
2. Check that every context's `vLicense` status is `success`.
3. Export and hand the workbook to the reviewer.

## Limits

These values have been checked only against the simulator, not a real
vCenter or a real RVTools license sheet. Confirm privileges and values on
your own version first.

<span id="intended-workflow-licensing-review-handoff"></span>
<span id="unresolved-validation"></span>
