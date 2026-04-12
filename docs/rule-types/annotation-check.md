# Rule Type: AnnotationCheck

The `AnnotationCheck` rule type **denies the request when the subject resource does not carry a required annotation** (and optionally a specific value for that annotation).  Use this to implement a "confirm delete" pattern where a resource can only be deleted after an operator explicitly adds a designated annotation.

---

## When to Use

Use `AnnotationCheck` when you want a two-step deletion workflow:

1. An operator must first annotate the resource to signal intent.
2. Only then does the DELETE operation succeed.

This is useful for high-value resources where accidental deletion would be costly and you want an out-of-band approval step.

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `annotationKey` | string | Yes | The annotation key that must be present on the resource, e.g. `earlywatch.io/confirm-delete`. |
| `annotationValue` | string | No | When specified, the annotation must have exactly this value. When omitted, any value (including empty string) is accepted as long as the key is present. |

For DELETE requests the annotation is read from `oldObject` (the resource being deleted).

---

## Example — Confirm-Delete Pattern

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: require-delete-confirmation
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: namespaces
    names:
      - production
  operations:
    - DELETE
  rules:
    - name: must-have-delete-confirmation
      type: AnnotationCheck
      annotationCheck:
        annotationKey: earlywatch.io/confirm-delete
        annotationValue: "yes"
      message: >
        Namespace "production" cannot be deleted without explicit confirmation.
        Run: kubectl annotate namespace production earlywatch.io/confirm-delete=yes
```

To proceed with deletion:

```bash
kubectl annotate namespace production earlywatch.io/confirm-delete=yes
kubectl delete namespace production
```

---

## Difference from CheckLock

| Feature | AnnotationCheck | CheckLock |
|---------|----------------|-----------|
| Direction | Requires annotation to **allow** | Requires annotation absence to **allow** |
| Pattern | Opt-in confirm-delete | Opt-in lock |
| Annotation key | Configurable | Always `earlywatch.io/lock` |
| Value match | Optional exact match | Any non-empty value triggers lock |
