#!/usr/bin/env bash
# demo-service-pod-selector-check.sh - Demonstrate ServicePodSelectorCheck behavior.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_SELECTOR_SERVICE="demo-selector-service"
DEMO_SELECTOR_OLD_POD="demo-selector-pod"
DEMO_SELECTOR_NEW_POD="demo-selector-new-pod"
DEMO_SELECTOR_CV="protect-service-selector-update"

print_header "Demo - ServicePodSelectorCheck"
print_info "Scenario: block a Service UPDATE that would drop all matching Pods."
print_info "After we create Pods for the new selector, the UPDATE is allowed."
pause

print_step "1a - Creating Service and initial matching Pod..."
run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: demo-selector-service
  namespace: default
spec:
  selector:
    app: selector-live
  ports:
    - port: 80
      targetPort: 8080
---
apiVersion: v1
kind: Pod
metadata:
  name: demo-selector-pod
  namespace: default
  labels:
    app: selector-live
spec:
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
EOF
run_cmd kubectl wait --for=condition=ready pod/"$DEMO_SELECTOR_OLD_POD" -n "$DEMO_NS" --timeout=90s
pause

print_step "1b - Applying ServicePodSelectorCheck ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_SELECTOR_CV}
  namespace: ${DEMO_NS}
spec:
  subject:
    apiGroup: ""
    resource: services
    names:
      - ${DEMO_SELECTOR_SERVICE}
  operations:
    - UPDATE
  rules:
    - name: service-must-not-drop-all-pods
      type: ServicePodSelectorCheck
      servicePodSelectorCheck: {}
      message: >
        Service "{{name}}" update would drop all selected Pods and is blocked.
EOF
pause

print_step "1c - Updating selector to no-pods value (should be DENIED)..."
print_cmd "kubectl patch service $DEMO_SELECTOR_SERVICE -n $DEMO_NS --type=merge -p '{\"spec\":{\"selector\":{\"app\":\"selector-new\"}}}'"
if kubectl patch service "$DEMO_SELECTOR_SERVICE" -n "$DEMO_NS" --type=merge -p '{"spec":{"selector":{"app":"selector-new"}}}' 2>&1; then
  print_error "Unexpected: selector update was allowed even though it matched no Pods."
else
  print_success "Update was correctly denied by ServicePodSelectorCheck."
fi
pause

print_step "1d - Creating Pod for new selector and retrying update..."
run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: demo-selector-new-pod
  namespace: default
  labels:
    app: selector-new
spec:
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
EOF
run_cmd kubectl wait --for=condition=ready pod/"$DEMO_SELECTOR_NEW_POD" -n "$DEMO_NS" --timeout=90s
print_cmd "kubectl patch service $DEMO_SELECTOR_SERVICE -n $DEMO_NS --type=merge -p '{\"spec\":{\"selector\":{\"app\":\"selector-new\"}}}'"
if kubectl patch service "$DEMO_SELECTOR_SERVICE" -n "$DEMO_NS" --type=merge -p '{"spec":{"selector":{"app":"selector-new"}}}' 2>&1; then
  print_success "Selector update succeeded once matching Pods existed for the new selector."
else
  print_error "Update still denied. Check pod readiness and retry."
fi
pause
