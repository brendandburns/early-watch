#!/usr/bin/env bash
# demo-configmap.sh — Protect a ConfigMap referenced by a Deployment.
#
# This script demonstrates EarlyWatch blocking the deletion of a ConfigMap
# that is mounted into a running Deployment, then allowing the deletion once
# the Deployment is removed.
#
# Sourced utilities (REPO_ROOT, print_* helpers, pause, run_cmd) must be
# available — either by sourcing demo-util.sh directly or by calling this
# script from demo.sh which sets up the environment first.
#
# Usage (standalone):
#   bash scripts/demo-configmap.sh
#
# Usage (called by demo.sh — preferred):
#   The variables and functions from demo-util.sh are already in scope.
set -euo pipefail

# Source shared utilities if they are not already loaded (standalone run).
if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

# ── Demo: Protect a ConfigMap ────────────────────────────────────────────
print_header "Demo — Protect a ConfigMap Referenced by a Deployment"
print_info "Scenario: you have a ConfigMap that is mounted as environment"
print_info "variables into a Deployment. Deleting that ConfigMap would cause"
print_info "new Pods to fail to start. EarlyWatch prevents this."
print_info ""
print_info "We will:"
print_info "  a) Create a ConfigMap 'demo-config' and a Deployment that uses it"
print_info "  b) Apply the 'protect-configmap-from-deletion' ChangeValidator"
print_info "  c) Try to delete the ConfigMap — expect a DENIAL"
print_info "  d) Delete the Deployment first, then retry — the deletion succeeds"
pause

# 2a — Create ConfigMap and Deployment
print_step "2a — Creating ConfigMap 'demo-config' and a Deployment that references it..."
pause

run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: default
data:
  MESSAGE: "Hello from EarlyWatch demo"
EOF

run_cmd kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo-app
  template:
    metadata:
      labels:
        app: demo-app
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
          envFrom:
            - configMapRef:
                name: demo-config
EOF

echo ""
print_success "ConfigMap and Deployment created."
run_cmd kubectl get configmap demo-config
run_cmd kubectl get deployment demo-app

pause

# 2b — Apply ChangeValidator
print_step "2b — Applying the 'protect-configmap-from-deletion' ChangeValidator..."
print_info "This rule checks whether any Deployment, DaemonSet, or CronJob in the"
print_info "same namespace references the ConfigMap being deleted."
pause

run_cmd kubectl apply -f "$REPO_ROOT/config/samples/protect_configmap_from_deletion.yaml"

echo ""
print_success "ChangeValidator applied."
run_cmd kubectl get changevalidator protect-configmap-from-deletion -n default

pause

# 2c — Try deleting the ConfigMap (should fail)
print_step "2c — Attempting to delete 'demo-config' (this should be DENIED)..."
print_info "EarlyWatch scans all Deployments, DaemonSets, and CronJobs in the"
print_info "'default' namespace for references to 'demo-config'. It finds"
print_info "'demo-app' using it via envFrom, so the deletion is denied."
print_info ""
print_info "Watch for: 'admission webhook ... denied the request:'"
pause

print_cmd "kubectl delete configmap demo-config"
if kubectl delete configmap demo-config 2>&1; then
  print_error "Unexpected: the deletion was NOT denied. Check that EarlyWatch is running."
else
  print_success "Deletion was correctly DENIED by EarlyWatch."
fi

pause

# 2d — Delete Deployment, then retry
print_step "2d — Deleting 'demo-app' Deployment, then retrying the ConfigMap deletion..."
print_info "Once the Deployment is gone there are no more references to 'demo-config'."
print_info ""
print_info "Expected outcome: the ConfigMap deletion succeeds."
pause

run_cmd kubectl delete deployment demo-app --wait=true

echo ""
print_info "Deployment removed. Retrying ConfigMap deletion..."
print_cmd "kubectl delete configmap demo-config"
if kubectl delete configmap demo-config 2>&1; then
  print_success "ConfigMap deleted successfully — EarlyWatch allowed it."
else
  print_error "Deletion still denied. Try again: kubectl delete configmap demo-config"
fi

pause
