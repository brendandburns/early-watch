# Validation Rule Types

EarlyWatch evaluates rules defined in a `ChangeValidator`'s `spec.rules` list.  All rules are evaluated in order; the **first violation denies the request** with the rule's `message`.

---

## Available Rule Types

| Type | Summary | Reference |
|------|---------|-----------|
| `ExistingResources` | Deny when dependent resources (e.g. Pods) still exist in the cluster. | [existing-resources.md](existing-resources.md) |
| `ExpressionCheck` | Evaluate a `field == 'value'` expression against the admission request (operation/namespace/name); deny when it matches. | [expression-check.md](expression-check.md) |
| `NameReferenceCheck` | Deny when the subject is referenced by name in other cluster resources (e.g. a ConfigMap mounted by a Deployment). | [name-reference-check.md](name-reference-check.md) |
| `CheckLock` | Deny when the subject carries the `earlywatch.io/lock` annotation with a non-empty value. | [check-lock.md](check-lock.md) |
| `AnnotationCheck` | Deny when the subject does not carry a required annotation (confirm-delete pattern). | [annotation-check.md](annotation-check.md) |
| `ApprovalCheck` | Deny unless the subject carries a valid RSA-PSS SHA-256 approval signature in an annotation. | [approval-check.md](approval-check.md) |
| `ManualTouchCheck` | Deny when a recent manual (kubectl) touch has been recorded for the resource within a configurable window. | [manual-touch-check.md](manual-touch-check.md) |
| `ServicePodSelectorCheck` | Deny a Service UPDATE when the old selector matched Pods but the new selector would match none. | - |

---

## Demo Matrix

Use the interactive demo driver to run any validator scenario:

```bash
bash scripts/demo.sh --demos=<key>
```

| Validator Type | Demo Key (`--demos`) | Demo Script |
|---|---|---|
| `ExistingResources` | `service` | `scripts/demo-service.sh` |
| `NameReferenceCheck` | `configmap` | `scripts/demo-configmap.sh` |
| `AnnotationCheck` | `annotation` | `scripts/demo-annotation-check.sh` |
| `ApprovalCheck` | `approval` | `scripts/demo-approval-check.sh` |
| `CheckLock` | `checklock` | `scripts/demo-check-lock.sh` |
| `ExpressionCheck` | `expression` | `scripts/demo-expression-check.sh` |
| `ManualTouchCheck` | `manualtouch` | `scripts/demo-manual-touch-check.sh` |
| `ServicePodSelectorCheck` | `servicepodselector` | `scripts/demo-service-pod-selector-check.sh` |

---

## Choosing a Rule Type

| Scenario | Recommended type |
|----------|-----------------|
| Prevent deletion of a resource while dependents still exist | `ExistingResources` |
| Prevent deletion of a resource that is still referenced by name from other resources | `NameReferenceCheck` |
| Allow only specific operations or restrict to a specific namespace/name | `ExpressionCheck` |
| Give operators a quick, reversible lock on any resource | `CheckLock` |
| Require an explicit annotation before a destructive operation proceeds | `AnnotationCheck` |
| Require cryptographically verifiable sign-off from a key holder | `ApprovalCheck` |
| Protect a recently manually-edited resource from being overwritten by automation | `ManualTouchCheck` |

---

## Rule Structure

Each entry in `spec.rules` has these common fields:

```yaml
rules:
  - name: <human-readable identifier>
    type: <RuleType>          # one of the types listed above
    message: >                # denial message shown to the user; supports {{name}} and {{namespace}}
      ...
    # type-specific configuration block (same name as the type, camelCase):
    existingResources: ...    # when type: ExistingResources
    expressionCheck: ...      # when type: ExpressionCheck
    nameReferenceCheck: ...   # when type: NameReferenceCheck
    # CheckLock has no extra configuration
    annotationCheck: ...      # when type: AnnotationCheck
    approvalCheck: ...        # when type: ApprovalCheck
    manualTouchCheck: ...     # when type: ManualTouchCheck
```

See the [ChangeValidator CRD reference](../custom-resources/change-validator.md) for the full field listing.
