# watchctl

`watchctl` is the command-line companion to the EarlyWatch webhook.  It provides subcommands for installing EarlyWatch onto a cluster and for approving resources before a protected operation.

---

## Installation

Build from source:

```bash
go build -o watchctl ./cmd/watchctl/...
```

---

## Subcommands

| Subcommand | Description |
|------------|-------------|
| [`watchctl install`](install.md) | Install all EarlyWatch infrastructure (CRD, RBAC, webhook) onto the current cluster. Pass `--manual-touch` to also install the audit-monitor for manual touch monitoring. |
| [`watchctl uninstall`](install.md) | Remove all EarlyWatch infrastructure from the current cluster. Pass `--manual-touch` to also remove the audit-monitor components. |
| [`watchctl approve`](approve.md) | Sign a Kubernetes resource's canonical path with an RSA private key and write the signature as an approval annotation. |

---

## Global Behaviour

- `watchctl` uses the kubeconfig file specified by `--kubeconfig`, or falls back to the in-cluster service account config when running inside a Pod.
- All subcommands exit with a non-zero status code on error and print a human-readable message to stderr.
