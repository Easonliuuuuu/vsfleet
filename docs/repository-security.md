# Repository and release security

How vsfleet keeps its releases from being tampered with, and what to do if
something is compromised. [Vulnerability scanning](security-scanning.md)
covers known vulnerabilities in the code and image.

## Controls

- **Pinned actions.** Every workflow action is pinned to a commit SHA, so a
  moved tag cannot change what runs. A required check rejects anything else.
- **Protected `main`.** Changes arrive only as squash-merged pull requests
  that pass `CI result`, `Go vulnerabilities` and `Pinned actions`. History
  cannot be rewritten.
- **Immutable tags.** A published `v*` tag cannot be moved or deleted.
- **Least-privilege tokens.** Workflows get a read-only token by default.
  A personal token publishes Homebrew and Scoop updates to `homebrew-tap`,
  scoped to that repository and expiring within a year.
- **Keyless signing.** Images are signed with the release workflow's
  identity; there is no signing key to leak.

vsfleet has one maintainer, so no change gets independent review. Required
approvals or code owners would only be self-approval.

## If something is compromised

Act in this order when a token, the maintainer account, an action, or a
release looks compromised:

1. **Stop publishing.** Disable Actions, or the `Release` workflow.
2. **Revoke credentials.** Revoke `TAP_GITHUB_TOKEN`, sign out other
   sessions, and review the account's tokens, SSH keys, and authorized apps.
3. **Find what changed.** Read the audit logs, `Release` runs, recent pushes
   to `main` and tags, and the `homebrew-tap` history.
4. **Check released artifacts.** Compare archives against `checksums.txt`
   and [verify image signatures](containers.md). Delete tampered assets and
   tags, and revert tampered formula or manifest commits.
5. **Recover.** Publish a security advisory naming the affected versions,
   rotate every credential, re-enable Actions, and ship a clean patch
   release.

If an upstream action is compromised, pin its last known-good commit or
remove it before re-enabling workflows.
