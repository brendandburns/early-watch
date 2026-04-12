# Rule Type: CheckLock

The `CheckLock` rule type **denies a DELETE request when the subject resource carries the `earlywatch.io/lock` annotation with a non-empty value**.  This provides a simple, opt-in lock that operators can set on any resource without deploying a new `ChangeValidator`.

---

## When to Use

Use `CheckLock` when you want to give operators a lightweight way to temporarily protect any resource from deletion without writing a full rule set.  Any team member can lock a resource with a single `kubectl annotate` command and unlock it just as easily.

---

## Fields

The `CheckLock` rule type has no extra configuration.  Simply set `type: CheckLock` and provide a `message`.

---

## Example

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
