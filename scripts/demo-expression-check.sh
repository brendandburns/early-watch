#!/usr/bin/env bash
# demo-expression-check.sh - Demonstrate ExpressionCheck behavior.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_EXPR_CM="demo-expression-cm"
DEMO_EXPR_CV="deny-delete-demo-expression-cm"

print_header "Demo - ExpressionCheck"
print_info "Scenario: block DELETE for one ConfigMap by matching request.name."
print_info "Deleting a different ConfigMap remains allowed."
pause

print_step "1a - Creating target and control ConfigMaps..."
run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-expression-cm
  namespace: default
data:
  hello: world
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-expression-control
  namespace: default
data:
  hello: control
EOF
pause

print_step "1b - Applying ExpressionCheck ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_EXPR_CV}
  namespace: ${DEMO_NS}
spec:
  subject:
    apiGroup: ""
    resource: configmaps
  operations:
    - DELETE
  rules:
    - name: block-delete-by-name
      type: ExpressionCheck
      expressionCheck:
        expression: "name == '${DEMO_EXPR_CM}'"
      message: >
        ConfigMap "{{name}}" is protected by ExpressionCheck and cannot be deleted.
EOF
pause

print_step "1c - Deleting protected ConfigMap (should be DENIED)..."
print_cmd "kubectl delete configmap $DEMO_EXPR_CM -n $DEMO_NS"
if kubectl delete configmap "$DEMO_EXPR_CM" -n "$DEMO_NS" 2>&1; then
  print_error "Unexpected: protected ConfigMap deletion was allowed."
else
  print_success "Deletion was correctly denied by ExpressionCheck."
fi
pause

print_step "1d - Deleting control ConfigMap (should be ALLOWED)..."
print_cmd "kubectl delete configmap demo-expression-control -n $DEMO_NS"
if kubectl delete configmap demo-expression-control -n "$DEMO_NS" 2>&1; then
  print_success "Control ConfigMap deleted successfully."
else
  print_error "Unexpected denial for control ConfigMap deletion."
fi

print_step "1e - Cleaning up protected ConfigMap after removing validator..."
run_cmd kubectl delete changevalidator "$DEMO_EXPR_CV" -n "$DEMO_NS" --ignore-not-found=true
run_cmd kubectl delete configmap "$DEMO_EXPR_CM" -n "$DEMO_NS" --ignore-not-found=true
pause
