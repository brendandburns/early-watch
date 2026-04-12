#!/usr/bin/env bash
# fix.sh — automatically fix formatting and simple style issues.
#
# Tools applied (in order):
#   1. gofmt  -s  — simplify and reformat all Go source files
#   2. goimports  — rewrite import blocks to match the project's grouping rules
#   3. golangci-lint --fix — apply auto-fixes from linters that support it
#                            (e.g. misspell, whitespace, unconvert)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

echo "==> gofmt -s (format + simplify)..."
gofmt -s -w .

if command -v goimports &>/dev/null; then
  echo "==> goimports (fix import grouping)..."
  goimports -local github.com/brendandburns/early-watch -w .
else
  echo "    goimports not found — skipping (install with: go install golang.org/x/tools/cmd/goimports@latest)"
fi

if command -v golangci-lint &>/dev/null; then
  echo "==> golangci-lint --fix (auto-fixable linter issues)..."
  golangci-lint run --config .golangci.yml --fix ./... || true
else
  echo "    golangci-lint not found — skipping (run scripts/install-hooks.sh to install)"
fi

echo ""
echo "Done. Review the changes with 'git diff' before committing."
