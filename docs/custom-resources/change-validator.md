# ChangeValidator CRD Reference

`ChangeValidator` is the core custom resource in EarlyWatch.  Each `ChangeValidator` declares:

- **which** Kubernetes resource type to protect (`spec.subject`),
- **which** admission operations to intercept (`spec.operations`), and
- **which** safety rules to enforce (`spec.rules`).

API group: `earlywatch.io/v1alpha1`  
Short name: `cv`  
Scope: Namespaced

> **Need cluster-wide coverage?**  Use [`ClusterChangeValidator`](cluster-change-validator.md) instead.  It shares the same `spec` schema but applies across all namespaces without requiring a copy per namespace.

---

## Minimal Example

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-service-from-deletion
  namespace: default
spec:
  subject:
    apiGroup: ""        # "" = core API group
    resource: services
  operations:
    - DELETE
  rules:
    - name: no-matching-pods
      type: ExistingResources
      existingResources:
        apiGroup: ""
        resource: pods
        labelSelectorFromField: spec.selector
        sameNamespace: true
      message: >
        Service "{{name}}" cannot be deleted because Pods that match its label
        selector are still running. Scale down or delete the Pods first.
```

---

## spec.subject

Identifies the Kubernetes resource type this validator protects.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiGroup` | string | No | API group; use `""` for core resources (Pods, Services…). |
| `resource` | string | Yes | Plural resource name, e.g. `services`, `deployments`. |
| `names` | []string | No | Restrict to only resources whose name is in this list. |
| `namespaceSelector` | LabelSelector | No | Restrict to namespaces whose labels match this selector. |

### subject.names — Protecting Specific Resources

`names` restricts the validator to a named subset of resources.  For example, to protect only the `production` and `staging` namespaces:

```yaml
spec:
  subject:
    apiGroup: ""
    resource: namespaces
    names:
      - production
      - staging
```

### subject.namespaceSelector — Namespace-Scoped Guards

`namespaceSelector` restricts the validator to namespaces whose labels match a Kubernetes `LabelSelector`.  This lets you protect resources only in production namespaces without enumerating them by name:

```yaml
spec:
  subject:
    apiGroup: apps
    resource: deployments
    namespaceSelector:
      matchLabels:
        env: production
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

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Human-readable identifier for this rule. |
| `type` | RuleType | Yes | Kind of check to perform (see below). |
| `message` | string | Yes | Denial message returned to the user when this rule is violated. Supports `{{name}}` and `{{namespace}}` template variables. |
| `existingResources` | ExistingResourcesCheck | Conditional | Required when `type` is `ExistingResources`. |
| `expressionCheck` | ExpressionCheck | Conditional | Required when `type` is `ExpressionCheck`. |
| `nameReferenceCheck` | NameReferenceCheck | Conditional | Required when `type` is `NameReferenceCheck`. |
| `approvalCheck` | ApprovalCheck | Conditional | Required when `type` is `ApprovalCheck`. |
| `annotationCheck` | AnnotationCheck | Conditional | Required when `type` is `AnnotationCheck`. |
| `manualTouchCheck` | ManualTouchCheck | Conditional | Required when `type` is `ManualTouchCheck`. |

### Rule Types

| Type | Description | Reference |
|------|-------------|-----------|
| `ExistingResources` | Denies when dependent resources still exist in the cluster. | [existing-resources.md](../rule-types/existing-resources.md) |
| `ExpressionCheck` | Evaluates a `field == 'value'` expression against the admission request (supported fields: `operation`, `namespace`, `name`). | [expression-check.md](../rule-types/expression-check.md) |
| `NameReferenceCheck` | Denies when the subject is referenced by name in other resources. | [name-reference-check.md](../rule-types/name-reference-check.md) |
| `CheckLock` | Denies when the subject carries the `earlywatch.io/lock` annotation. | [check-lock.md](../rule-types/check-lock.md) |
| `AnnotationCheck` | Denies when the subject lacks a required annotation. | [annotation-check.md](../rule-types/annotation-check.md) |
| `ApprovalCheck` | Denies unless the subject carries a valid RSA-PSS approval signature. | [approval-check.md](../rule-types/approval-check.md) |
| `ManualTouchCheck` | Denies when a recent manual touch event was recorded for the resource. | [manual-touch-check.md](../rule-types/manual-touch-check.md) |

---

## Full CRD Schema

The generated CRD YAML (including all validation markers) is at:  
[`config/crd/bases/earlywatch.io_changevalidators.yaml`](../../config/crd/bases/earlywatch.io_changevalidators.yaml)
