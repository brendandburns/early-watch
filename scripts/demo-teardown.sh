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

# ── Demo resource cleanup ────────────────────────────────────────────────────
print_header "Teardown — Removing Demo Resources"

print_step "Removing demo Kubernetes resources..."

# Remove validators first so webhook rules do not block cleanup deletes.
kubectl delete changevalidator protect-service-from-deletion   -n default --ignore-not-found=true
kubectl delete changevalidator protect-configmap-from-deletion -n default --ignore-not-found=true
kubectl delete changevalidator require-annotation-confirm-delete --ignore-not-found=true
kubectl delete changevalidator require-approval-signature -n default --ignore-not-found=true
kubectl delete changevalidator protect-deployments-with-check-lock -n default --ignore-not-found=true
kubectl delete changevalidator deny-delete-demo-expression-cm -n default --ignore-not-found=true
kubectl delete changevalidator block-update-after-manual-touch -n default --ignore-not-found=true
kubectl delete changevalidator protect-service-selector-update -n default --ignore-not-found=true

kubectl delete service        demo-service --ignore-not-found=true
kubectl delete service        demo-selector-service --ignore-not-found=true
kubectl delete pod            demo-pod     --ignore-not-found=true
kubectl delete pod            demo-selector-pod --ignore-not-found=true
kubectl delete pod            demo-selector-new-pod --ignore-not-found=true
kubectl delete configmap      demo-config  --ignore-not-found=true
kubectl delete configmap      demo-approved-config --ignore-not-found=true
kubectl delete configmap      demo-expression-cm --ignore-not-found=true
kubectl delete configmap      demo-expression-control --ignore-not-found=true
kubectl delete deployment     demo-app     --ignore-not-found=true
kubectl delete deployment     demo-lock-app --ignore-not-found=true
kubectl delete deployment     demo-manual-app --ignore-not-found=true
kubectl delete namespace      demo-annotation-ns --ignore-not-found=true

if kubectl api-resources --verbs=list --namespaced -o name 2>/dev/null | grep -qx "manualtouchevents.earlywatch.io"; then
  kubectl delete manualtouchevent demo-manual-touch-event -n early-watch-system --ignore-not-found=true
else
  print_info "ManualTouchEvent CRD not installed — skipping ManualTouchEvent cleanup."
fi
print_success "Demo resources removed."

# ── Uninstall EarlyWatch ─────────────────────────────────────────────────────
print_step "Uninstalling EarlyWatch..."
if kubectl get namespace early-watch-system &>/dev/null; then
  "$WATCHCTL" uninstall --kubeconfig "$HOME/.kube/config"
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
