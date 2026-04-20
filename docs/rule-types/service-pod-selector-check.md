# Rule Type: ServicePodSelectorCheck

The `ServicePodSelectorCheck` rule type **denies a Service UPDATE when the old selector matched at least one Pod but the new selector would match none**.  This prevents accidental traffic blackholes caused by a typo or copy-paste error in a Service's label selector.

Headless Services (`spec.clusterIP: "None"`) that carry no selector are exempt.

---

## When to Use

Use `ServicePodSelectorCheck` when you want to ensure that a Service always has at least one healthy backend after an UPDATE:

- Catch selector key/value typos before they reach production.
- Prevent rolling selector changes that would silently drop all traffic.
- Enforce a "at least one Pod must match" invariant on critical Services.

---

## Fields

`ServicePodSelectorCheck` has no configuration fields.  Add the key with an empty object (`{}`) in the rule:

```yaml
servicePodSelectorCheck: {}
```

---

## Example — Protect a Service's Selector

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-service-selector
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: services
    names:
      - my-service
  operations:
    - UPDATE
  rules:
    - name: selector-must-match-pods
      type: ServicePodSelectorCheck
      servicePodSelectorCheck: {}
      message: >
        Service "{{name}}" update is denied: the new selector would match zero
        Pods.  Ensure at least one Pod with the new labels exists before
        updating the Service.
```

To proceed with a selector change:

1. Deploy Pods that match the *new* selector labels.
2. Verify at least one Pod is Running with those labels.
3. Apply the Service UPDATE — the check will now pass.
4. Clean up Pods matching the old selector.

---

## Example YAML

[`docs/examples/service-pod-selector-check.yaml`](../examples/service-pod-selector-check.yaml)
