#!/usr/bin/env bash
# demo-annotation-check.sh - Demonstrate AnnotationCheck confirm-delete behavior.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_ANNOTATION_NS="demo-annotation-ns"
DEMO_ANNOTATION_CV="require-annotation-confirm-delete"

print_header "Demo - AnnotationCheck Confirm Delete"
print_info "Scenario: deleting a protected namespace requires an explicit annotation."
print_info "We will create a namespace, block deletion without confirmation,"
print_info "then add the required annotation and delete successfully."
pause

print_step "1a - Creating protected namespace '$DEMO_ANNOTATION_NS'..."
run_cmd kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${DEMO_ANNOTATION_NS}
EOF
run_cmd kubectl label namespace "$DEMO_ANNOTATION_NS" purpose=annotation-demo --overwrite=true
pause

print_step "1b - Applying AnnotationCheck ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_ANNOTATION_CV}
spec:
  subject:
    apiGroup: ""
    resource: namespaces
    names:
      - ${DEMO_ANNOTATION_NS}
  operations:
    - DELETE
  rules:
    - name: must-have-confirm-delete
      type: AnnotationCheck
      annotationCheck:
        annotationKey: earlywatch.io/confirm-delete
        annotationValue: "yes"
      message: >
        Namespace "{{name}}" cannot be deleted until it is annotated with
        earlywatch.io/confirm-delete=yes.
EOF
run_cmd kubectl get changevalidator "$DEMO_ANNOTATION_CV"
pause

print_step "1c - Attempting namespace delete before annotation (should be DENIED)..."
print_cmd "kubectl delete namespace $DEMO_ANNOTATION_NS"
if kubectl delete namespace "$DEMO_ANNOTATION_NS" 2>&1; then
  print_error "Unexpected: deletion was allowed before confirmation annotation."
else
  print_success "Deletion was correctly denied by AnnotationCheck."
fi
pause

print_step "1d - Adding confirmation annotation and retrying delete..."
run_cmd kubectl annotate namespace "$DEMO_ANNOTATION_NS" earlywatch.io/confirm-delete=yes --overwrite=true
print_cmd "kubectl delete namespace $DEMO_ANNOTATION_NS"
if kubectl delete namespace "$DEMO_ANNOTATION_NS" 2>&1; then
  print_success "Namespace deleted successfully after explicit confirmation."
else
  print_error "Deletion still denied. Check webhook status and retry."
fi
pause
