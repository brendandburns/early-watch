#!/usr/bin/env bash
# demo.sh — Interactive EarlyWatch demo scenarios.
#
# This script optionally installs EarlyWatch onto the cluster (Steps 1 and 2),
# then walks through concrete examples of EarlyWatch protecting Kubernetes
# resources from unsafe deletions.  Each demo scenario lives in its own script
# under scripts/:
#
#   demo-1-service.sh   — Protect a Service while matching Pods are running
#   demo-2-configmap.sh — Protect a ConfigMap referenced by a Deployment
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
#                        [--demos=<comma-separated list>]
#
#   --skip-cleanup              Skip automatic teardown when the script exits.
#                               Run scripts/demo-teardown.sh manually to clean up later.
#   --skip-earlywatch-install   Skip the EarlyWatch install and resource inspection
#                               steps (Steps 1 and 2). Use this when EarlyWatch is
#                               already installed on the cluster (e.g. after a previous run).
#   --image-pull-secret=<path>  Path to a Docker config JSON file (e.g. ~/.docker/config.json)
#                               used to pull images from a private registry. The script creates
#                               a Kubernetes Secret named "pullsecret" in the early-watch-system
#                               namespace from this file (optional).
#   --demos=<list>              Comma-separated list of demo numbers to run (default: all).
#                               Examples: --demos=1   --demos=2   --demos=1,2
set -euo pipefail

# ── Flags ────────────────────────────────────────────────────────────────────
SKIP_CLEANUP=false
SKIP_EARLYWATCH_INSTALL=false
IMAGE_PULL_SECRET=""
DEMOS_ARG=""
for arg in "$@"; do
  case "$arg" in
    --skip-cleanup)             SKIP_CLEANUP=true ;;
    --skip-earlywatch-install)  SKIP_EARLYWATCH_INSTALL=true ;;
    --image-pull-secret=*)      IMAGE_PULL_SECRET="${arg#--image-pull-secret=}" ;;
    --demos=*)                  DEMOS_ARG="${arg#--demos=}" ;;
  esac
done

# Build the set of demos to run.  Default: all demos.
ALL_DEMOS=(1 2)
DEMOS=()
if [ -z "$DEMOS_ARG" ]; then
  DEMOS=("${ALL_DEMOS[@]}")
else
  IFS=',' read -ra DEMOS <<< "$DEMOS_ARG"
  # Validate that every requested demo number is a positive integer that
  # corresponds to a known demo (1..${#ALL_DEMOS[@]}).
  for d in "${DEMOS[@]}"; do
    if ! [[ "$d" =~ ^[1-9][0-9]*$ ]] || [ "$d" -gt "${#ALL_DEMOS[@]}" ]; then
      echo "${RED}Error: invalid demo number '${d}'. Valid values are: ${ALL_DEMOS[*]}${RESET}"
      exit 1
    fi
  done
fi

# Helper: returns 0 if demo number $1 is in the DEMOS array.
_demo_selected() {
  local n="$1"
  for d in "${DEMOS[@]}"; do
    if [ "$d" = "$n" ]; then return 0; fi
  done
  return 1
}

# ── Shared utilities ─────────────────────────────────────────────────────────
# shellcheck source=scripts/demo-util.sh
source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"

# ── Cleanup on exit ──────────────────────────────────────────────────────────
cleanup() {
  if [ "$SKIP_CLEANUP" = "true" ]; then
    echo ""
    print_info "Skipping cleanup (--skip-cleanup was set)."
    print_info "Run 'bash scripts/demo-teardown.sh' to clean up when you are done."
    return
  fi
  bash "$(dirname "${BASH_SOURCE[0]}")/demo-teardown.sh"
}
trap cleanup EXIT

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
    print_info "Creating image pull secret 'pullsecret' in early-watch-system..."
    run_cmd "kubectl create secret generic pullsecret \
    --from-file=.dockerconfigjson=\"$IMAGE_PULL_SECRET\" \
    --type=kubernetes.io/dockerconfigjson \
    --namespace=early-watch-system \
    --dry-run=client -o yaml \
    | kubectl apply -f -"
    INSTALL_ARGS+=("--image-pull-secret" "pullsecret")
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
_demo_selected 1 && echo "  1. A Service blocked from deletion because matching Pods are running"
_demo_selected 2 && echo "  2. A ConfigMap blocked from deletion because a Deployment references it"
echo "  • Each deletion successfully completing once dependencies are removed"
echo ""
echo "${DIM}Estimated run time: ~$((${#DEMOS[@]} + 1)) minute(s)  (≈1 min per demo + 1 min for install/uninstall)${RESET}"
pause

# ── Run selected demos ────────────────────────────────────────────────────────
SCRIPTS_DIR="$(dirname "${BASH_SOURCE[0]}")"

if _demo_selected 1; then
  # shellcheck source=scripts/demo-1-service.sh
  source "$SCRIPTS_DIR/demo-1-service.sh"
fi

if _demo_selected 2; then
  # shellcheck source=scripts/demo-2-configmap.sh
  source "$SCRIPTS_DIR/demo-2-configmap.sh"
fi

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
_demo_selected 1 && echo "  ${GREEN}✔${RESET}  Blocking a Service deletion while Pods are running"
_demo_selected 2 && echo "  ${GREEN}✔${RESET}  Blocking a ConfigMap deletion while a Deployment references it"
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
