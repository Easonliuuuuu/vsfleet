#!/usr/bin/env bash
# Print the Dockerfile base image as BASE_IMAGE_NAME and BASE_IMAGE_DIGEST
# lines for $GITHUB_ENV. .goreleaser.yaml reads them for the OCI base-image
# annotations, so Dependabot's Dockerfile digest bumps need no second edit.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base="$(sed -n 's/^FROM \([^ ]*\).*/\1/p' "$repo_root/Dockerfile" | head -n 1)"
if [[ "$base" != *@sha256:* ]]; then
  printf 'base image env: Dockerfile base image is not pinned by digest: %s\n' "$base" >&2
  exit 1
fi
printf 'BASE_IMAGE_NAME=%s\n' "${base%@*}"
printf 'BASE_IMAGE_DIGEST=%s\n' "${base#*@}"
