# Rule Type: CheckLock

The `CheckLock` rule type **denies a DELETE request when the subject resource carries the `earlywatch.io/lock` annotation with a non-empty value**.  This provides a simple, opt-in lock that operators can set on any resource without deploying a new `ChangeValidator`.

Optionally, by setting `lockOnMutate: true` in the rule's `checkLock` configuration, the lock can also block **UPDATE (mutation)** requests, preventing any changes to a locked resource.

---

## When to Use

Use `CheckLock` when you want to give operators a lightweight way to temporarily protect any resource from deletion (and optionally mutation) without writing a full rule set.  Any team member can lock a resource with a single `kubectl annotate` command and unlock it just as easily.

---

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `checkLock.lockOnMutate` | `boolean` | No | When `true`, extends the lock to UPDATE operations as well as DELETE. Defaults to `false` (delete-only). |

When `checkLock` is omitted entirely, only DELETE requests are blocked (preserving backward-compatible behavior).

---

## Examples

### Delete-only lock (default)

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-deployments-with-lock
  namespace: default
spec:
  subject:
    apiGroup: apps
    resource: deployments
  operations:
    - DELETE
  rules:
    - name: resource-must-not-be-locked
      type: CheckLock
      message: >
        This resource is locked. Remove the earlywatch.io/lock annotation
        before deleting it.
```

### Lock on both deletes and mutations

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-deployments-from-changes
  namespace: default
spec:
  subject:
    apiGroup: apps
    resource: deployments
  operations:
    - DELETE
    - UPDATE
  rules:
    - name: resource-must-not-be-locked
      type: CheckLock
      checkLock:
        lockOnMutate: true
      message: >
        This resource is locked. Remove the earlywatch.io/lock annotation
        before making any changes to it.
```

---

## Locking and Unlocking Resources

**Lock** a resource (any non-empty annotation value is treated as a lock):

```bash
kubectl annotate deployment my-app earlywatch.io/lock="protected by team-ops"
```

**Unlock** a resource:

```bash
kubectl annotate deployment my-app earlywatch.io/lock-
```

The annotation value is purely descriptive — it is not interpreted by the webhook.  Use it to record who placed the lock and why.
