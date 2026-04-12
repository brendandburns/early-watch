# Rule Type: NameReferenceCheck

The `NameReferenceCheck` rule type **denies the request when the subject resource is referenced by name in other cluster resources**.  It scans a list of resource types for field paths that may contain the subject's name and blocks the operation if any such reference is found.

---

## When to Use

Use `NameReferenceCheck` when you want to prevent deletion of resources that are still consumed by name from other workloads.  Common examples:

- Prevent a `ConfigMap` from being deleted while Deployments, DaemonSets, or CronJobs reference it via volumes or env injection.
- Prevent a `Secret` from being deleted while workloads or Ingress TLS entries reference it.
- Prevent a `Service` from being deleted while an `Ingress` references it as a backend.

---

## Fields

### NameReferenceCheck

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `resources` | []NameReferenceResource | Yes | List of resource types to scan for references. |
| `sameNamespace` | bool | No | Restrict the lookup to the same namespace as the subject. Defaults to `true`. |

### NameReferenceResource

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiGroup` | string | No | API group of the resource type to scan. Use `""` for core resources and `"apps"` for Deployments/DaemonSets. |
| `resource` | string | Yes | Plural name of the resource type to scan, e.g. `deployments`, `daemonsets`. |
| `version` | string | No | API version of the resource type to scan. Defaults to `v1` when omitted. |
| `nameFields` | []string | Yes | Dot-separated JSON field paths at which the subject's name may appear. Array elements are traversed automatically. |

### Array Traversal

Array elements along any `nameField` path are traversed automatically — **no wildcard syntax is required**.  For example, `spec.template.spec.volumes.configMap.name` correctly traverses the `volumes` array and finds any ConfigMap volume reference.

---

## Example — Protect a ConfigMap from Deletion

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-configmap-from-deletion
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: configmaps
  operations:
    - DELETE
  rules:
    - name: configmap-not-referenced-by-workloads
      type: NameReferenceCheck
      nameReferenceCheck:
        sameNamespace: true
        resources:
          - apiGroup: apps
            resource: deployments
            version: v1
            nameFields:
              - spec.template.spec.volumes.configMap.name
              - spec.template.spec.containers.envFrom.configMapRef.name
              - spec.template.spec.initContainers.envFrom.configMapRef.name
              - spec.template.spec.containers.env.valueFrom.configMapKeyRef.name
              - spec.template.spec.initContainers.env.valueFrom.configMapKeyRef.name
          - apiGroup: apps
            resource: daemonsets
            version: v1
            nameFields:
              - spec.template.spec.volumes.configMap.name
              - spec.template.spec.containers.envFrom.configMapRef.name
              - spec.template.spec.containers.env.valueFrom.configMapKeyRef.name
      message: >
        ConfigMap "{{name}}" cannot be deleted because it is still referenced
        by one or more Deployments or DaemonSets. Remove all references before
        deleting it.
```

---

## Sample Files

- [`config/samples/protect_configmap_from_deletion.yaml`](../../config/samples/protect_configmap_from_deletion.yaml)
- [`config/samples/protect_secret_from_deletion.yaml`](../../config/samples/protect_secret_from_deletion.yaml)
- [`config/samples/protect_service_referenced_by_ingress.yaml`](../../config/samples/protect_service_referenced_by_ingress.yaml)
