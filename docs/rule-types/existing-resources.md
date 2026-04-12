# Rule Type: ExistingResources

The `ExistingResources` rule type **denies the request when dependent resources still exist in the cluster**.  It queries the cluster for resources related to the subject (using a label selector or a static selector) and blocks the operation if any results are returned.

---

## When to Use

Use `ExistingResources` when you want to prevent a resource from being changed (usually deleted) while other resources depend on it at the infrastructure level.  Common examples:

- Prevent a `Service` from being deleted while matching `Pods` are running.
- Prevent a `Namespace` from being deleted while `Pods` exist inside it.
- Prevent a parent resource from being deleted while child resources still reference it via labels.

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiGroup` | string | No | API group of the dependent resource. Use `""` for core resources such as Pods. |
| `resource` | string | Yes | Plural name of the dependent resource type, e.g. `pods`, `replicasets`. |
| `labelSelectorFromField` | string | No | Dot-separated JSON path into the subject's spec that contains a `map[string]string` to use as a label selector. Mutually exclusive with `labelSelector`. |
| `labelSelector` | LabelSelector | No | Static label selector. Mutually exclusive with `labelSelectorFromField`. |
| `sameNamespace` | bool | No | When `true`, restricts the lookup to the same namespace as the subject. Defaults to `true`. |

---

## Example — Protect a Service from Deletion

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-service-from-deletion
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: services
  operations:
    - DELETE
  rules:
    - name: no-matching-pods
      type: ExistingResources
      existingResources:
        apiGroup: ""
        resource: pods
        # Read the label selector from the Service's spec.selector field.
        labelSelectorFromField: spec.selector
        sameNamespace: true
      message: >
        Service "{{name}}" in namespace "{{namespace}}" cannot be deleted
        because Pods that match its label selector are still running.
        Scale down or delete the matching Pods before removing the Service.
```

---

## Example — Protect a Namespace from Deletion (Static Selector)

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-namespace-from-deletion
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: namespaces
  operations:
    - DELETE
  rules:
    - name: namespace-must-be-empty
      type: ExistingResources
      existingResources:
        apiGroup: ""
        resource: pods
        sameNamespace: false
        labelSelector:
          matchExpressions: []   # match all pods; scoped by namespace via sameNamespace
      message: >
        Namespace "{{name}}" cannot be deleted because it still contains running Pods.
        Delete or evict all Pods before removing the namespace.
```

---

## Sample File

[`config/samples/protect_service.yaml`](../../config/samples/protect_service.yaml)
