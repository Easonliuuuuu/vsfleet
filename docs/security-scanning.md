# Vulnerability scanning

CI scans the Go code with [govulncheck](https://go.dev/doc/security/vuln/)
and the container image with [grype](https://github.com/anchore/grype). Pull
requests, releases, and a daily rescan of the published release apply the
same rules. The [workflows README](https://github.com/Easonliuuuuu/vsfleet/blob/main/.github/workflows/README.md)
lists when each scan runs.

## What fails

| Scanner | Blocks | Reported only |
| --- | --- | --- |
| govulncheck | vsfleet calls the vulnerable code | the vulnerable code is present but never called |
| grype | a High or Critical finding with a fix available | lower severities, or no fix yet |

A scan also fails when it could not run, when its vulnerability database is
stale, or when an exception is invalid or expired.

If the release scan fails, the tag and release exist but nothing is
published. Fix the finding on `main`; the next release supersedes it.

## Fixing a finding

1. **Go standard library:** rerun once the fixed Go patch is out; CI always
   uses the newest one.
2. **Go module:** bump it. Dependabot may already have.
3. **Base image:** merge Dependabot's digest bump, or bump the `Dockerfile`
   by hand.
4. **No fix, or vsfleet is not affected:** add an exception.

## Exceptions

Add one to `.github/vulnerability-exceptions.toml`:

```toml
[[exception]]
id = "GO-2026-0000"
aliases = ["CVE-2026-00000", "GHSA-xxxx-xxxx-xxxx"]
artifact = "github.com/example/module"   # as the report names it
versions = ["v1.2.3"]
justification = "No fixed release exists; vsfleet only opens files the operator supplies. Remove when upstream tags a fix."
owner = "Easonliuuuuu"
expires = 2026-12-31                     # at most 90 days ahead
```

An expired exception fails every scan until it is renewed or removed.

## Support scope

Only the latest stable release receives security fixes, as a patch release.
See the [security policy](https://github.com/Easonliuuuuu/vsfleet/blob/main/SECURITY.md).
