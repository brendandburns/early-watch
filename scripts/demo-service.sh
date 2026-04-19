#!/usr/bin/env bash
# demo-service.sh — Protect a Service from deletion.
#
# This script demonstrates EarlyWatch blocking the deletion of a Service
# while matching Pods are still running, then allowing the deletion once
# the Pods are removed.
#
# Sourced utilities (REPO_ROOT, print_* helpers, pause, run_cmd) must be
# available — either by sourcing demo-util.sh directly or by calling this
# script from demo.sh which sets up the environment first.
#
# Usage (standalone):
#   bash scripts/demo-service.sh
#
# Usage (called by demo.sh — preferred):
#   The variables and functions from demo-util.sh are already in scope.
set -euo pipefail

# Source shared utilities if they are not already loaded (standalone run).
if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

# ── Demo: Protect a Service ───────────────────────────────────────────────
print_header "Demo — Protect a Service from Deletion"
print_info "Scenario: you have a Service and Pods that it routes traffic to."
print_info "EarlyWatch should prevent you from deleting the Service while"
print_info "the Pods are still running, so traffic is never silently dropped."
print_info ""
print_info "We will:"
print_info "  a) Create a Service named 'demo-service' and a matching Pod"
print_info "  b) Apply the 'protect-service-from-deletion' ChangeValidator"
print_info "  c) Try to delete the Service — expect a DENIAL from EarlyWatch"
print_info "  d) Delete the Pod first, then retry — the deletion succeeds"
pause

# 1a — Create Service and Pod
print_step "1a — Creating Service 'demo-service' and a matching Pod..."
print_info "The Service selects Pods with the label 'app=demo'. We will"
print_info "create one such Pod so the Service has active traffic targets."
pause

run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: demo-service
  namespace: default
spec:
  selector:
    app: demo
  ports:
    - port: 80
      targetPort: 8080
EOF

run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: demo-pod
  namespace: default
  labels:
    app: demo
spec:
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
EOF

print_info "Waiting for the demo-pod to be Running..."
run_cmd kubectl wait --for=condition=ready pod/demo-pod --timeout=60s

echo ""
print_success "Service and Pod are ready."
run_cmd kubectl get service demo-service
run_cmd kubectl get pod demo-pod -o wide

pause

# 1b — Apply ChangeValidator
print_step "1b — Applying the 'protect-service-from-deletion' ChangeValidator..."
print_info "This ChangeValidator tells EarlyWatch: 'Deny any DELETE on a Service"
print_info "in the default namespace if Pods matching its spec.selector exist.'"
pause

run_cmd kubectl apply -f "$REPO_ROOT/config/samples/protect_service.yaml"

echo ""
print_success "ChangeValidator applied."
run_cmd kubectl get changevalidator protect-service-from-deletion -n default

pause

# 1c — Try deleting the Service (should fail)
print_step "1c — Attempting to delete 'demo-service' (this should be DENIED)..."
print_info "EarlyWatch will intercept the DELETE request and check whether any"
print_info "Pods with matching labels exist. Because 'demo-pod' is running with"
print_info "'app=demo', the deletion will be denied with a clear error message."
print_info ""
print_info "Watch for: 'admission webhook ... denied the request:'"
pause

print_cmd "kubectl delete service demo-service"
if kubectl delete service demo-service 2>&1; then
  print_error "Unexpected: the deletion was NOT denied. Check that EarlyWatch is running."
else
  print_success "Deletion was correctly DENIED by EarlyWatch."
fi

pause

# 1d — Delete the Pod, then retry
print_step "1d — Deleting 'demo-pod' first, then retrying the Service deletion..."
print_info "Once the matching Pod is gone there is nothing left to protect."
print_info "EarlyWatch will re-evaluate the rules and this time allow the deletion."
print_info ""
print_info "Expected outcome: the Service deletion succeeds."
pause

run_cmd kubectl delete pod demo-pod --wait=true

echo ""
print_info "Pod removed. Retrying Service deletion..."
print_cmd "kubectl delete service demo-service"
if kubectl delete service demo-service 2>&1; then
  print_success "Service deleted successfully — EarlyWatch allowed it."
else
  print_error "Deletion still denied. The Pod may not be fully terminated yet."
  print_info  "Try again in a few seconds: kubectl delete service demo-service"
fi

pause
