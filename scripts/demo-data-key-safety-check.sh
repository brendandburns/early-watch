#!/usr/bin/env bash
# demo-data-key-safety-check.sh - Demonstrate DataKeySafetyCheck behavior.
set -euo pipefail

if ! declare -f print_header &>/dev/null; then
  # shellcheck source=scripts/demo-util.sh
  source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
fi

DEMO_DKSC_CM="demo-dksc-configmap"
DEMO_DKSC_POD="demo-dksc-pod"
DEMO_DKSC_CV="protect-configmap-keys"

print_header "Demo - DataKeySafetyCheck"
print_info "Scenario: block a ConfigMap UPDATE that removes a key still referenced"
print_info "by a running Pod via configMapKeyRef."
print_info "After the Pod reference is removed, the key deletion is allowed."
pause

print_step "1a - Creating ConfigMap with two keys and a Pod that references one of them..."
run_cmd kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${DEMO_DKSC_CM}
  namespace: ${DEMO_NS}
data:
  app.port: "8080"
  app.loglevel: "info"
EOF
run_cmd kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${DEMO_DKSC_POD}
  namespace: ${DEMO_NS}
spec:
  containers:
    - name: app
      image: registry.k8s.io/pause:3.9
      env:
        - name: APP_PORT
          valueFrom:
            configMapKeyRef:
              name: ${DEMO_DKSC_CM}
              key: app.port
EOF
run_cmd kubectl wait --for=condition=ready pod/"${DEMO_DKSC_POD}" -n "${DEMO_NS}" --timeout=90s
pause

print_step "1b - Applying DataKeySafetyCheck ChangeValidator..."
run_cmd kubectl apply -f - <<EOF
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: ${DEMO_DKSC_CV}
  namespace: ${DEMO_NS}
spec:
  subject:
    apiGroup: ""
    resource: configmaps
    names:
      - ${DEMO_DKSC_CM}
  operations:
    - UPDATE
  rules:
    - name: no-referenced-key-removal
      type: DataKeySafetyCheck
      dataKeySafetyCheck:
        resources:
          - apiGroup: ""
            resource: pods
            keyReferenceFields:
              - refPath: spec.containers.env.valueFrom.configMapKeyRef
              - refPath: spec.initContainers.env.valueFrom.configMapKeyRef
        sameNamespace: true
      message: >
        ConfigMap "{{name}}" cannot have a key removed because it is still
        referenced by a running Pod.
EOF
run_cmd kubectl get changevalidator "${DEMO_DKSC_CV}" -n "${DEMO_NS}"
pause

print_step "1c - Attempting to remove the referenced key 'app.port' (should be DENIED)..."
print_cmd "kubectl create configmap ${DEMO_DKSC_CM} -n ${DEMO_NS} --from-literal=app.loglevel=info --dry-run=client -o yaml | kubectl apply -f -"
if kubectl create configmap "${DEMO_DKSC_CM}" -n "${DEMO_NS}" \
    --from-literal=app.loglevel=info \
    --dry-run=client -o yaml | kubectl apply -f - 2>&1; then
  print_error "Unexpected: key removal was allowed while the Pod still references it."
else
  print_success "Key removal was correctly denied by DataKeySafetyCheck."
fi
pause

print_step "1d - Deleting the Pod and retrying the ConfigMap key removal..."
run_cmd kubectl delete pod "${DEMO_DKSC_POD}" -n "${DEMO_NS}"
print_cmd "kubectl create configmap ${DEMO_DKSC_CM} -n ${DEMO_NS} --from-literal=app.loglevel=info --dry-run=client -o yaml | kubectl apply -f -"
if kubectl create configmap "${DEMO_DKSC_CM}" -n "${DEMO_NS}" \
    --from-literal=app.loglevel=info \
    --dry-run=client -o yaml | kubectl apply -f - 2>&1; then
  print_success "ConfigMap key removed successfully once the referencing Pod was gone."
else
  print_error "Key removal still denied. Check webhook status and retry."
fi
pause
