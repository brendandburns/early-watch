#!/usr/bin/env bash
# demo-check-lock.sh - Demonstrate CheckLock behavior on DELETE.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_LOCK_DEPLOY="demo-lock-app"
DEMO_LOCK_CV="protect-deployments-with-check-lock"

print_header "Demo - CheckLock"
print_info "Scenario: a deployment with earlywatch.io/lock cannot be deleted."
print_info "Removing the lock annotation allows deletion."
pause

print_step "1a - Creating demo Deployment '$DEMO_LOCK_DEPLOY'..."
run_cmd kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-lock-app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo-lock-app
  template:
    metadata:
      labels:
        app: demo-lock-app
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
EOF
run_cmd kubectl rollout status deployment/"$DEMO_LOCK_DEPLOY" -n "$DEMO_NS" --timeout=90s
pause

print_step "1b - Applying CheckLock ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_LOCK_CV}
  namespace: ${DEMO_NS}
spec:
  subject:
    apiGroup: apps
    resource: deployments
    names:
      - ${DEMO_LOCK_DEPLOY}
  operations:
    - DELETE
  rules:
    - name: deployment-must-not-be-locked
      type: CheckLock
      message: >
        Deployment "{{name}}" is locked. Remove earlywatch.io/lock before deletion.
EOF
pause

print_step "1c - Locking deployment and attempting delete (should be DENIED)..."
run_cmd kubectl annotate deployment "$DEMO_LOCK_DEPLOY" -n "$DEMO_NS" earlywatch.io/lock="ops-hold" --overwrite=true
print_cmd "kubectl delete deployment $DEMO_LOCK_DEPLOY -n $DEMO_NS"
if kubectl delete deployment "$DEMO_LOCK_DEPLOY" -n "$DEMO_NS" 2>&1; then
  print_error "Unexpected: locked deployment deletion was allowed."
else
  print_success "Deletion was correctly denied by CheckLock."
fi
pause

print_step "1d - Unlocking deployment and retrying delete..."
run_cmd kubectl annotate deployment "$DEMO_LOCK_DEPLOY" -n "$DEMO_NS" earlywatch.io/lock-
print_cmd "kubectl delete deployment $DEMO_LOCK_DEPLOY -n $DEMO_NS"
if kubectl delete deployment "$DEMO_LOCK_DEPLOY" -n "$DEMO_NS" --wait=true 2>&1; then
  print_success "Deployment deleted successfully after lock removal."
else
  print_error "Deletion still denied. Check deployment annotations and retry."
fi
pause
