#!/usr/bin/env bash
# demo.sh — Interactive EarlyWatch demo scenarios.
#
# This script optionally installs EarlyWatch onto the cluster (Steps 1 and 2),
# then walks through two concrete examples of EarlyWatch protecting Kubernetes
# resources from unsafe deletions.
#
# Run scripts/demo-setup.sh first to create the kind cluster and download
# watchctl, then run this script.
# To tear everything down afterwards, run scripts/demo-teardown.sh.
#
# Prerequisites (all must be on your PATH):
#   • kubectl — https://kubernetes.io/docs/tasks/tools/
#
# Usage:
#   bash scripts/demo.sh [--skip-cleanup] [--skip-earlywatch-install]
#                        [--image-pull-secret=<path>]
#
#   --skip-cleanup              Skip automatic teardown when the script exits.
#                               Run scripts/demo-teardown.sh manually to clean up later.
#   --skip-earlywatch-install   Skip the EarlyWatch install and resource inspection
#                               steps (Steps 1 and 2). Use this when EarlyWatch is
#                               already installed on the cluster (e.g. after a previous run).
#   --image-pull-secret=<path>  Path to a Docker config JSON file (e.g. ~/.docker/config.json)
#                               used to pull images from a private registry. The script creates
#                               a Kubernetes Secret named "pullSecret" in the early-watch-system
#                               namespace from this file (optional).
set -euo pipefail

# ── Flags ────────────────────────────────────────────────────────────────────
SKIP_CLEANUP=false
SKIP_EARLYWATCH_INSTALL=false
IMAGE_PULL_SECRET=""
for arg in "$@"; do
  case "$arg" in
    --skip-cleanup)             SKIP_CLEANUP=true ;;
    --skip-earlywatch-install)  SKIP_EARLYWATCH_INSTALL=true ;;
    --image-pull-secret=*)      IMAGE_PULL_SECRET="${arg#--image-pull-secret=}" ;;
  esac
done

# ── Shared utilities ─────────────────────────────────────────────────────────
# shellcheck source=scripts/demo-util.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"

# ── Cleanup / keep-terminal-open on exit ────────────────────────────────────
cleanup() {
  if [ "$SKIP_CLEANUP" = "true" ]; then
    echo ""
    print_info "Skipping cleanup (--skip-cleanup was set)."
    print_info "Run 'bash scripts/demo-teardown.sh' to clean up when you are done."
    return
  fi
  bash "$(dirname "${BASH_SOURCE[0]}")/demo-teardown.sh"
}

_on_exit() {
  cleanup
  echo ""
  echo -n "${DIM}   Press Enter to close...${RESET}"
  read -r _
}
trap '_on_exit' EXIT

# ── Verify cluster and watchctl are available ────────────────────────────────
if ! kubectl cluster-info &>/dev/null; then
  echo "${RED}Error: cannot reach a Kubernetes cluster.${RESET}"
  echo "Run ${BOLD}scripts/demo-setup.sh${RESET} first to create a kind cluster."
  exit 1
fi

if [ ! -x "$WATCHCTL" ]; then
  echo "${RED}Error: watchctl binary not found at $WATCHCTL${RESET}"
  echo "Run ${BOLD}scripts/demo-setup.sh${RESET} first to download or build it."
  exit 1
fi

# ── Step 1 (optional): Install EarlyWatch ───────────────────────────────────
if [ "$SKIP_EARLYWATCH_INSTALL" = "true" ]; then
  print_header "Step 1 — EarlyWatch Install Skipped"
  print_info "Skipping EarlyWatch install (--skip-earlywatch-install was set)."
  if ! kubectl get namespace early-watch-system &>/dev/null; then
    print_error "EarlyWatch namespace 'early-watch-system' not found."
    print_info  "Remove --skip-earlywatch-install so the script can install it automatically."
    exit 1
  fi
  print_success "EarlyWatch namespace found — assuming it is already installed."
  pause
else
  print_header "Step 1 — Install EarlyWatch onto the Cluster"
  print_info "watchctl install applies the following resources in one go:"
  print_info "  • ChangeValidator CRD  — defines the custom resource type"
  print_info "  • RBAC (ClusterRole + ClusterRoleBinding + ServiceAccount)"
  print_info "  • Webhook Deployment   — the admission controller pod"
  print_info "  • Webhook Service      — exposes the controller inside the cluster"
  print_info "  • ValidatingWebhookConfiguration — registers with the API server"
  print_info ""
  print_info "The install is idempotent; running it twice is safe."
  print_info ""
  print_info "Expected outcome: all EarlyWatch components are Running in the"
  print_info "'early-watch-system' namespace."
  pause

  INSTALL_ARGS=()
  if [ -n "$IMAGE_PULL_SECRET" ]; then
    if [ ! -f "$IMAGE_PULL_SECRET" ]; then
      print_error "Docker config file not found: $IMAGE_PULL_SECRET"
      exit 1
    fi
    print_info "Ensuring namespace 'early-watch-system' exists..."
    run_cmd "kubectl create namespace early-watch-system --dry-run=client -o yaml | kubectl apply -f -"
    print_info "Creating image pull secret 'pullSecret' in early-watch-system..."
    run_cmd "kubectl create secret generic pullSecret \
    --from-file=.dockerconfigjson=\"$IMAGE_PULL_SECRET\" \
    --type=kubernetes.io/dockerconfigjson \
    --namespace=early-watch-system \
    --dry-run=client -o yaml \
    | kubectl apply -f -"
    INSTALL_ARGS+=("--image-pull-secret" "pullSecret")
  fi
  run_cmd "$WATCHCTL" install "${INSTALL_ARGS[@]}"

  echo ""
  print_info "Waiting for the webhook deployment to become ready (up to 120s)..."
  run_cmd kubectl rollout status deployment/early-watch-webhook \
    -n early-watch-system --timeout=120s

  print_success "EarlyWatch is installed and ready."
  pause
fi

# ── Step 2 (optional): Inspect installed resources ──────────────────────────
if [ "$SKIP_EARLYWATCH_INSTALL" = "false" ]; then
  print_header "Step 2 — Inspect the Installed Resources"
  print_info "Let's take a look at what was created."
  print_info ""
  print_info "You should see:"
  print_info "  • The 'early-watch-system' namespace"
  print_info "  • The webhook pod in a Running state"
  print_info "  • The 'changevalidators.earlywatch.io' CRD"
  print_info "  • The ValidatingWebhookConfiguration that hooks into the API server"
  pause

  echo ""
  echo "${BOLD}Namespace:${RESET}"
  run_cmd kubectl get namespace early-watch-system

  echo ""
  echo "${BOLD}Pods in early-watch-system:${RESET}"
  run_cmd kubectl get pods -n early-watch-system

  echo ""
  echo "${BOLD}ChangeValidator CRD:${RESET}"
  run_cmd kubectl get crd changevalidators.earlywatch.io

  echo ""
  echo "${BOLD}ValidatingWebhookConfiguration:${RESET}"
  run_cmd kubectl get validatingwebhookconfiguration early-watch-validating-webhook

  pause
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
if [ "$SKIP_EARLYWATCH_INSTALL" = "false" ]; then
  echo "  ${GREEN}✔${RESET}  EarlyWatch installed onto the cluster"
fi
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

# The EXIT trap calls cleanup() automatically when the script exits, which
# in turn runs demo-teardown.sh unless --skip-cleanup was passed.
