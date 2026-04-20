#!/usr/bin/env bash
# demo-setup.sh — Prepare a local kind cluster for the EarlyWatch demo.
#
# This script handles everything that needs to happen before the interactive
# demo starts:
#   • Checks that required tools are present
#   • Creates a kind cluster named "earlywatch-demo"
#   • Downloads the watchctl CLI from the latest GitHub release (or builds it)
#
# Run this once, then run scripts/demo.sh to walk through the demo scenarios.
# demo.sh will install EarlyWatch onto the cluster and walk through the demos.
#
# Prerequisites (all must be on your PATH):
#   • kind    — https://kind.sigs.k8s.io/docs/user/quick-start/#installation
#   • kubectl — https://kubernetes.io/docs/tasks/tools/
#   • curl    — pre-installed on most systems
#   • docker  — https://docs.docker.com/get-docker/
#   • go      — https://go.dev/doc/install  (only required with --build)
#
# Usage:
#   bash scripts/demo-setup.sh [--skip-cluster-create] [--build]
#
#   --skip-cluster-create  Reuse an existing kind cluster named "earlywatch-demo"
#   --build                Build watchctl from source instead of downloading it.
#                          Requires go on your PATH.
set -euo pipefail

# ── Flags ────────────────────────────────────────────────────────────────────
SKIP_CLUSTER_CREATE=false
BUILD_WATCHCTL=false
for arg in "$@"; do
  case "$arg" in
    --skip-cluster-create) SKIP_CLUSTER_CREATE=true ;;
    --build)               BUILD_WATCHCTL=true ;;
  esac
done

# ── Shared utilities ─────────────────────────────────────────────────────────
# shellcheck source=scripts/demo-util.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"

# ── Header ───────────────────────────────────────────────────────────────────
clear
echo "${BOLD}EarlyWatch Demo — Setup${RESET}"
echo ""
echo "This script prepares a local kind cluster for EarlyWatch."
echo "It covers:"
echo "  1. Prerequisite check"
echo "  2. kind cluster creation"
if [ "$BUILD_WATCHCTL" = "true" ]; then
  echo "  3. Building the watchctl CLI from source"
else
  echo "  3. Downloading the watchctl CLI from the latest GitHub release"
fi
echo ""
echo "Once setup is complete, run ${BOLD}scripts/demo.sh${RESET} for the interactive demo."
echo "(demo.sh will install EarlyWatch onto the cluster.)"
echo ""
echo "${DIM}Estimated run time: ~1 minute${RESET}"
pause

# ── Step 0: Prerequisite check ───────────────────────────────────────────────
print_header "Step 0 — Checking Prerequisites"
if [ "$BUILD_WATCHCTL" = "true" ]; then
  print_info "We need kind, kubectl, go, and docker to be installed and accessible."
else
  print_info "We need kind, kubectl, curl, and docker to be installed and accessible."
fi
pause

MISSING=()
REQUIRED_TOOLS=(kind kubectl docker)
if [ "$BUILD_WATCHCTL" = "true" ]; then
  REQUIRED_TOOLS+=(go)
else
  REQUIRED_TOOLS+=(curl)
fi

for tool in "${REQUIRED_TOOLS[@]}"; do
  if command -v "$tool" &>/dev/null; then
    print_success "$tool found at $(command -v "$tool")"
  else
    print_error "$tool not found"
    MISSING+=("$tool")
  fi
done

