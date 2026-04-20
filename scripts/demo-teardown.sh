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
