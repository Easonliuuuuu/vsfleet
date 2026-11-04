#!/usr/bin/env bash
# Fail when a workflow or local action uses another action or reusable
# workflow by anything but a full commit SHA with a version comment:
#
#   uses: owner/repo@<40-hex SHA> # vX.Y.Z
#
# Dependabot reads the comment to propose updates. Local (./) and docker://
# references are allowed. docs/repository-security.md describes the policy.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
failed=0

while IFS= read -r file; do
  lineno=0
  while IFS= read -r line; do
    lineno=$((lineno + 1))
    [[ "$line" =~ ^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*([^[:space:]#]+)(.*)$ ]] || continue
    ref="${BASH_REMATCH[2]}"
    rest="${BASH_REMATCH[3]}"
    ref="${ref%\"}"; ref="${ref#\"}"; ref="${ref%\'}"; ref="${ref#\'}"
    case "$ref" in
      ./* | docker://*) continue ;;
    esac
    if [[ ! "$ref" =~ ^[^@]+@[0-9a-f]{40}$ ]]; then
      printf '%s:%d: %s is not pinned to a full commit SHA\n' "$file" "$lineno" "$ref" >&2
      failed=1
    elif [[ ! "$rest" =~ ^[[:space:]]+#[[:space:]]+v?[0-9]+(\.[0-9]+)*([-+][^[:space:]]*)?[[:space:]]*$ ]]; then
      printf '%s:%d: %s needs a "# vX.Y.Z" comment naming the pinned release\n' "$file" "$lineno" "$ref" >&2
      failed=1
    fi
  done <"$file"
done < <(find .github/workflows .github/actions -type f \( -name '*.yml' -o -name '*.yaml' \) 2>/dev/null | sort)

if [[ $failed -ne 0 ]]; then
  printf 'action pins: resolve the tag with `gh api repos/OWNER/REPO/commits/TAG --jq .sha` and pin that SHA\n' >&2
  exit 1
fi
printf 'action pins: every action is pinned to a commit SHA\n'
