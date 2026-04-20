#!/usr/bin/env bash
# demo-approval-check.sh - Demonstrate ApprovalCheck with watchctl approve.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_APPROVAL_CM="demo-approved-config"
DEMO_APPROVAL_CV="require-approval-signature"
TMP_DIR="$(mktemp -d)"
PRIVATE_KEY_PATH="${TMP_DIR}/private-key.pem"
PUBLIC_KEY_PATH="${TMP_DIR}/public-key.pem"

print_header "Demo - ApprovalCheck"
print_info "Scenario: deleting a ConfigMap requires a valid RSA signature annotation."
print_info "We will deny delete first, then sign with watchctl approve and retry."
pause

print_step "1a - Creating demo ConfigMap '$DEMO_APPROVAL_CM'..."
run_cmd kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-approved-config
  namespace: default
data:
  protected: "true"
EOF
pause

print_step "1b - Generating RSA key pair for ApprovalCheck demo..."
run_cmd openssl genrsa -out "$PRIVATE_KEY_PATH" 2048
run_cmd openssl rsa -in "$PRIVATE_KEY_PATH" -pubout -out "$PUBLIC_KEY_PATH"
PUBLIC_KEY_INDENTED="$(sed 's/^/          /' "$PUBLIC_KEY_PATH")"
pause

print_step "1c - Applying ApprovalCheck ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_APPROVAL_CV}
  namespace: ${DEMO_NS}
spec:
  subject:
    apiGroup: ""
    resource: configmaps
    names:
      - ${DEMO_APPROVAL_CM}
  operations:
    - DELETE
  rules:
    - name: must-have-valid-approval-signature
      type: ApprovalCheck
      approvalCheck:
        publicKey: |
${PUBLIC_KEY_INDENTED}
      message: >
        ConfigMap "{{name}}" cannot be deleted without a valid approval signature.
EOF
pause

print_step "1d - Attempting delete before signature (should be DENIED)..."
print_cmd "kubectl delete configmap $DEMO_APPROVAL_CM -n $DEMO_NS"
if kubectl delete configmap "$DEMO_APPROVAL_CM" -n "$DEMO_NS" 2>&1; then
  print_error "Unexpected: deletion was allowed before approval signature."
else
  print_success "Deletion was correctly denied by ApprovalCheck."
fi
pause

print_step "1e - Signing the resource with watchctl approve..."
run_cmd "$WATCHCTL" approve \
  --private-key "$PRIVATE_KEY_PATH" \
  --group "" \
  --version v1 \
  --resource configmaps \
  --namespace "$DEMO_NS" \
  --name "$DEMO_APPROVAL_CM"
pause

print_step "1f - Retrying delete after signature (should be ALLOWED)..."
print_cmd "kubectl delete configmap $DEMO_APPROVAL_CM -n $DEMO_NS"
if kubectl delete configmap "$DEMO_APPROVAL_CM" -n "$DEMO_NS" 2>&1; then
  print_success "ConfigMap deleted successfully after approval signature."
else
  print_error "Deletion still denied. Verify signature annotation and retry."
fi

rm -rf "$TMP_DIR"
pause
