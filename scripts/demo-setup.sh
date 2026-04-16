#!/usr/bin/env bash
# demo-setup.sh — Prepare a local kind cluster for the EarlyWatch demo.
#
# This script handles everything that needs to happen before the interactive
# demo starts:
#   • Checks that required tools are present
#   • Creates a kind cluster named "earlywatch-demo"
#   • Builds the watchctl CLI from source
#   • Installs EarlyWatch onto the cluster
#   • Inspects the installed resources so you can confirm everything is healthy
#
# Run this once, then run scripts/demo.sh to walk through the demo scenarios.
#
# Prerequisites (all must be on your PATH):
#   • kind    — https://kind.sigs.k8s.io/docs/user/quick-start/#installation
#   • kubectl — https://kubernetes.io/docs/tasks/tools/
#   • go      — https://go.dev/doc/install  (1.21+)
#   • docker  — https://docs.docker.com/get-docker/
#
# Usage:
#   bash scripts/demo-setup.sh [--skip-cluster-create]
#
#   --skip-cluster-create  Reuse an existing kind cluster named "earlywatch-demo"
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
echo "This script prepares a local kind cluster with EarlyWatch installed."
echo "It covers:"
echo "  1. Prerequisite check"
echo "  2. kind cluster creation"
echo "  3. Building the watchctl CLI"
echo "  4. Installing EarlyWatch onto the cluster"
echo "  5. Inspecting the installed resources"
echo ""
echo "Once setup is complete, run ${BOLD}scripts/demo.sh${RESET} for the interactive demo."
echo ""
echo "${DIM}Estimated run time: ~3 minutes${RESET}"
pause

# ── Step 0: Prerequisite check ───────────────────────────────────────────────
print_header "Step 0 — Checking Prerequisites"
print_info "We need kind, kubectl, go, and docker to be installed and accessible."
pause

MISSING=()
for tool in kind kubectl go docker; do
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

# ── Step 2: Build watchctl ───────────────────────────────────────────────────
print_header "Step 2 — Build the watchctl CLI"
print_info "watchctl is EarlyWatch's companion CLI tool. It can install and"
print_info "uninstall EarlyWatch on any cluster in a single command."
print_info ""
print_info "Expected outcome: a 'watchctl' binary appears in the repo root."
pause

cd "$REPO_ROOT"
run_cmd go build -o watchctl ./cmd/watchctl/...

print_success "watchctl built successfully at $WATCHCTL"
pause

# ── Step 3: Install EarlyWatch ───────────────────────────────────────────────
print_header "Step 3 — Install EarlyWatch onto the Cluster"
print_info "watchctl install applies the following resources in one go:"
print_info "  • ChangeValidator CRD  — defines the custom resource type"
print_info "  • RBAC (ClusterRole + ClusterRoleBinding + ServiceAccount)"
print_info "  • Webhook Deployment   — the admission controller pod"
print_info "  • Webhook Service      — exposes the controller inside the cluster"
print_info "  • ValidatingWebhookConfiguration — registers with the API server"
print_info ""
print_info "The install is idempotent; running it twice is safe."
print_info ""
print_info "Expected outcome: all EarlyWatch components are Running in the"
print_info "'early-watch-system' namespace."
pause

run_cmd "$WATCHCTL" install --kubeconfig "$HOME/.kube/config"

echo ""
print_info "Waiting for the webhook deployment to become ready (up to 120s)..."
run_cmd kubectl rollout status deployment/early-watch-webhook \
  -n early-watch-system --timeout=120s

print_success "EarlyWatch is installed and ready."
pause

# ── Step 4: Inspect installed resources ─────────────────────────────────────
print_header "Step 4 — Inspect the Installed Resources"
print_info "Let's take a look at what was created."
print_info ""
print_info "You should see:"
print_info "  • The 'early-watch-system' namespace"
print_info "  • The webhook pod in a Running state"
print_info "  • The 'changevalidators.earlywatch.io' CRD"
print_info "  • The ValidatingWebhookConfiguration that hooks into the API server"
pause

echo ""
echo "${BOLD}Namespace:${RESET}"
run_cmd kubectl get namespace early-watch-system

echo ""
echo "${BOLD}Pods in early-watch-system:${RESET}"
run_cmd kubectl get pods -n early-watch-system

echo ""
echo "${BOLD}ChangeValidator CRD:${RESET}"
run_cmd kubectl get crd changevalidators.earlywatch.io

echo ""
echo "${BOLD}ValidatingWebhookConfiguration:${RESET}"
run_cmd kubectl get validatingwebhookconfiguration early-watch-validating-webhook

pause

# ── Setup complete ───────────────────────────────────────────────────────────
print_header "Setup Complete!"
echo ""
echo "Your kind cluster '${CLUSTER_NAME}' has EarlyWatch installed and running."
echo ""
echo "  ${GREEN}✔${RESET}  Prerequisites verified"
echo "  ${GREEN}✔${RESET}  kind cluster '${CLUSTER_NAME}' created"
echo "  ${GREEN}✔${RESET}  watchctl built from source"
echo "  ${GREEN}✔${RESET}  EarlyWatch installed and webhook ready"
echo ""
echo "Next step — run the interactive demo:"
echo ""
echo "${BOLD}   bash scripts/demo.sh${RESET}"
echo ""
echo "${DIM}Pass --skip-cleanup to keep the cluster running after the demo.${RESET}"
echo ""
