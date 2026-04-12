# EarlyWatch Sample ChangeValidators

This directory contains sample `ChangeValidator` manifests that demonstrate common safety patterns for protecting Kubernetes resources from unsafe operations.

## Adding a Sample

Use `watchctl add` to apply a sample to your cluster:

```bash
# Apply a single sample
watchctl add config/samples/protect_service.yaml

# Apply all samples at once
watchctl add config/samples/
```

## Available Samples

### `protect_service.yaml`

Prevents a **Service** from being deleted while Pods that match its label selector are still running.

```bash
watchctl add config/samples/protect_service.yaml
```

### `protect_service_referenced_by_ingress.yaml`

Prevents a **Service** from being deleted while it is referenced as a backend by any Ingress resource in the same namespace.

```bash
watchctl add config/samples/protect_service_referenced_by_ingress.yaml
```

### `protect_configmap_from_deletion.yaml`

Prevents a **ConfigMap** from being deleted while it is still referenced by Deployments, DaemonSets, or CronJobs in the same namespace (as a volume mount, full environment variable source, or individual key reference).

```bash
watchctl add config/samples/protect_configmap_from_deletion.yaml
```

### `protect_secret_from_deletion.yaml`

Prevents a **Secret** from being deleted while it is referenced by Deployments, DaemonSets, or CronJobs (as a volume, environment variable source, or image pull secret), or used as a TLS certificate by an Ingress resource in the same namespace.  This file contains two `ChangeValidator` objects and both are applied together.

```bash
watchctl add config/samples/protect_secret_from_deletion.yaml
```

### `protect_serviceaccount_from_deletion.yaml`

Prevents a **ServiceAccount** from being deleted while it is still in use by a running Pod or referenced in the pod-template spec of a Deployment, DaemonSet, or CronJob in the same namespace.

```bash
watchctl add config/samples/protect_serviceaccount_from_deletion.yaml
```

### `protect_namespace_from_deletion.yaml`

Two cluster-scoped examples of namespace protection:

1. Prevents **all** namespaces from being deleted while they still contain Pods.
2. Protects only the `production` and `staging` namespaces from non-empty deletion.

```bash
watchctl add config/samples/protect_namespace_from_deletion.yaml
```

### `protect_kube_system_from_deletion.yaml`

Prevents the `kube-system` namespace from being deleted unless the operator has first added an explicit confirmation annotation:

```bash
kubectl annotate namespace kube-system earlywatch.io/confirm-delete=true
```

Apply the guard:

```bash
watchctl add config/samples/protect_kube_system_from_deletion.yaml
```
