#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_VERSION="1"
readonly MODULE="github.com/easonliuuuuu/vsfleet"

mode="companion-local"
repo=""
output_dir=""

usage() {
  cat <<'EOF'
Usage: make-testbed.sh [options]

Build the deterministic VSFleet TUI with synthetic vCenter inventory.

Options:
  --mode companion-local  Build and print activation instructions (default)
  --mode shell-hook       Build and print only shell code suitable for eval
  --mode launch           Build and launch the TUI in the current terminal
  --mode verify           Run the Go suite, build, and print instructions
  --repo PATH             VSFleet checkout (default: current repository)
  --output-dir PATH       Isolated output directory
  -h, --help              Show this help
EOF
}

die() {
  printf 'testbed: %s\n' "$*" >&2
  exit 1
}

while (($#)); do
  case "$1" in
    --mode)
      (($# >= 2)) || die "--mode requires a value"
      mode="$2"
      shift 2
      ;;
    --repo)
      (($# >= 2)) || die "--repo requires a path"
      repo="$2"
      shift 2
      ;;
    --output-dir)
      (($# >= 2)) || die "--output-dir requires a path"
      output_dir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

case "$mode" in
  companion-local|shell-hook|launch|verify) ;;
  *) die "unknown mode '$mode'" ;;
esac

command -v go >/dev/null 2>&1 || die "Go is required"

if [[ -z "$repo" ]]; then
  repo="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  [[ -n "$repo" ]] || die "use --repo to name the VSFleet checkout"
fi
repo="$(cd "$repo" && pwd -P)"

[[ -f "$repo/go.mod" ]] || die "$repo does not contain go.mod"
[[ -d "$repo/cmd/vsfleet-demo" ]] || die "$repo does not contain cmd/vsfleet-demo"
[[ -f "$repo/internal/demo/backend.go" ]] || die "$repo does not contain internal/demo/backend.go"
grep -qx "module $MODULE" "$repo/go.mod" || die "$repo is not the expected VSFleet module"

if [[ -z "$output_dir" ]]; then
  output_dir="${TMPDIR:-/tmp}/vsfleet-testbed-$(id -u)"
fi
mkdir -p "$output_dir"
output_dir="$(cd "$output_dir" && pwd -P)"

marker="$output_dir/.vsfleet-testbed"
bin_dir="$output_dir/bin"
launcher="$bin_dir/vsfleet"
activation="$output_dir/activate"

if [[ -e "$launcher" && ! -f "$marker" ]]; then
  die "refusing to replace unmarked launcher at $launcher"
fi

mkdir -p "$bin_dir"
printf 'vsfleet-testbed %s\nmodule %s\n' "$SCRIPT_VERSION" "$MODULE" >"$marker"

tmp_launcher="$(mktemp "$bin_dir/.vsfleet.XXXXXX")"
cleanup() {
  if [[ -n "${tmp_launcher:-}" && -e "$tmp_launcher" ]]; then
    rm -f -- "$tmp_launcher"
  fi
}
trap cleanup EXIT

if [[ "$mode" == "verify" ]]; then
  printf 'testbed: running go test ./...\n' >&2
  (cd "$repo" && go test ./...)
fi

printf 'testbed: building synthetic launcher\n' >&2
(cd "$repo" && go build -trimpath -o "$tmp_launcher" ./cmd/vsfleet-demo)
chmod 0755 "$tmp_launcher"
mv -f -- "$tmp_launcher" "$launcher"
tmp_launcher=""

printf 'export VSFLEET_TESTBED_ROOT=%q\n' "$output_dir" >"$activation"
printf 'export PATH=%q:"$PATH"\n' "$bin_dir" >>"$activation"
chmod 0644 "$activation"

if [[ "$mode" == "shell-hook" ]]; then
  printf 'source %q\n' "$activation"
  exit 0
fi

if [[ "$mode" == "launch" ]]; then
  [[ -t 0 && -t 1 ]] || die "launch mode requires an interactive terminal"
  exec "$launcher"
fi

printf '\nVSFleet synthetic testbed is ready.\n'
printf 'Launcher: %s\n' "$launcher"
printf 'Estate: two healthy synthetic vCenters and one failed DR site\n'
printf '\nEnable it in this shell, then launch:\n'
printf '  eval "$(%q --mode shell-hook --repo %q --output-dir %q)"\n' "$0" "$repo" "$output_dir"
printf '  vsfleet\n'
