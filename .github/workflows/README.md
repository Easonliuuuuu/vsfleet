# Workflows

| Workflow | Runs | Does |
| --- | --- | --- |
| `ci.yml` | PRs and pushes to `main`; docs-only changes skip the jobs | Format, vet, race tests on Linux/macOS/Windows, staticcheck, TUI scenarios, PTY journeys, vcsim integration, and a release snapshot that is built, smoke-tested, and scanned. `CI result` sums them up for branch protection. Publishes the README platform badge. |
| `vulnerabilities.yml` | Every PR and push to `main`; daily 04:41 UTC | `Go vulnerabilities` and `Pinned actions`, the two checks `main` requires. The daily run also scans the published release and keeps one issue open while it fails. |
| `docs.yml` | Changes to `docs/`, `README.md`, or `mkdocs.yml` | `mkdocs build --strict`; deploys GitHub Pages from `main`. |
| `fuzz.yml` | Nightly 03:17 UTC; PRs that touch fuzz tests | 5 minutes per target nightly, 1 minute on PRs. |
| `kubernetes.yml` | Weekly Monday 01:17 UTC; PRs that change backend, container, or deploy files | Runs the shipped CronJob in kind against two vcsim services. |
| `release.yml` | Pushes to `main` | release-please opens the release PR; merging it tags and publishes binaries, images, Homebrew, and Scoop, then puts the release's What's new notes at the top of the release. |
| `labeler.yml` | Issues and PRs opened or edited | Labels from the PR title or the issue form's Component field. |

Run `fuzz`, `kubernetes`, `vulnerabilities`, `docs`, and `release` by hand
from **Actions → *workflow* → Run workflow**. Run `kubernetes` before a
release when the deployment has changed.

## When a check fails

- **Go vulnerabilities or a release scan:** follow
  [vulnerability scanning](../../docs/security-scanning.md).
  To reproduce locally:
  `PATH="$(scripts/install-scanners.sh ~/.cache/vsfleet-scanners govulncheck):$PATH" scripts/vulnscan govulncheck "Go source" /tmp/vuln ./...`
- **Pinned actions:** pin to a full commit SHA with the version in a
  comment, as the other workflows do. Get the SHA with
  `gh api repos/OWNER/REPO/commits/vX.Y.Z --jq .sha` and check it is on the
  upstream repository, not a fork.
- **TUI goldens:** review the diff, then rerun with `--update-goldens`; see
  [render contracts](../../docs/testbed.md#render-contracts).
- **Fuzz:** commit the failing input under the package's
  `testdata/fuzz/<Target>/` so it becomes a regression seed.

Failure artifacts (PTY transcripts, vcsim logs, scan reports, Kubernetes
logs) are attached to the run.

## Keeping pins current

Dependabot updates action SHAs weekly, but not these. Bump them by hand:

| Tool | Pinned in |
| --- | --- |
| GoReleaser | `version:` in `ci.yml` and `release.yml` |
| Staticcheck | `version:` in `ci.yml` |
| kind, kubectl, node image | `kubernetes.yml`; update together |
| govulncheck, grype | `scripts/install-scanners.sh`; update the grype checksums with the version |

Go is deliberately unpinned: `setup-go` installs the newest 1.26 patch so
standard-library fixes arrive without a change. Do not switch it to
`go-version-file: go.mod`, which builds with Go 1.26.0.

`scripts/check-release-pins.sh` keeps the base image digest and the release
version in docs and `deploy/` consistent.

## Publishing tokens

`TAP_GITHUB_TOKEN` publishes Homebrew and Scoop updates with Contents write
on `homebrew-tap` only. It expires within a year. To rotate it, create the
replacement, update the Actions secret, and revoke the old token. CI uses a
placeholder, so the next release is its first real use.
