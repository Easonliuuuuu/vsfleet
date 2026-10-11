Welcome external contributors, just know what you are doing. 

## Release publishing

The release workflow runs GoReleaser after release-please creates a release.
It publishes the binaries, Linux packages and container image, updates the
Homebrew/Scoop tap, and submits stable versions to WinGet.

WinGet manifests use `Easonliuuuuu.vsfleet` and the Windows amd64/arm64 ZIPs.
GoReleaser pushes them to `Easonliuuuuu/winget-pkgs` on `vsfleet-VERSION` and
opens a PR against `microsoft/winget-pkgs:master`. Prereleases are not submitted.
The workflow confirms that the PR exists and links it in the job summary.
Availability through WinGet still requires Microsoft's validation, approval
and catalog publication.

Before the first automated submission:

1. Keep the `Easonliuuuuu/winget-pkgs` fork available and sign Microsoft's CLA
   with the account used for submissions.
2. Create a dedicated GitHub classic personal access token with `public_repo`
   scope for the fork owner. It must write to the fork and open PRs in
   `microsoft/winget-pkgs`; the workflow's `GITHUB_TOKEN` only covers vsfleet.
3. Add it as the repository Actions secret `WINGET_GITHUB_TOKEN`. With GitHub
   CLI, run the following and paste the token at the hidden prompt:

   ```sh
   gh secret set WINGET_GITHUB_TOKEN --repo Easonliuuuuu/vsfleet
   ```

The existing `TAP_GITHUB_TOKEN` remains responsible for Homebrew and Scoop.
CI snapshots use placeholder tokens and skip publishing, so PR builds need
no publishing credentials.

Check the WinGet PR after each release. If validation fails, correct that
version's manifests in the fork and update its PR. If the release workflow
fails to submit, inspect the GoReleaser logs and download the `winget-manifests`
artifact from the failed workflow run for manual submission. It contains the
generated `dist/winget` directory when generation completed.
See [GoReleaser's WinGet publisher](https://goreleaser.com/customization/publish/winget/)
and [Microsoft's validation process](https://github.com/microsoft/winget-pkgs/blob/master/doc/Validation.md).
