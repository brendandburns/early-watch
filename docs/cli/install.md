# watchctl install

The `install` subcommand applies all EarlyWatch infrastructure manifests onto the cluster in the correct order using Server-Side Apply.  Running it multiple times is safe (idempotent).

The following resources are created:

1. `ChangeValidator`, `ManualTouchMonitor`, and `ManualTouchEvent` CRDs
2. `early-watch-system` namespace, `ClusterRole`, `ClusterRoleBinding`, and `ServiceAccount`
3. Webhook `Deployment` and `Service`
4. `ValidatingWebhookConfiguration`

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
```

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `""` | Path to the kubeconfig file. Defaults to in-cluster config when empty. |
| `--image` | `early-watch:latest` | Container image for the webhook Deployment. |
| `--namespace` | `early-watch-system` | Kubernetes namespace to install EarlyWatch into. |

---

## watchctl uninstall

The `uninstall` subcommand removes every resource that `watchctl install` created.  Resources are removed in reverse installation order so the webhook stops intercepting requests before lower-level objects are deleted.  Resources that are already absent are silently skipped.

```bash
watchctl uninstall

watchctl uninstall --kubeconfig ~/.kube/config --namespace my-earlywatch-ns
```

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `""` | Path to the kubeconfig file. |
| `--namespace` | `early-watch-system` | Namespace that EarlyWatch was installed into. |
