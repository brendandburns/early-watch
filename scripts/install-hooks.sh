#!/usr/bin/env bash
# install-hooks.sh — install git hooks and golangci-lint for local development.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOLANGCI_LINT_VERSION="v2.11.4"

echo "==> Configuring git to use .githooks directory..."
git -C "${REPO_ROOT}" config core.hooksPath .githooks

echo "==> Checking for golangci-lint ${GOLANGCI_LINT_VERSION}..."
if ! command -v golangci-lint &>/dev/null; then
  echo "    golangci-lint not found — installing..."
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b "$(go env GOPATH)/bin" "${GOLANGCI_LINT_VERSION}"
else
  INSTALLED_VERSION="$(golangci-lint --version 2>&1 | awk '{print $4}')"
  echo "    golangci-lint ${INSTALLED_VERSION} already installed."
fi

echo ""
echo "Done! Git hooks are active and golangci-lint is ready."
echo "To run the linter manually:  golangci-lint run --config .golangci.yml ./..."
