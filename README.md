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

## Custom Resources

### `ChangeValidator` (`earlywatch.io/v1alpha1`)

A `ChangeValidator` defines the resources to watch, the operations to intercept, and the safety rules to enforce.

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: protect-service-from-deletion
  namespace: default
spec:
  # The Kubernetes resource type this guard protects.
  subject:
    apiGroup: ""        # "" = core API group
    resource: services

  # Admission operations that trigger rule evaluation.
  operations:
    - DELETE

  # Safety rules — all are evaluated; first violation denies the request.
  rules:
    - name: no-matching-pods
      type: ExistingResources
      existingResources:
        apiGroup: ""
        resource: pods
        # Read the label selector from spec.selector of the watched Service
        # and use it to query for matching Pods.
        labelSelectorFromField: spec.selector
        sameNamespace: true
      message: >
        This Service cannot be deleted because Pods that match its label
        selector are still running. Scale down or delete the matching Pods
        before removing the Service.
```

#### Rule Types

| Type | Description |
|------|-------------|
| `ExistingResources` | Denies the request when dependent resources (e.g. Pods) still exist in the cluster. |
| `ExpressionCheck` | Evaluates a simple expression against the admission request (e.g. `operation == 'DELETE'`). |

Full CRD schema: [`config/crd/bases/earlywatch.io_changevalidators.yaml`](config/crd/bases/earlywatch.io_changevalidators.yaml)

---

## Project Structure

```
early-watch/
├── cmd/
│   └── webhook/
│       └── main.go                   # Admission webhook server entry point
├── pkg/
│   ├── apis/
│   │   └── earlywatch/
│   │       └── v1alpha1/
│   │           ├── changevalidator_types.go      # ChangeValidator Go type definitions
│   │           ├── groupversion_info.go      # Scheme registration
│   │           └── zz_generated.deepcopy.go  # DeepCopy implementations
│   └── webhook/
│       ├── admission.go              # Admission webhook handler
│       └── admission_test.go         # Unit tests
├── config/
│   ├── crd/
│   │   └── bases/
│   │       └── earlywatch.io_changevalidators.yaml  # CRD manifest
│   ├── webhook/
│   │   ├── manifests.yaml            # ValidatingWebhookConfiguration
│   │   ├── service.yaml              # Webhook Service
│   │   └── deployment.yaml           # Webhook Deployment
│   ├── rbac/
│   │   ├── role.yaml                 # ClusterRole
│   │   └── role_binding.yaml         # ClusterRoleBinding + ServiceAccount
│   └── samples/
│       └── protect_service.yaml      # Example ChangeValidator
├── go.mod
└── README.md
```

---

## Getting Started

### Prerequisites

- Go 1.21+
- A Kubernetes cluster (v1.26+)
- `kubectl` configured to talk to the cluster
- [cert-manager](https://cert-manager.io/) (recommended for TLS certificate management)

### Build

```bash
go build ./cmd/webhook/...
```

### Run Tests

```bash
go test ./...
```

### Deploy to a Cluster

1. **Install the CRD**:
   ```bash
   kubectl apply -f config/crd/bases/earlywatch.io_changevalidators.yaml
   ```

2. **Create the namespace and RBAC**:
   ```bash
   kubectl create namespace early-watch-system
   kubectl apply -f config/rbac/
   ```

3. **Deploy the webhook server** (ensure TLS certificates are available):
   ```bash
   kubectl apply -f config/webhook/
   ```

4. **Apply a sample ChangeValidator**:
   ```bash
   kubectl apply -f config/samples/protect_service.yaml
   ```

5. **Test it** — try deleting a Service while matching Pods are running and observe the denial.

---

## License

See [LICENSE](LICENSE).
