# Vulnerability scanning

CI checks Go code with [govulncheck](https://go.dev/doc/security/vuln/) and
the container image with [grype](https://github.com/anchore/grype). Each scan
is judged by `cmd/vsfleet-vulngate` against one policy and one exception
file, so a pull request, a release, and the daily rescan fail for the same
reasons.

## When scans run

| Trigger | What is scanned | Workflow job |
| --- | --- | --- |
| Every pull request and push to `main` | Go source, with the release toolchain | `Vulnerabilities / Go vulnerabilities` |
| Pull requests that run CI | Every snapshot release binary, and each snapshot image | `CI / Release snapshot` |
| Release, before anything is published | Go source with the release toolchain, and the pinned base image for both platforms | `Release / goreleaser` |
| Daily at 04:41 UTC, or on demand | Go source on `main`; every binary and both image platforms of the supported release, by digest | `Vulnerabilities` |

`Go vulnerabilities` runs on every pull request, documentation-only ones
included, so the `main` ruleset can require it.

The release image is the pinned distroless base plus the release binary, and
the release job gates both before GoReleaser publishes. The release job does
not build a separate image to scan; CI's snapshot job scans the image built
from the same configuration. If the release gate fails, release-please has
already created the tag and release, but no binaries, packages, or images
exist for it. Fix the finding on `main` and let the next release supersede
it.

Every run keeps its JSON reports and verdicts as a
`vulnerability-results-*` artifact for 90 days. The verdict also appears in
the job summary.

## What fails

| Scanner | Fails the gate | Reported only |
| --- | --- | --- |
| govulncheck | A vulnerable symbol is reachable: the source calls it, or the binary contains it | The package is imported but the vulnerable code is not called, or the module is required but the package is not imported |
| grype | A High or Critical finding with a fixed version available | Medium, Low, Negligible, or Unknown severity; High or Critical with no fix yet |

The gate also fails, whatever the findings, when:

- the scanner exits with an error or produces no report, for example when its
  database cannot be downloaded. The summary says the scan did not run;
- the vulnerability database is too old: 14 days since `vuln.go.dev` last
  changed for govulncheck, 120 hours since the grype database was built.
  Grype also refuses a database older than 120 hours itself;
- an exception is missing a field, has expired, or expires more than 90 days
  ahead.

govulncheck owns Go reachability. Grype also matches the Go binary inside
the image, without reachability, so a Go advisory can appear in both reports.
One exception can cover both through its aliases.

## Exceptions

Add an exception to `.github/vulnerability-exceptions.toml` only when a
blocking finding cannot be fixed yet, or does not affect vsfleet:

```toml
[[exception]]
id = "GO-2026-0000"
aliases = ["CVE-2026-00000", "GHSA-xxxx-xxxx-xxxx"]
artifact = "github.com/example/module"
versions = ["v1.2.3"]
justification = "No fixed release exists; vsfleet only opens files the operator supplies. Remove when upstream tags a fix."
owner = "Easonliuuuuu"
expires = 2026-12-31
```

| Key | Meaning |
| --- | --- |
| `id`, `aliases` | Advisory IDs the exception matches; a finding matches when any of its IDs appears |
| `artifact` | Go module path, `stdlib`, or OS package name, exactly as the report shows it |
| `versions` | The affected versions the exception covers; a new version needs a new review |
| `justification` | Why the finding is accepted and what ends the exception |
| `owner` | Who renews or removes it |
| `expires` | Last day the exception applies, in UTC; at most 90 days ahead |

Unknown keys are errors, so a misspelled key cannot silently change what an
exception covers. An expired exception fails every scan until it is removed
or renewed with a new review.

## Triage

1. **Go standard library.** CI and releases use the latest Go 1.26 patch
   release, so rerun the job once the fixed Go release is out. Do not pin
   `go-version-file: go.mod`: setup-go reads the `go 1.26.0` line as an
   exact release and builds with an unpatched standard library.
2. **Go module, fix released.** Bump the module, run the tests, and merge.
   Dependabot's weekly update may already have it.
3. **Base image packages.** Dependabot bumps the distroless digest in the
   `Dockerfile`; merge that update, or bump it by hand when the daily scan
   finds a fixable package first.
4. **No fix, or not exploitable in vsfleet.** Add an exception with a
   justification and a short expiry, then revisit it before it expires.
5. **Reported-only findings.** No action is required. Pick up the fix
   through normal dependency updates.

The daily run opens one issue titled "Daily vulnerability scan is failing",
comments on it while the scan keeps failing, and closes it after the next
clean run.

## Support scope

Only the latest stable release, its binaries, packages, and its
`ghcr.io/easonliuuuuu/vsfleet` image, receives security fixes. The daily scan
covers that release by image digest; older releases are fixed by upgrading.
When the scan finds a blocking vulnerability in the supported release, the
fix ships as a patch release. vsfleet has one maintainer, so as the
[security policy](https://github.com/Easonliuuuuu/vsfleet/blob/main/SECURITY.md)
says, there is no guaranteed response time.

## Scanner versions

`scripts/install-scanners.sh` pins govulncheck and grype. The Go checksum
database verifies govulncheck; the script checks grype's archive against the
SHA-256 sums from the grype release. To upgrade grype, change
`GRYPE_VERSION` and both checksums together, taking them from the release's
`checksums.txt`. Dependabot does not update these pins.

Run a scan locally with the same gate. It scans with your local Go
toolchain, so an older patch release reports its standard library findings:

```sh
dir="$(scripts/install-scanners.sh "$HOME/.cache/vsfleet-scanners" govulncheck)"
PATH="$dir:$PATH" scripts/vulnscan govulncheck "Go source" /tmp/vuln ./...
```
