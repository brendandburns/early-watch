# Getting Started

This guide walks you through installing EarlyWatch on a Kubernetes cluster and applying your first `ChangeValidator`.

---

## Prerequisites

- Go 1.21+
- A Kubernetes cluster (v1.26+)
- `kubectl` configured to talk to the cluster
- [cert-manager](https://cert-manager.io/) installed on the cluster (recommended for TLS certificate management)

---

## Install with watchctl

The fastest way to install EarlyWatch is with the `watchctl` CLI.

### 1. Build watchctl

```bash
go build -o watchctl ./cmd/watchctl/...
```

### 2. Install EarlyWatch onto your cluster

```bash
./watchctl install
```

This applies the CRD, RBAC, webhook Deployment, and `ValidatingWebhookConfiguration` in the correct order using Server-Side Apply.  Running `install` multiple times is safe (idempotent).

Use `--image` to specify a custom container image:

```bash
./watchctl install --image ghcr.io/my-org/early-watch:v1.2.3
```

See [cli/install.md](cli/install.md) for the full flag reference.

---

## Apply your first ChangeValidator

Once EarlyWatch is running, create a `ChangeValidator` to protect a resource.  The example below prevents any `Service` in the `default` namespace from being deleted while matching `Pods` are still running.

```bash
kubectl apply -f config/samples/protect_service.yaml
```

Test it by trying to delete a Service while its Pods are running:

```bash
kubectl delete service my-service
# Error from server: admission webhook "validate.earlywatch.io" denied the request:
# Service "my-service" cannot be deleted because Pods that match its label selector are still running.
```

---

## Next Steps

| Topic | Link |
|-------|------|
| How EarlyWatch works end-to-end | [architecture.md](architecture.md) |
| ChangeValidator CRD reference | [custom-resources/change-validator.md](custom-resources/change-validator.md) |
| All rule types | [rule-types/](rule-types/) |
| Manual cluster deployment (without watchctl) | [deployment/cluster-setup.md](deployment/cluster-setup.md) |
| CLI reference | [cli/watchctl.md](cli/watchctl.md) |
| Contributing and development | [contributing/development.md](contributing/development.md) |
