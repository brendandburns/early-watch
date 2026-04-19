#!/usr/bin/env bash
# build.sh -- build watchctl with the version set to the current git tag.
#
# Usage:
#   scripts/build.sh [output-path]
#
# The binary is written to <output-path> (default: ./watchctl).  The version
# is taken from the most recent git tag reachable from HEAD (via
# "git describe --tags --abbrev=0").  When no tag exists the string "latest"
# is used so that the binary remains functional on untagged development trees.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT="${1:-${REPO_ROOT}/watchctl}"

VERSION="$(git -C "${REPO_ROOT}" describe --tags --abbrev=0 2>/dev/null || echo "latest")"
LDFLAGS="-X github.com/brendandburns/early-watch/cmd/watchctl.Version=${VERSION} -X github.com/brendandburns/early-watch/pkg/install.Version=${VERSION}"

echo "Building watchctl ${VERSION} → ${OUTPUT}"
go build -ldflags "${LDFLAGS}" -o "${OUTPUT}" "${REPO_ROOT}/cmd/watchctl/"
echo "Done."
