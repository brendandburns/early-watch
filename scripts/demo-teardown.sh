#!/usr/bin/env bash
# demo-teardown.sh — Tear down the EarlyWatch demo environment.
#
# This script removes all demo resources, uninstalls EarlyWatch, and (by
# default) deletes the kind cluster that was created by demo-setup.sh.
#
# Run this script manually after you have finished exploring, or it is
# called automatically by demo.sh's EXIT trap.
#
# Usage:
#   bash scripts/demo-teardown.sh [--skip-cluster-delete]
#
#   --skip-cluster-delete  Remove demo resources and uninstall EarlyWatch but
#                          leave the kind cluster running.
set -euo pipefail

# ── Flags ────────────────────────────────────────────────────────────────────
SKIP_CLUSTER_DELETE=false
for arg in "$@"; do
  case "$arg" in
    --skip-cluster-delete) SKIP_CLUSTER_DELETE=true ;;
  esac
done

# ── Shared utilities ─────────────────────────────────────────────────────────
# shellcheck source=scripts/demo-util.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"

# ── Exit trap — keep terminal open ────────────────────────────────────────────
_on_exit() {
  echo ""
  echo -n "${DIM}   Press Enter to close...${RESET}"
  read -r _
}
trap '_on_exit' EXIT

# ── Demo resource cleanup ────────────────────────────────────────────────────
print_header "Teardown — Removing Demo Resources"

print_step "Removing demo Kubernetes resources..."
run_cmd kubectl delete service        demo-service --ignore-not-found=true
run_cmd kubectl delete pod            demo-pod     --ignore-not-found=true
run_cmd kubectl delete configmap      demo-config  --ignore-not-found=true
run_cmd kubectl delete deployment     demo-app     --ignore-not-found=true
run_cmd kubectl delete changevalidator protect-service-from-deletion   -n default --ignore-not-found=true
run_cmd kubectl delete changevalidator protect-configmap-from-deletion -n default --ignore-not-found=true
print_success "Demo resources removed."

# ── Uninstall EarlyWatch ─────────────────────────────────────────────────────
print_step "Uninstalling EarlyWatch..."
if kubectl get namespace early-watch-system &>/dev/null; then
  run_cmd "$WATCHCTL" uninstall --kubeconfig "$HOME/.kube/config"
  print_success "EarlyWatch uninstalled."
else
  print_info "EarlyWatch is not installed — skipping uninstall."
fi

# ── Cluster teardown ─────────────────────────────────────────────────────────
if [ "$SKIP_CLUSTER_DELETE" = "true" ]; then
  echo ""
  print_info "Skipping cluster deletion (--skip-cluster-delete was set)."
  print_info "Run 'kind delete cluster --name $CLUSTER_NAME' to remove it later."
else
  print_step "Deleting kind cluster '$CLUSTER_NAME'..."
  kind delete cluster --name "$CLUSTER_NAME"
  print_success "Cluster '$CLUSTER_NAME' deleted."
fi

echo ""
print_success "Teardown complete."
