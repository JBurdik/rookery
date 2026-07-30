#!/bin/sh
# Install rookery's `rook` command from the current main branch.
# Usage: curl -fsSL https://raw.githubusercontent.com/JBurdik/rookery/main/install.sh | sh

set -eu

if ! command -v go >/dev/null 2>&1; then
  echo "rook installer: Go is required. Install Go from https://go.dev/dl/ and try again." >&2
  exit 1
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/rookery-install.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

curl -fsSL https://github.com/JBurdik/rookery/archive/refs/heads/main.tar.gz \
  | tar -xz -C "$work_dir" --strip-components=1

(cd "$work_dir" && go install ./cmd/rook)

bin_dir=${GOBIN:-"$(go env GOPATH)/bin"}
printf 'Installed rook to %s/rook\n' "$bin_dir"

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) printf 'Add %s to your PATH, then run: rook setup && rook\n' "$bin_dir" ;;
esac
