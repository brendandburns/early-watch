#!/usr/bin/env bash
# demo-cluster-change-validator.sh - Demonstrate ClusterChangeValidator behavior.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_CLUSTER_NS="demo-prod-ns"
DEMO_CLUSTER_SVC="demo-prod-service"
DEMO_CLUSTER_CCV="protect-prod-services-demo"

print_header "Demo - ClusterChangeValidator"
print_info "Scenario: a ClusterChangeValidator blocks Service deletion in any"
print_info "namespace labelled env=prod, without needing a per-namespace validator."
print_info "Deleting the same Service in an unlabelled namespace remains allowed."
pause

print_step "1a - Creating a production namespace labelled env=prod..."
run_cmd kubectl create namespace "$DEMO_CLUSTER_NS" --dry-run=client -o yaml \
  | kubectl apply -f -
run_cmd kubectl label namespace "$DEMO_CLUSTER_NS" env=prod --overwrite
pause

print_step "1b - Creating a Service in the production namespace..."
run_cmd kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: ${DEMO_CLUSTER_SVC}
  namespace: ${DEMO_CLUSTER_NS}
spec:
  selector:
    app: demo-prod
  ports:
    - port: 80
      targetPort: 8080
EOF
pause

print_step "1c - Applying ClusterChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ClusterChangeValidator
metadata:
  name: ${DEMO_CLUSTER_CCV}
spec:
  subject:
    apiGroup: ""
    resource: services
    namespaceSelector:
      matchLabels:
        env: prod
  operations:
    - DELETE
  rules:
    - name: deny-prod-service-deletion
      type: ExpressionCheck
      expressionCheck:
        expression: "operation == 'DELETE'"
      message: >
        Deleting Services in production namespaces is not allowed by cluster
        policy. Remove the env=prod label from the namespace or obtain an
        explicit approval before proceeding.
EOF
pause

print_step "1d - Attempting to delete Service in the prod namespace (should be DENIED)..."
print_cmd "kubectl delete service ${DEMO_CLUSTER_SVC} -n ${DEMO_CLUSTER_NS}"
if kubectl delete service "$DEMO_CLUSTER_SVC" -n "$DEMO_CLUSTER_NS" 2>&1; then
  print_error "Unexpected: Service deletion in prod namespace was allowed."
else
  print_success "Deletion was correctly denied by ClusterChangeValidator."
fi
pause

print_step "1e - Removing the env=prod label and retrying deletion (should be ALLOWED)..."
run_cmd kubectl label namespace "$DEMO_CLUSTER_NS" env- --overwrite
print_cmd "kubectl delete service ${DEMO_CLUSTER_SVC} -n ${DEMO_CLUSTER_NS}"
if kubectl delete service "$DEMO_CLUSTER_SVC" -n "$DEMO_CLUSTER_NS" 2>&1; then
  print_success "Deletion succeeded once the namespace no longer carries env=prod."
else
  print_error "Deletion still denied after removing prod label."
fi

print_step "1f - Cleaning up..."
run_cmd kubectl delete clusterchangevalidator "$DEMO_CLUSTER_CCV" --ignore-not-found=true
run_cmd kubectl delete namespace "$DEMO_CLUSTER_NS" --ignore-not-found=true
pause
