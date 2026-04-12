# ManualTouchMonitor CRD Reference

`ManualTouchMonitor` declares which Kubernetes resources and operations the EarlyWatch audit monitor should watch for manual (e.g. kubectl) touches.  When the audit monitor detects a matching operation it writes a `ManualTouchEvent` resource that can later be queried by the `ManualTouchCheck` rule type.

API group: `earlywatch.io/v1alpha1`  
Short name: `mtm`  
Scope: Namespaced

---

## Example

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ManualTouchMonitor
metadata:
  name: monitor-kubectl-deletes
  namespace: default
spec:
  subjects:
    - apiGroup: ""        # core API group
      resource: services
    - apiGroup: apps
      resource: deployments
  operations:
    - DELETE
    - CREATE
    - UPDATE
  userAgentPatterns:
    - "^kubectl/"
  excludeServiceAccounts:
    - "system:serviceaccount:ci-system:pipeline-bot"
```

Query recorded events:

```bash
kubectl get manualtouchevents -n early-watch-system
```

---

## spec.subjects

A list of Kubernetes resource types to monitor.  At least one subject is required.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiGroup` | string | No | API group; use `""` for core resources. |
| `resource` | string | Yes | Plural resource name, e.g. `services`, `deployments`. |
| `namespaceSelector` | LabelSelector | No | Restrict monitoring to namespaces whose labels match this selector. |

---

## spec.operations

A list of operations to flag as manual touches.  Valid values: `DELETE`, `CREATE`, `UPDATE`.  At least one value is required.

---

## spec.userAgentPatterns

A list of regular expressions that identify "manual" user agents.  A request is considered a manual touch when its `User-Agent` header matches at least one pattern.  Defaults to `["^kubectl/"]` when the list is empty.

Common patterns:

| Tool | Pattern |
|------|---------|
| kubectl | `^kubectl/` |
| k9s | `^k9s/` |
| Lens | `^OpenAPI-Generator/` |

---

## spec.excludeServiceAccounts

A list of Kubernetes service account usernames (in the form `system:serviceaccount:<ns>:<name>`) whose operations should never be flagged as manual touches.  Use this to exclude CI/CD automation that runs under a known service account.

```yaml
excludeServiceAccounts:
  - "system:serviceaccount:ci-system:pipeline-bot"
  - "system:serviceaccount:flux-system:flux-reconciler"
```

---

## spec.alerting

Optional notification sink configuration.

| Field | Type | Description |
|-------|------|-------------|
| `prometheusLabels` | map[string]string | Static labels added to the `earlywatch_manual_touch_total` Prometheus counter for events detected by this monitor. |

---

## ManualTouchEvent

Each detected manual touch is recorded as a `ManualTouchEvent` resource (short name: `mte`) in the namespace configured on the `ManualTouchCheck` rule (defaults to `early-watch-system`).

Key fields:

| Field | Description |
|-------|-------------|
| `spec.timestamp` | Time the operation was received by the API server. |
| `spec.user` | Kubernetes username that performed the operation. |
| `spec.userAgent` | Raw User-Agent string, e.g. `kubectl/v1.29.0 (linux/amd64)`. |
| `spec.operation` | HTTP verb: `DELETE`, `CREATE`, or `UPDATE`. |
| `spec.resource` | Plural resource type that was touched. |
| `spec.resourceName` | Name of the specific resource. |
| `spec.resourceNamespace` | Namespace of the resource (empty for cluster-scoped). |
| `spec.auditID` | Kubernetes audit event ID for cross-referencing with the raw audit log. |
| `spec.monitorName` | Name of the `ManualTouchMonitor` that generated this event. |

---

## Full CRD Schema

The generated CRD YAML is at:  
[`config/crd/bases/earlywatch.io_changevalidators.yaml`](../../config/crd/bases/earlywatch.io_changevalidators.yaml)
