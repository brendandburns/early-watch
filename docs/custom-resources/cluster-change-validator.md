# ClusterChangeValidator CRD Reference

`ClusterChangeValidator` is the cluster-scoped counterpart to `ChangeValidator`.  It shares the same `spec` schema but applies its rules **across all namespaces** rather than being bound to a single namespace.  Use a `ClusterChangeValidator` when you want to enforce a policy cluster-wide rather than repeating a `ChangeValidator` in every namespace.

API group: `earlywatch.io/v1alpha1`  
Short name: `ccv`  
Scope: Cluster

---

## How It Differs From ChangeValidator

| | `ChangeValidator` | `ClusterChangeValidator` |
|-|-------------------|--------------------------|
| **Scope** | Namespaced — lives in and only guards resources in its own namespace | Cluster — guards resources in every namespace |
| **Short name** | `cv` | `ccv` |
| **Namespace restriction** | Not needed (already namespace-scoped) | Use `spec.subject.namespaceSelector` to target a subset of namespaces |
| **Typical use case** | Per-team or per-application protection | Organisation-wide safety policies |

Both kinds share the same `spec.subject`, `spec.operations`, and `spec.rules` fields.

---

## Minimal Example

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ClusterChangeValidator
metadata:
  name: protect-prod-services
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
```

---

## spec.subject

Identifies the Kubernetes resource type this validator protects and, optionally, which namespaces it applies to.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiGroup` | string | No | API group; use `""` for core resources (Pods, Services…). |
| `resource` | string | Yes | Plural resource name, e.g. `services`, `deployments`. |
| `names` | []string | No | Restrict to only resources whose name is in this list. |
| `namespaceSelector` | LabelSelector | No | Restrict to namespaces whose labels match this selector.  When omitted the validator applies to **all** namespaces. |

### Restricting to Specific Namespaces

Use `namespaceSelector` to target only namespaces that carry particular labels.  For example, to apply a policy only to production namespaces:

```yaml
spec:
  subject:
    apiGroup: apps
    resource: deployments
    namespaceSelector:
      matchLabels:
        env: prod
```

When a `ClusterChangeValidator` carries no `namespaceSelector` (or an empty one), it applies to all namespaces.

### Protecting Namespace Resources Themselves

`ClusterChangeValidator` is the natural home for namespace-deletion guards because the `ChangeValidator` (which is namespaced) cannot be deployed into a namespace that does not yet exist.  For example, to block deletion of any namespace while it still contains Pods:

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ClusterChangeValidator
metadata:
  name: protect-nonempty-namespaces
spec:
  subject:
    apiGroup: ""
    resource: namespaces
  operations:
    - DELETE
  rules:
    - name: namespace-has-pods
      type: ExistingResources
      existingResources:
        apiGroup: ""
        resource: pods
        sameNamespace: true
      message: >
        Namespace "{{name}}" still contains running Pods.
        Drain the namespace before deleting it.
```

---

## spec.operations

A list of admission operations that trigger rule evaluation.  Valid values: `CREATE`, `UPDATE`, `DELETE`, `CONNECT`.  At least one value is required.

```yaml
operations:
  - DELETE
  - UPDATE
```

---

## spec.rules

A list of `GuardRule` objects.  All rules are evaluated; the first violation denies the request.  At least one rule is required.

The rule schema is identical to `ChangeValidator`.  See the [rule types reference](../rule-types/README.md) and the [ChangeValidator CRD reference](change-validator.md#specrules) for the complete field listing and all available rule types.

---

## Full CRD Schema

The generated CRD YAML is at:  
[`config/crd/bases/earlywatch.io_clusterchangevalidators.yaml`](../../config/crd/bases/earlywatch.io_clusterchangevalidators.yaml)
