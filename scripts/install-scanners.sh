#!/usr/bin/env bash
# Install the pinned vulnerability scanners into DIR and print DIR, for
# $GITHUB_PATH. Bump a version and its checksums together; the policy and
# the bump procedure are in docs/security-scanning.md.
#
#   scripts/install-scanners.sh DIR [govulncheck] [grype]
#
# With no tool names, both are installed. grype is Linux-only here.
set -euo pipefail

GOVULNCHECK_VERSION=v1.9.0
GRYPE_VERSION=0.121.1
GRYPE_SHA256_linux_amd64=bc3ea3f9e6fd56ac781c08c6c5e614271b79f438200757f6932085c19b0a4657
GRYPE_SHA256_linux_arm64=33670612aad351c48cf3c37f4a85d8b043d1b64b21e6034e675bb00b956d78c1

dir="${1:?usage: install-scanners.sh DIR [govulncheck] [grype]}"
shift
tools=("$@")
[[ ${#tools[@]} -gt 0 ]] || tools=(govulncheck grype)
mkdir -p "$dir"
dir="$(cd "$dir" && pwd)"

for tool in "${tools[@]}"; do
  case "$tool" in
    govulncheck)
      # The Go checksum database verifies the module download.
      GOBIN="$dir" go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
      ;;
    grype)
      case "$(uname -m)" in
        x86_64) arch=amd64 ;;
        aarch64 | arm64) arch=arm64 ;;
        *) printf 'install-scanners: no pinned grype for %s\n' "$(uname -m)" >&2; exit 1 ;;
      esac
      sum_var="GRYPE_SHA256_linux_${arch}"
      archive="grype_${GRYPE_VERSION}_linux_${arch}.tar.gz"
      tmp="$(mktemp -d)"
      trap 'rm -rf "$tmp"' EXIT
      curl -fsSL --retry 3 -o "$tmp/$archive" \
        "https://github.com/anchore/grype/releases/download/v${GRYPE_VERSION}/${archive}"
      printf '%s  %s\n' "${!sum_var}" "$tmp/$archive" | sha256sum --check --quiet
      tar -xzf "$tmp/$archive" -C "$dir" grype
      ;;
    *)
      printf 'install-scanners: unknown tool %s\n' "$tool" >&2
      exit 1
      ;;
  esac
done
printf '%s\n' "$dir"
