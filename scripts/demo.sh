#!/usr/bin/env bash
# demo.sh — Interactive EarlyWatch demo scenarios.
#
# This script walks through two concrete examples of EarlyWatch protecting
# Kubernetes resources from unsafe deletions.
#
# Run scripts/demo-setup.sh first to create the kind cluster and install
# EarlyWatch, then run this script to walk through the demo scenarios.
#
# Usage:
#   bash scripts/demo.sh [--skip-cleanup]
#
#   --skip-cleanup  Leave the kind cluster running after the demo finishes.
#                   Useful if you want to explore further after the demo.
set -euo pipefail

# ── Flags ────────────────────────────────────────────────────────────────────
SKIP_CLEANUP=false
for arg in "$@"; do
  case "$arg" in
    --skip-cleanup) SKIP_CLEANUP=true ;;
  esac
done

# ── Shared utilities ─────────────────────────────────────────────────────────
# shellcheck source=scripts/demo-util.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"

# ── Cleanup on exit ──────────────────────────────────────────────────────────
cleanup() {
  if [ "$SKIP_CLEANUP" = "true" ]; then
    echo ""
    print_info "Skipping cleanup (--skip-cleanup was set)."
    print_info "Run 'kind delete cluster --name $CLUSTER_NAME' to remove the cluster."
    return
  fi
  echo ""
  echo "${YELLOW}Cleaning up demo resources...${RESET}"
  kubectl delete service        demo-service --ignore-not-found=true
  kubectl delete pod            demo-pod     --ignore-not-found=true
  kubectl delete configmap      demo-config  --ignore-not-found=true
  kubectl delete deployment     demo-app     --ignore-not-found=true
  kubectl delete changevalidator protect-service-from-deletion   -n default --ignore-not-found=true
  kubectl delete changevalidator protect-configmap-from-deletion -n default --ignore-not-found=true
  "$WATCHCTL" uninstall --kubeconfig "$HOME/.kube/config" || true
  kind delete cluster --name "$CLUSTER_NAME"
  print_success "Cleanup complete."
}
trap cleanup EXIT

# ── Verify setup has been run ────────────────────────────────────────────────
if ! kubectl get namespace early-watch-system &>/dev/null; then
  echo "${RED}Error: EarlyWatch does not appear to be installed.${RESET}"
  echo "Run ${BOLD}scripts/demo-setup.sh${RESET} first, then re-run this script."
  exit 1
fi

if [ ! -x "$WATCHCTL" ]; then
  echo "${RED}Error: watchctl binary not found at $WATCHCTL${RESET}"
  echo "Run ${BOLD}scripts/demo-setup.sh${RESET} first to build it."
  exit 1
fi

# ── Welcome banner ───────────────────────────────────────────────────────────
clear
cat <<'BANNER'
  ______           _      __          __   _       _
 |  ____|         | |     \ \        / /  | |     | |
 | |__   __ _ _ __| |_   _ \ \  /\  / /_ | |_ ___| |__
 |  __| / _` | '__| | | | | \ \/  \/ / _\| __/ __| '_ \
 | |___| (_| | |  | | |_| |  \  /\  / (_| | || (__| | | |
 |______\__,_|_|  |_|\__, |   \/  \/ \__,_|\__/\___|_| |_|
                       __/ |
                      |___/    Interactive Demo
BANNER
echo ""
echo "${BOLD}Welcome to the EarlyWatch interactive demo!${RESET}"
echo ""
echo "EarlyWatch is a Kubernetes admission controller that enforces"
echo "change-safety rules — it prevents you from accidentally breaking"
echo "your cluster by deleting resources that are still in use."
echo ""
echo "During this demo you will see:"
echo "  1. A Service blocked from deletion because matching Pods are running"
echo "  2. A ConfigMap blocked from deletion because a Deployment references it"
echo "  3. Both deletions successfully completing once dependencies are removed"
echo ""
echo "${DIM}Estimated run time: ~3 minutes${RESET}"
pause

# ── Demo 1 — Protect a Service ───────────────────────────────────────────────
print_header "Demo 1 — Protect a Service from Deletion"
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

# ── Demo 2 — Protect a ConfigMap ────────────────────────────────────────────
print_header "Demo 2 — Protect a ConfigMap Referenced by a Deployment"
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

# ── Uninstall ────────────────────────────────────────────────────────────────
print_header "Uninstall EarlyWatch"
print_info "watchctl uninstall removes all EarlyWatch components from the cluster"
print_info "in the correct order: first the ValidatingWebhookConfiguration (so no"
print_info "more requests are intercepted), then the Deployment, Service, RBAC,"
print_info "and finally the CRDs."
print_info ""
print_info "Expected outcome: the 'early-watch-system' namespace and all CRDs"
print_info "are gone and the API server no longer routes admission requests to"
print_info "EarlyWatch."
pause

run_cmd "$WATCHCTL" uninstall --kubeconfig "$HOME/.kube/config"

echo ""
print_info "Verifying resources were removed..."
if ! kubectl get namespace early-watch-system &>/dev/null; then
  print_success "'early-watch-system' namespace removed."
else
  print_info  "Namespace is terminating (Kubernetes finalizer cleanup in progress)."
fi

if ! kubectl get crd changevalidators.earlywatch.io &>/dev/null; then
  print_success "'changevalidators.earlywatch.io' CRD removed."
fi

pause

# ── Fin ──────────────────────────────────────────────────────────────────────
print_header "Demo Complete!"
echo ""
echo "You have seen EarlyWatch:"
echo "  ${GREEN}✔${RESET}  Blocking a Service deletion while Pods are running"
echo "  ${GREEN}✔${RESET}  Blocking a ConfigMap deletion while a Deployment references it"
echo "  ${GREEN}✔${RESET}  Allowing deletions once their dependencies are cleaned up"
echo "  ${GREEN}✔${RESET}  Cleanly uninstalled from the cluster"
echo ""
echo "Next steps:"
echo "  • Explore other sample ChangeValidators in config/samples/"
echo "  • Read the rule-type docs:  docs/rule-types/"
echo "  • Build your own ChangeValidator for your workloads"
echo ""
echo "${DIM}docs/getting-started.md has a full walkthrough with more examples.${RESET}"
echo ""

# The EXIT trap calls cleanup() automatically when the script exits.
# Nothing else needed here.
