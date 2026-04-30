# EarlyWatch

EarlyWatch is a Kubernetes admission controller that implements **change safety** — it ensures that changes to Kubernetes resources are safe before they occur.

For example, EarlyWatch can prevent you from deleting a `Service` while there are still `Pods` running that match the Service's label selector.

---

## How It Works

EarlyWatch introduces a `ChangeValidator` custom resource.  Each `ChangeValidator` watches a specific Kubernetes resource type and defines a set of safety rules.  When an admission request matches a guard's subject and operations, the EarlyWatch webhook evaluates the rules against the current cluster state.  If any rule is violated the request is **denied** with a clear error message.

```
User/CI → kubectl delete service my-svc
              │
              ▼
    Kubernetes API Server
              │
              │ ValidatingWebhookConfiguration
              ▼
    EarlyWatch Webhook
              │
              │ lists ChangeValidator rules for "services" (same namespace)
              │ lists ClusterChangeValidator rules for "services" (all namespaces)
              │ queries cluster for matching Pods
              ▼
    DENY: "This Service cannot be deleted because Pods that
           match its label selector are still running."
```

---

## Quick Start

```bash
# Build the CLI
go build -o watchctl ./cmd/watchctl/...

# Install EarlyWatch onto your cluster
./watchctl install

# Apply a sample ChangeValidator
kubectl apply -f config/samples/protect_service.yaml
```

See [docs/getting-started.md](docs/getting-started.md) for a full walkthrough.

---

## Documentation

### Change Validator Summaries

| Validator Type | Summary | Sample YAML |
|-------|------|------|
| `ExistingResources` | Denies a request when dependent resources still exist (for example, matching Pods behind a Service). | [docs/examples/existing-resources.yaml](docs/examples/existing-resources.yaml) |
| `NameReferenceCheck` | Denies a request when the subject is still referenced by name in other resources. | [docs/examples/name-reference-check.yaml](docs/examples/name-reference-check.yaml) |
| `AnnotationCheck` | Denies a request unless a required annotation (optionally with a required value) is present. | [docs/examples/annotation-check.yaml](docs/examples/annotation-check.yaml) |
| `ApprovalCheck` | Denies a request unless the resource carries a valid RSA-PSS approval signature annotation. | [docs/examples/approval-check.yaml](docs/examples/approval-check.yaml) |
| `CheckLock` | Denies DELETE (and optionally UPDATE) when `earlywatch.io/lock` is set with a non-empty value. | [docs/examples/check-lock.yaml](docs/examples/check-lock.yaml) |
| `ExpressionCheck` | Denies a request when a configured expression against operation/namespace/name evaluates to true. | [docs/examples/expression-check.yaml](docs/examples/expression-check.yaml) |
| `ManualTouchCheck` | Denies a request when a recent manual touch event exists within the configured time window. | [docs/examples/manual-touch-check.yaml](docs/examples/manual-touch-check.yaml) |
| `ServicePodSelectorCheck` | Denies a Service UPDATE when the old selector matched Pods but the new selector would match none. | [docs/examples/service-pod-selector-check.yaml](docs/examples/service-pod-selector-check.yaml) |
| `ClusterChangeValidator` | Applies any rule type cluster-wide across all namespaces (optionally filtered by `namespaceSelector`). | [docs/examples/cluster-change-validator.yaml](docs/examples/cluster-change-validator.yaml) |

---

### Interactive Demo Matrix

Run a specific validator demo with:

```bash
bash scripts/demo.sh --demos=<key>
```

| Validator Type | Demo Key (`--demos`) | Demo Script |
|-------|------|------|
| `ExistingResources` | `service` | `scripts/demo-service.sh` |
| `NameReferenceCheck` | `configmap` | `scripts/demo-configmap.sh` |
| `AnnotationCheck` | `annotation` | `scripts/demo-annotation-check.sh` |
| `ApprovalCheck` | `approval` | `scripts/demo-approval-check.sh` |
| `CheckLock` | `checklock` | `scripts/demo-check-lock.sh` |
| `ExpressionCheck` | `expression` | `scripts/demo-expression-check.sh` |
| `ManualTouchCheck` | `manualtouch` | `scripts/demo-manual-touch-check.sh` |
| `ServicePodSelectorCheck` | `servicepodselector` | `scripts/demo-service-pod-selector-check.sh` |
| `ClusterChangeValidator` | `clustervalidator` | `scripts/demo-cluster-change-validator.sh` |

---

| Topic | Link |
|-------|------|
| Getting started | [docs/getting-started.md](docs/getting-started.md) |
| Architecture and source tree | [docs/architecture.md](docs/architecture.md) |
| ChangeValidator CRD reference | [docs/custom-resources/change-validator.md](docs/custom-resources/change-validator.md) |
| ClusterChangeValidator CRD reference | [docs/custom-resources/cluster-change-validator.md](docs/custom-resources/cluster-change-validator.md) |
| ManualTouchMonitor CRD reference | [docs/custom-resources/manual-touch-monitor.md](docs/custom-resources/manual-touch-monitor.md) |
| Rule types | [docs/rule-types/](docs/rule-types/) |
| CLI reference (watchctl) | [docs/cli/watchctl.md](docs/cli/watchctl.md) |
| Manual cluster deployment | [docs/deployment/cluster-setup.md](docs/deployment/cluster-setup.md) |
| TLS and cert-manager | [docs/deployment/tls-and-cert-manager.md](docs/deployment/tls-and-cert-manager.md) |
| Contributing and development | [docs/contributing/development.md](docs/contributing/development.md) |
| Go style guide | [docs/style-guide.md](docs/style-guide.md) |

---

## License

See [LICENSE](LICENSE).
