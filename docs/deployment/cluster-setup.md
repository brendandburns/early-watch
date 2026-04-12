# Manual Cluster Setup

This guide walks through deploying EarlyWatch onto a cluster without using `watchctl install`.  Use this approach when you need full control over each step, or when integrating EarlyWatch into an existing GitOps pipeline.

For the recommended one-command install, see [getting-started.md](../getting-started.md).

---

## Prerequisites

- `kubectl` configured to talk to your target cluster
- [cert-manager](https://cert-manager.io/) installed (for TLS certificate management — see [tls-and-cert-manager.md](tls-and-cert-manager.md))
- The EarlyWatch container image available in a registry your cluster can pull from

---

## Step 1 — Apply the CRDs

```bash
kubectl apply -f config/crd/bases/earlywatch.io_changevalidators.yaml
```

This installs the `ChangeValidator`, `ManualTouchMonitor`, and `ManualTouchEvent` custom resource definitions.

---

## Step 2 — Create the Namespace and RBAC

```bash
kubectl create namespace early-watch-system
kubectl apply -f config/rbac/
```

This creates:
- `ClusterRole` — grants the webhook read access to all resources it may need to evaluate rules.
- `ClusterRoleBinding` — binds the ClusterRole to the `early-watch` ServiceAccount.
- `ServiceAccount` — identity used by the webhook Pod.

---

## Step 3 — Deploy the Webhook Server

Before applying the webhook manifests, ensure TLS certificates are available.  See [tls-and-cert-manager.md](tls-and-cert-manager.md) for cert-manager integration details.

```bash
kubectl apply -f config/webhook/
```

This creates:
- `Deployment` — runs the EarlyWatch webhook server.
- `Service` — exposes the webhook server within the cluster.
- `ValidatingWebhookConfiguration` — registers the webhook with the API server.

Verify the Pod is running:

```bash
kubectl get pods -n early-watch-system
```

---

## Step 4 — Apply Sample ChangeValidators

```bash
# Protect a Service from deletion while matching Pods are running:
kubectl apply -f config/samples/protect_service.yaml

# Protect a Service from deletion while it is referenced by an Ingress:
kubectl apply -f config/samples/protect_service_referenced_by_ingress.yaml

# Protect a ConfigMap from deletion while workloads reference it:
kubectl apply -f config/samples/protect_configmap_from_deletion.yaml

# Protect Secrets from deletion while workloads or Ingress TLS reference them:
kubectl apply -f config/samples/protect_secret_from_deletion.yaml

# Protect Namespaces from deletion while they contain Pods:
kubectl apply -f config/samples/protect_namespace_from_deletion.yaml
```

---

## Step 5 — Verify

Try deleting a protected resource to confirm the webhook is active:

```bash
# This should be denied if matching Pods are running:
kubectl delete service my-service
```

---

## Uninstalling

Remove resources in reverse order to avoid leaving the webhook intercepting requests after the backend is gone:

```bash
kubectl delete -f config/webhook/
kubectl delete -f config/rbac/
kubectl delete namespace early-watch-system
kubectl delete -f config/crd/bases/earlywatch.io_changevalidators.yaml
```

Or use `watchctl uninstall` — see [cli/install.md](../cli/install.md).
