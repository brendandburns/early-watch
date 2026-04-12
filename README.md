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
              │ lists ChangeValidator rules for "services"
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

| Topic | Link |
|-------|------|
| Getting started | [docs/getting-started.md](docs/getting-started.md) |
| Architecture and source tree | [docs/architecture.md](docs/architecture.md) |
| ChangeValidator CRD reference | [docs/custom-resources/change-validator.md](docs/custom-resources/change-validator.md) |
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
