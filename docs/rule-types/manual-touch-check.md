# Rule Type: ManualTouchCheck

The `ManualTouchCheck` rule type **denies the request when a recent manual touch (kubectl operation) has been recorded for the same resource within a configurable look-back window**.  Use this to prevent automated pipelines from overwriting an operator's manual change.

---

## When to Use

Use `ManualTouchCheck` when you run automated reconciliation (GitOps, CI/CD) that normally owns a set of resources, but you want to protect any resource that an operator has recently touched manually.  For example:

- A Flux or ArgoCD reconciliation loop that syncs Deployment replicas should not overwrite a manual `kubectl scale` that an operator applied minutes ago.
- An automated job that recreates Secrets should be blocked if an operator recently updated one by hand.

### Prerequisites

The `ManualTouchCheck` rule requires the **audit monitor** component to be running.  The audit monitor watches the Kubernetes audit log and writes `ManualTouchEvent` resources for each detected manual touch.  Configure what counts as a manual touch with a `ManualTouchMonitor` resource.

See [custom-resources/manual-touch-monitor.md](../custom-resources/manual-touch-monitor.md).

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `windowDuration` | string | No | Go duration string for the look-back window, e.g. `"30m"`, `"2h"`, `"24h"`. Defaults to `"1h"` when omitted. |
| `eventNamespace` | string | No | Namespace where `ManualTouchEvent` resources are stored. Defaults to `"early-watch-system"` when omitted. |

---

## Example

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-manual-changes
  namespace: default
spec:
  subject:
    apiGroup: apps
    resource: deployments
  operations:
    - UPDATE
  rules:
    - name: no-recent-manual-touch
      type: ManualTouchCheck
      manualTouchCheck:
        windowDuration: "2h"
        eventNamespace: early-watch-system
      message: >
        Deployment "{{name}}" was recently modified manually. Automated updates
        are blocked for 2 hours after a manual touch. If you want to proceed,
        delete the corresponding ManualTouchEvent in the early-watch-system namespace.
```

---

## Workflow

1. An operator runs `kubectl scale deployment my-app --replicas=5`.
2. The audit monitor detects the manual touch and writes a `ManualTouchEvent` in `early-watch-system`.
3. Within the next 2 hours, an automated pipeline tries to update the same Deployment.
4. The `ManualTouchCheck` rule finds the recent event and denies the update.
5. After the window expires (or after the operator deletes the event), automated updates proceed normally.

---

## Querying Events

```bash
# List all recorded manual touch events
kubectl get manualtouchevents -n early-watch-system

# Describe a specific event
kubectl describe manualtouchevent <name> -n early-watch-system

# Remove an event to unblock automation before the window expires
kubectl delete manualtouchevent <name> -n early-watch-system
```
