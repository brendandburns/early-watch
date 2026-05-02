# watchctl

`watchctl` is the command-line companion to the EarlyWatch webhook.  It provides subcommands for installing EarlyWatch onto a cluster, applying manifests, and approving resources before a protected operation.

---

## Subcommands

| Subcommand | Description |
|------------|-------------|
| [`watchctl install`](install.md) | Install all EarlyWatch infrastructure (CRD, RBAC, webhook) onto the current cluster. Pass `--manual-touch` to also install the audit-monitor for manual touch monitoring. |
| [`watchctl uninstall`](install.md) | Remove all EarlyWatch infrastructure from the current cluster. Pass `--manual-touch` to also remove the audit-monitor components. |
| [`watchctl add`](add.md) | Apply one or more manifests from a YAML file or directory to the cluster using Server-Side Apply. |
| [`watchctl approve delete`](approve.md) | Sign a Kubernetes resource's canonical path with an RSA private key and write the delete-approval annotation on the resource. |
| [`watchctl approve change`](approve.md) | Sign the merge patch for a resource modification and output the annotated resource JSON, ready to pipe into `kubectl apply`. |
| [`watchctl list-touches`](list-touches.md) | List all `ManualTouchEvent` resources, showing manual (e.g. `kubectl`) changes detected by the audit monitor. |

---

## Global Behaviour

- `watchctl` uses the kubeconfig file specified by `--kubeconfig`, or falls back to the in-cluster service account config when running inside a Pod.
- All subcommands exit with a non-zero status code on error and print a human-readable message to stderr.