if [ ${#MISSING[@]} -gt 0 ]; then
  echo ""
  print_error "Missing tools: ${MISSING[*]}"
  echo "Please install the missing tools and re-run setup."
  exit 1
fi

echo ""
print_success "All prerequisites satisfied."
pause

# ── Step 1: Create kind cluster ──────────────────────────────────────────────
print_header "Step 1 — Create a Local Kubernetes Cluster with kind"
print_info "kind (Kubernetes IN Docker) spins up a full Kubernetes cluster"
print_info "inside Docker containers on your local machine. We will create"
print_info "a single-node cluster named '${CLUSTER_NAME}'."
print_info ""
print_info "Expected outcome: a running cluster and a kubeconfig entry for it."
pause

if [ "$SKIP_CLUSTER_CREATE" = "true" ]; then
  print_info "Skipping cluster creation (--skip-cluster-create was set)."
  run_cmd kind export kubeconfig --name "$CLUSTER_NAME"
else
  if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    print_info "Cluster '${CLUSTER_NAME}' already exists — reusing it."
    run_cmd kind export kubeconfig --name "$CLUSTER_NAME"
  else
    run_cmd kind create cluster --name "$CLUSTER_NAME" --wait 60s
  fi
fi

echo ""
print_success "Cluster '${CLUSTER_NAME}' is ready."
echo ""
print_info "Current cluster nodes:"
run_cmd kubectl get nodes
pause

# ── Step 2: Install watchctl ─────────────────────────────────────────────────
if [ "$BUILD_WATCHCTL" = "true" ]; then
  print_header "Step 2 — Build the watchctl CLI"
  print_info "watchctl is EarlyWatch's companion CLI tool. It can install and"
  print_info "uninstall EarlyWatch on any cluster in a single command."
  print_info ""
  print_info "Expected outcome: a 'watchctl' binary appears in the repo root."
  pause

  run_cmd "$REPO_ROOT/scripts/build.sh" "$REPO_ROOT/watchctl"

  print_success "watchctl built successfully at $WATCHCTL"
  pause
else
  print_header "Step 2 — Download the watchctl CLI"
  print_info "watchctl is EarlyWatch's companion CLI tool. It can install and"
  print_info "uninstall EarlyWatch on any cluster in a single command."
  print_info ""
  print_info "We will download the latest pre-built binary from GitHub Releases."
  print_info "Pass --build to compile from source instead."
  print_info ""
  print_info "Expected outcome: a 'watchctl' binary appears in the repo root."
  pause

  print_info "Fetching the latest release tag from GitHub..."
  LATEST_TAG=$(curl -sSfL "https://api.github.com/repos/brendandburns/early-watch/releases" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')

  if [ -z "${LATEST_TAG}" ]; then
    print_error "Could not determine the latest release tag."
    print_info  "The GitHub API may be rate-limited, unreachable, or have no published releases."
    print_info  "Tip: run with --build to compile watchctl from source instead."
    exit 1
  fi

  print_info "Latest release: ${LATEST_TAG}"

  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "${ARCH}" in
    x86_64)          ARCH="amd64" ;;
    aarch64|arm64)   ARCH="arm64" ;;
  esac

  # Asset naming convention matches the release workflow:
  # watchctl-<tag>-<os>-<arch>  (e.g. watchctl-v0.1.0-linux-amd64)
  BINARY_NAME="watchctl-${LATEST_TAG}-${OS}-${ARCH}"
  DOWNLOAD_URL="https://github.com/brendandburns/early-watch/releases/download/${LATEST_TAG}/${BINARY_NAME}"

  print_info "Downloading ${BINARY_NAME}..."
  if ! curl -sSfL "${DOWNLOAD_URL}" -o "${WATCHCTL}"; then
    print_error "Failed to download watchctl from: ${DOWNLOAD_URL}"
    print_info  "The asset may not exist for your platform (${OS}/${ARCH}) or release (${LATEST_TAG})."
    print_info  "Tip: run with --build to compile watchctl from source instead."
    exit 1
  fi
  run_cmd chmod +x "${WATCHCTL}"

  print_success "watchctl ${LATEST_TAG} downloaded successfully at $WATCHCTL"
  pause
fi

# ── Setup complete ───────────────────────────────────────────────────────────
print_header "Setup Complete!"
echo ""
echo "Your kind cluster '${CLUSTER_NAME}' is up and running with watchctl ready."
echo ""
echo "  ${GREEN}✔${RESET}  Prerequisites verified"
echo "  ${GREEN}✔${RESET}  kind cluster '${CLUSTER_NAME}' created"
if [ "$BUILD_WATCHCTL" = "true" ]; then
  echo "  ${GREEN}✔${RESET}  watchctl built from source"
else
  echo "  ${GREEN}✔${RESET}  watchctl downloaded from latest release"
fi
echo ""
echo "Next step — run the interactive demo (installs EarlyWatch + walks through scenarios):"
echo ""
echo "${BOLD}   bash scripts/demo.sh${RESET}"
echo ""
echo "${DIM}Pass --skip-earlywatch-install to demo.sh if EarlyWatch is already on the cluster.${RESET}"
echo ""
