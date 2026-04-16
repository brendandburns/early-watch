#!/usr/bin/env bash
# post-create.sh — set up the development environment after the devcontainer is created.
set -euo pipefail

GOLANGCI_LINT_VERSION="v2.11.4"
KIND_VERSION="v0.27.0"

echo "==> Installing golangci-lint ${GOLANGCI_LINT_VERSION}..."
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
  | sh -s -- -b "$(go env GOPATH)/bin" "${GOLANGCI_LINT_VERSION}"

echo "==> Installing KIND ${KIND_VERSION}..."
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)  KIND_ARCH="amd64" ;;
  aarch64) KIND_ARCH="arm64" ;;
  *)       KIND_ARCH="${ARCH}" ;;
esac
mkdir -p "${HOME}/.local/bin"
curl -sSfL "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${KIND_ARCH}" \
  -o "${HOME}/.local/bin/kind"
chmod +x "${HOME}/.local/bin/kind"

echo "==> Adding ~/.local/bin to PATH..."
for rc in "${HOME}/.bashrc" "${HOME}/.profile"; do
  if ! grep -qF 'export PATH="${HOME}/.local/bin:${PATH}"' "${rc}" 2>/dev/null; then
    echo 'export PATH="${HOME}/.local/bin:${PATH}"' >> "${rc}"
  fi
done
export PATH="${HOME}/.local/bin:${PATH}"

echo "==> Installing Go tools..."
go install golang.org/x/tools/cmd/goimports@latest

echo "==> Configuring git hooks..."
git config core.hooksPath .githooks

echo ""
echo "Done! Development environment is ready."
echo "  kind create cluster   — create a local Kubernetes cluster"
echo "  go test ./pkg/...     — run unit tests"
echo "  scripts/lint.sh       — run linters"
