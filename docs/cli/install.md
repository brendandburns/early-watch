# watchctl install

The `install` subcommand applies all EarlyWatch infrastructure manifests onto the cluster in the correct order using Server-Side Apply.  Running it multiple times is safe (idempotent).

The following resources are created by default:

1. `ChangeValidator` CRD
2. `early-watch-system` namespace, `ClusterRole`, `ClusterRoleBinding`, and `ServiceAccount`
3. Webhook `Deployment` and `Service`
4. `ValidatingWebhookConfiguration`

Pass `--manual-touch` to additionally install the manual touch monitoring stack:

5. `ManualTouchMonitor` and `ManualTouchEvent` CRDs
6. Audit-monitor `ClusterRole`, `ClusterRoleBinding`, and `ServiceAccount`
7. Audit-monitor `Deployment` and `Service`

---

## Usage

```bash
# Install using the current kubeconfig context
watchctl install

# Install with an explicit kubeconfig
watchctl install --kubeconfig ~/.kube/config

# Install with a custom webhook image
watchctl install --image ghcr.io/my-org/early-watch:v1.2.3

# Install into a custom namespace
watchctl install --namespace my-earlywatch-ns

# Also install the audit-monitor for manual touch monitoring
watchctl install --manual-touch

# Install with a custom audit-monitor image
watchctl install --manual-touch --audit-monitor-image ghcr.io/my-org/early-watch-audit-monitor:v1.2.3
```

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `""` | Path to the kubeconfig file. Defaults to in-cluster config when empty. |
| `--image` | `early-watch:v0.0.1` | Container image for the webhook Deployment. |
| `--namespace` | `early-watch-system` | Kubernetes namespace to install EarlyWatch into. |
| `--manual-touch` | `false` | Also install the audit-monitor components for manual touch monitoring. |
| `--audit-monitor-image` | `early-watch-audit-monitor:v0.0.1` | Container image for the audit-monitor Deployment. Only used with `--manual-touch`. |

---

## watchctl uninstall

The `uninstall` subcommand removes every resource that `watchctl install` created.  Resources are removed in reverse installation order so the webhook stops intercepting requests before lower-level objects are deleted.  Resources that are already absent are silently skipped.

Pass `--manual-touch` to also remove the audit-monitor resources that were installed with `watchctl install --manual-touch`.

```bash
watchctl uninstall

watchctl uninstall --kubeconfig ~/.kube/config --namespace my-earlywatch-ns

# Also remove the audit-monitor components
watchctl uninstall --manual-touch
```

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `""` | Path to the kubeconfig file. |
| `--namespace` | `early-watch-system` | Namespace that EarlyWatch was installed into. |
| `--manual-touch` | `false` | Also remove the audit-monitor components for manual touch monitoring. |
