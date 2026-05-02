# watchctl list-touches

The `list-touches` subcommand retrieves `ManualTouchEvent` resources from the cluster and displays them in a human-readable table.

`ManualTouchEvent` resources are created by the EarlyWatch audit monitor whenever a manual change (e.g. a direct `kubectl` edit) is detected on a resource that is watched by a `ManualTouchMonitor`.  Use this subcommand to audit which resources have been manually modified and when.

---

## Usage

```bash
# List all manual touches across all namespaces
watchctl list-touches

# List manual touches in a specific namespace
watchctl list-touches --namespace default

# List with an explicit kubeconfig
watchctl list-touches --kubeconfig ~/.kube/config --namespace default
```

---

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespace` | `-n` | `""` | Kubernetes namespace to list touches from. Lists across all namespaces when empty. |
| `--kubeconfig` | | `""` | Path to the kubeconfig file. Defaults to the default kubeconfig loading rules when empty. |

---

## Prerequisites

`list-touches` requires the manual touch monitoring stack to be installed.  If you have not already done so, install it with:

```bash
watchctl install --manual-touch
```

See [watchctl install](install.md) and [ManualTouchMonitor](../custom-resources/manual-touch-monitor.md) for details.
