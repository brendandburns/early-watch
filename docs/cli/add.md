# watchctl add

The `add` subcommand applies one or more Kubernetes manifests from a YAML file or directory to the cluster using Server-Side Apply.  Because it uses Server-Side Apply, running it multiple times is safe (idempotent).

---

## Usage

```bash
# Apply all manifests from a single file
watchctl add config/samples/protect_service.yaml

# Apply all manifests in a directory
watchctl add config/samples/

# Apply with an explicit kubeconfig
watchctl add config/samples/protect_service.yaml --kubeconfig ~/.kube/config
```

---

## Arguments

| Argument | Description |
|----------|-------------|
| `<file-or-directory>` | Path to a single YAML file or a directory containing YAML files. Required. |

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `""` | Path to the kubeconfig file. Defaults to in-cluster config when empty. |

---

## Notes

- When a directory is provided, all `.yaml` and `.yml` files in that directory are applied.
- Resources with an empty `metadata.namespace` are defaulted to the `default` namespace before being applied.
- Every applied resource is stamped with the label `earlywatch.io/created-by=watchctl`.
