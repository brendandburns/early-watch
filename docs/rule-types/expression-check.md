# Rule Type: ExpressionCheck

The `ExpressionCheck` rule type **evaluates a [Common Expression Language (CEL)](https://cel.dev/) expression against the admission request and denies the request when the expression returns `true`**.

---

## When to Use

Use `ExpressionCheck` when you need fine-grained control over which requests are blocked based on properties of the admission request itself — the operation type, the user, the object fields, or any combination thereof.

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `expression` | string | Yes | A CEL expression that receives the admission request as `request` and must return a boolean. The request is **denied** when the expression returns `true`. |

---

## CEL Variables

The expression receives the admission request object as the variable `request`.  Useful fields:

| Variable | Type | Description |
|----------|------|-------------|
| `request.operation` | string | Admission operation: `"CREATE"`, `"UPDATE"`, `"DELETE"`, or `"CONNECT"`. |
| `request.name` | string | Name of the resource being changed. |
| `request.namespace` | string | Namespace of the resource (empty for cluster-scoped resources). |
| `request.userInfo.username` | string | Kubernetes username of the requester. |
| `request.userInfo.groups` | list(string) | Groups the requester belongs to. |
| `request.object` | map | The new object (null for DELETE). |
| `request.oldObject` | map | The existing object (null for CREATE). |

---

## Example — Block DELETE Unless User is in a Specific Group

```yaml
rules:
  - name: only-admins-can-delete
    type: ExpressionCheck
    expressionCheck:
      expression: >
        request.operation == 'DELETE' &&
        !('system:cluster-admins' in request.userInfo.groups)
    message: >
      Only members of the cluster-admins group may delete this resource.
```

---

## Example — Require a Specific Label Before Update

```yaml
rules:
  - name: update-requires-approved-label
    type: ExpressionCheck
    expressionCheck:
      expression: >
        request.operation == 'UPDATE' &&
        !('earlywatch.io/approved' in request.object.metadata.annotations)
    message: >
      This resource cannot be updated without the earlywatch.io/approved annotation.
```
