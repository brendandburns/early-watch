#!/usr/bin/env bash
# lint.sh — run golangci-lint against the entire repository.
set -euo pipefail

# shellcheck source=scripts/demo-util.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"

if ! command -v golangci-lint &>/dev/null; then
  echo "golangci-lint not found. Run scripts/install-hooks.sh to set up the dev environment." >&2
  exit 1
fi

cd "${REPO_ROOT}"
echo "Running golangci-lint..."
golangci-lint run --config .golangci.yml ./...
echo "Lint passed."
