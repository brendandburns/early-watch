# Rule Type: DataKeySafetyCheck

The `DataKeySafetyCheck` rule type **denies an UPDATE request that removes a data key from a ConfigMap or Secret when that specific key is still referenced by another cluster resource**.  It prevents broken pod environments caused by removing a key that a running workload still mounts or injects as an environment variable.

---

## When to Use

Use `DataKeySafetyCheck` when you want to protect ConfigMap or Secret keys that are consumed by workloads:

- Prevent removing a key from a ConfigMap while a Deployment still references it via `configMapKeyRef`.
- Prevent removing a key from a Secret while a Pod still references it via `secretKeyRef`.
- Guard against key removal during GitOps or automated config rotation workflows.

---

## Fields

### `dataKeySafetyCheck`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `resources` | []DataKeyReferenceResource | Yes | List of resource types to scan for key references. |
| `sameNamespace` | bool | No | Restricts the lookup to the same namespace as the subject. Defaults to `true`. |

### `DataKeyReferenceResource`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiGroup` | string | No | API group of the resource to scan. Use `""` for core resources, `"apps"` for Deployments. |
| `resource` | string | Yes | Plural name of the resource type to scan, e.g. `pods`, `deployments`. |
| `version` | string | No | API version of the resource to scan. Defaults to `"v1"`. |
| `keyReferenceFields` | []KeyReferenceField | Yes | Locations in the resource where a name and key appear as sibling fields. |

### `KeyReferenceField`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `refPath` | string | Yes | Dot-separated JSON path to the object that contains both `name` and `key` sub-fields. Array elements along the path are traversed automatically. |
| `nameSubField` | string | No | Field name within the `refPath` object that holds the ConfigMap or Secret name. Defaults to `"name"`. |
| `keySubField` | string | No | Field name within the `refPath` object that holds the data key. Defaults to `"key"`. |

---

## Example — Protect ConfigMap Keys Referenced by Deployments

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-configmap-keys
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: configmaps
    names:
      - app-config
  operations:
    - UPDATE
  rules:
    - name: no-referenced-key-removal
      type: DataKeySafetyCheck
      dataKeySafetyCheck:
        resources:
          - apiGroup: apps
            resource: deployments
            version: v1
            keyReferenceFields:
              - refPath: spec.template.spec.containers.env.valueFrom.configMapKeyRef
          - apiGroup: ""
            resource: pods
            keyReferenceFields:
              - refPath: spec.containers.env.valueFrom.configMapKeyRef
              - refPath: spec.initContainers.env.valueFrom.configMapKeyRef
        sameNamespace: true
      message: >
        ConfigMap "{{name}}" cannot have a key removed because it is still
        referenced by a running workload.  Update or redeploy the workload
        before removing the key.
```

---

## Example — Protect Secret Keys Referenced by Pods

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-secret-keys
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: secrets
    names:
      - db-credentials
  operations:
    - UPDATE
  rules:
    - name: no-referenced-secret-key-removal
      type: DataKeySafetyCheck
      dataKeySafetyCheck:
        resources:
          - apiGroup: ""
            resource: pods
            keyReferenceFields:
              - refPath: spec.containers.env.valueFrom.secretKeyRef
              - refPath: spec.initContainers.env.valueFrom.secretKeyRef
        sameNamespace: true
      message: >
        Secret "{{name}}" cannot have a key removed while it is referenced
        by a Pod.  Remove the reference first.
```

---

## Example YAML

[`docs/examples/data-key-safety-check.yaml`](../examples/data-key-safety-check.yaml)
