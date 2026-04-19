#!/usr/bin/env bash
# demo-setup.sh — Prepare a local kind cluster for the EarlyWatch demo.
#
# This script handles everything that needs to happen before the interactive
# demo starts:
#   • Checks that required tools are present
#   • Creates a kind cluster named "earlywatch-demo"
#
# Run this once, then run scripts/demo.sh to walk through the demo scenarios.
# demo.sh will install the watchctl CLI and EarlyWatch onto the cluster.
#
# Prerequisites (all must be on your PATH):
#   • kind    — https://kind.sigs.k8s.io/docs/user/quick-start/#installation
#   • kubectl — https://kubernetes.io/docs/tasks/tools/
#   • docker  — https://docs.docker.com/get-docker/
#
# Usage:
#   bash scripts/demo-setup.sh [--skip-cluster-create]
#
#   --skip-cluster-create   Reuse an existing kind cluster named "earlywatch-demo"
set -euo pipefail

# ── Flags ────────────────────────────────────────────────────────────────────
SKIP_CLUSTER_CREATE=false
for arg in "$@"; do
  case "$arg" in
    --skip-cluster-create) SKIP_CLUSTER_CREATE=true ;;
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
echo ""
echo "Once setup is complete, run ${BOLD}scripts/demo.sh${RESET} for the interactive demo."
echo "(demo.sh will install the watchctl CLI and EarlyWatch onto the cluster.)"
echo ""
echo "${DIM}Estimated run time: ~1 minute${RESET}"
pause

# ── Step 0: Prerequisite check ───────────────────────────────────────────────
print_header "Step 0 — Checking Prerequisites"
print_info "We need kind, kubectl, and docker to be installed and accessible."
pause

MISSING=()
REQUIRED_TOOLS=(kind kubectl docker)

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

# ── Setup complete ───────────────────────────────────────────────────────────
print_header "Setup Complete!"
echo ""
echo "Your kind cluster '${CLUSTER_NAME}' is up and running."
echo ""
echo "  ${GREEN}✔${RESET}  Prerequisites verified"
echo "  ${GREEN}✔${RESET}  kind cluster '${CLUSTER_NAME}' created"
echo ""
echo "Next step — run the interactive demo (installs watchctl + EarlyWatch):"
echo ""
echo "${BOLD}   bash scripts/demo.sh${RESET}"
echo ""
echo "${DIM}Pass --skip-cleanup to keep the cluster running after the demo.${RESET}"
echo ""
