#!/usr/bin/env bash
# demo-manual-touch-check.sh - Demonstrate ManualTouchCheck behavior.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_MANUAL_DEPLOY="demo-manual-app"
DEMO_MANUAL_CV="block-update-after-manual-touch"
DEMO_MANUAL_EVENT="demo-manual-touch-event"
EVENT_NS="early-watch-system"

print_header "Demo - ManualTouchCheck"
print_info "Scenario: block automated UPDATE when a recent manual touch exists."
print_info "We create a ManualTouchEvent, see UPDATE denied, then remove the event"
print_info "and retry the UPDATE successfully."
pause

print_step "1a - Creating demo Deployment '$DEMO_MANUAL_DEPLOY'..."
run_cmd kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-manual-app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo-manual-app
  template:
    metadata:
      labels:
        app: demo-manual-app
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
EOF
run_cmd kubectl rollout status deployment/"$DEMO_MANUAL_DEPLOY" -n "$DEMO_NS" --timeout=90s
pause

print_step "1b - Applying ManualTouchCheck ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_MANUAL_CV}
  namespace: ${DEMO_NS}
spec:
  subject:
    apiGroup: apps
    resource: deployments
    names:
      - ${DEMO_MANUAL_DEPLOY}
  operations:
    - UPDATE
  rules:
    - name: no-recent-manual-touch
      type: ManualTouchCheck
      manualTouchCheck:
        windowDuration: "24h"
        eventNamespace: ${EVENT_NS}
      message: >
        Deployment "{{name}}" was recently touched manually; update is blocked.
EOF
pause

print_step "1c - Creating a recent ManualTouchEvent for this deployment..."
NOW_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ManualTouchEvent
metadata:
  name: ${DEMO_MANUAL_EVENT}
  namespace: ${EVENT_NS}
  labels:
    earlywatch.io/resource: deployments
    earlywatch.io/resource-namespace: ${DEMO_NS}
    earlywatch.io/resource-name: ${DEMO_MANUAL_DEPLOY}
    earlywatch.io/api-group: apps
spec:
  timestamp: "${NOW_UTC}"
  user: demo-operator
  userAgent: kubectl/v1.demo
  operation: UPDATE
  apiGroup: apps
  resource: deployments
  resourceName: ${DEMO_MANUAL_DEPLOY}
  resourceNamespace: ${DEMO_NS}
  sourceIP: 127.0.0.1
  auditID: demo-manual-touch-audit
  monitorName: demo-monitor
  monitorNamespace: default
EOF
run_cmd kubectl get manualtouchevent "$DEMO_MANUAL_EVENT" -n "$EVENT_NS"
pause

print_step "1d - Attempting UPDATE while event exists (should be DENIED)..."
print_cmd "kubectl scale deployment $DEMO_MANUAL_DEPLOY -n $DEMO_NS --replicas=2"
if kubectl scale deployment "$DEMO_MANUAL_DEPLOY" -n "$DEMO_NS" --replicas=2 2>&1; then
  print_error "Unexpected: update was allowed despite recent manual touch event."
else
  print_success "Update was correctly denied by ManualTouchCheck."
fi
pause

print_step "1e - Deleting event and retrying UPDATE..."
run_cmd kubectl delete manualtouchevent "$DEMO_MANUAL_EVENT" -n "$EVENT_NS" --ignore-not-found=true
print_cmd "kubectl scale deployment $DEMO_MANUAL_DEPLOY -n $DEMO_NS --replicas=2"
if kubectl scale deployment "$DEMO_MANUAL_DEPLOY" -n "$DEMO_NS" --replicas=2 2>&1; then
  print_success "Deployment update succeeded after removing the event."
else
  print_error "Update still denied. Verify event cleanup and retry."
fi
pause
