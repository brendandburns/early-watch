# Rule Type: ExpressionCheck

The `ExpressionCheck` rule type **evaluates a simple field expression against the admission request and denies the request when the expression returns `true`**.

> **Note:** The current implementation supports only a minimal `field == 'value'` syntax.  The evaluated fields are `operation`, `namespace`, and `name`.  Full [CEL](https://cel.dev/) support is planned for a future release.

---

## When to Use

Use `ExpressionCheck` when you need to block requests based on a specific property of the admission request — for example, restricting which operations are allowed on a resource, or limiting changes to a specific namespace.

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `expression` | string | Yes | An expression of the form `field == 'value'`. The request is **denied** when the expression evaluates to `true`. |

---

## Supported Fields

| Field | Description |
|-------|-------------|
| `operation` | Admission operation: `"CREATE"`, `"UPDATE"`, `"DELETE"`, or `"CONNECT"`. |
| `namespace` | Namespace of the resource being changed. |
| `name` | Name of the resource being changed. |

---

## Example — Deny DELETE for a Specific Namespace

```yaml
rules:
  - name: block-delete-in-production
    type: ExpressionCheck
    expressionCheck:
      expression: "namespace == 'production'"
    message: >
      Direct deletion is not permitted in the production namespace.
```

---

## Example — Deny a Specific Operation

```yaml
rules:
  - name: block-updates
    type: ExpressionCheck
    expressionCheck:
      expression: "operation == 'UPDATE'"
    message: >
      Updates to this resource are not allowed.
```

