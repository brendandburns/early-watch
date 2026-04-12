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
    # Optional: restrict this guard to specific resource names only.
    # names:
    #   - my-critical-service
    # Optional: restrict this guard to namespaces that match a label selector.
    # namespaceSelector:
    #   matchLabels:
    #     env: production

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

#### `subject.names` — Protecting Specific Resources

The optional `names` list restricts a guard to a named subset of resources.  For example, to protect only the `production` and `staging` namespaces:

```yaml
spec:
  subject:
    apiGroup: ""
    resource: namespaces
    names:
      - production
      - staging
```

#### `subject.namespaceSelector` — Namespace-Scoped Guards

The optional `namespaceSelector` restricts a guard to namespaces whose labels match a Kubernetes `LabelSelector`.  This lets you protect resources only in namespaces tagged `env: production` without enumerating each namespace by name.

#### Rule Types

| Type | Description |
|------|-------------|
| `ExistingResources` | Denies the request when dependent resources (e.g. Pods) still exist in the cluster. |
| `ExpressionCheck` | Evaluates a simple expression against the admission request (e.g. `operation == 'DELETE'`). |
| `NameReferenceCheck` | Denies the request when the subject resource is referenced by name in other cluster resources (e.g. a ConfigMap referenced by a Deployment volume). |
| `CheckLock` | Denies a DELETE request when the subject resource carries the `earlywatch.io/lock` annotation. |
| `ApprovalCheck` | Denies the operation unless the resource carries a valid RSA-PSS SHA-256 approval signature in an annotation. Use `watchctl approve` to create the signature. |

Full CRD schema: [`config/crd/bases/earlywatch.io_changevalidators.yaml`](config/crd/bases/earlywatch.io_changevalidators.yaml)

#### `NameReferenceCheck` — Blocking Deletion When Referenced by Name

`NameReferenceCheck` scans other cluster resources for references to the subject by name and blocks the operation if any are found.  This is useful for preventing deletion of resources such as ConfigMaps or Secrets that are still mounted or referenced by workloads.

```yaml
  rules:
    - name: configmap-not-referenced-by-workloads
      type: NameReferenceCheck
      nameReferenceCheck:
        sameNamespace: true
        resources:
          - apiGroup: apps
            resource: deployments
            version: v1
            nameFields:
              - spec.template.spec.volumes.configMap.name
              - spec.template.spec.containers.envFrom.configMapRef.name
              - spec.template.spec.containers.env.valueFrom.configMapKeyRef.name
      message: >
        This ConfigMap cannot be deleted because it is still referenced by
        one or more Deployments. Remove all references before deleting it.
```

Array elements along any `nameField` path are traversed automatically — no wildcard syntax is required.

---

#### `CheckLock` — Annotation-Based Lock

The `CheckLock` rule type denies a **DELETE** request when the subject resource carries the annotation `earlywatch.io/lock` with a non-empty value.  This provides a simple opt-in lock that operators can set on any resource without deploying a new `ChangeValidator`.

```yaml
  rules:
    - name: resource-must-not-be-locked
      type: CheckLock
      message: >
        This resource is locked. Remove the earlywatch.io/lock annotation
        before deleting it.
```

To lock a resource:

```bash
kubectl annotate deployment my-app earlywatch.io/lock="protected by team-ops"
```

To unlock it:

```bash
kubectl annotate deployment my-app earlywatch.io/lock-
```

---

## Project Structure

```
early-watch/
├── cmd/
│   ├── watchctl/
│   │   ├── main.go                   # watchctl CLI entry point (cobra root command)
│   │   ├── approve.go                # watchctl approve subcommand
│   │   └── install.go                # watchctl install subcommand
│   └── webhook/
│       └── main.go                   # Admission webhook server entry point
├── pkg/
│   ├── approve/
│   │   ├── approve.go                # Core approve logic (sign + annotate)
│   │   └── approve_test.go           # Unit tests for approve logic
│   ├── install/
│   │   ├── install.go                # Core install logic (embed + apply manifests)
│   │   └── manifests/                # Embedded YAML manifests (CRD, RBAC, webhook)
│   ├── apis/
│   │   └── earlywatch/
│   │       └── v1alpha1/
│   │           ├── changevalidator_types.go      # ChangeValidator Go type definitions
│   │           ├── changevalidator_types_test.go # Unit tests for types
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
│       ├── protect_service.yaml                      # Protect Service (ExistingResources)
│       ├── protect_service_referenced_by_ingress.yaml # Protect Service (NameReferenceCheck)
│       ├── protect_configmap_from_deletion.yaml      # Protect ConfigMap (NameReferenceCheck)
│       ├── protect_secret_from_deletion.yaml         # Protect Secret (NameReferenceCheck)
│       └── protect_namespace_from_deletion.yaml      # Protect Namespace (cluster-scoped)
├── test/
│   └── e2e/                          # End-to-end tests
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
# Admission webhook server
go build ./cmd/webhook/...

# watchctl CLI
go build ./cmd/watchctl/...
```

### Run Tests

```bash
go test ./...
```

---

## watchctl — The EarlyWatch CLI

`watchctl` is the command-line companion to the EarlyWatch webhook.

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `watchctl install` | Install all EarlyWatch infrastructure (CRD, RBAC, webhook) onto the current cluster. |
| `watchctl approve` | Sign a Kubernetes resource's canonical path with an RSA private key and write the signature as an approval annotation. |

### `watchctl install`

The `install` subcommand applies all EarlyWatch infrastructure manifests in the correct order using Server-Side Apply.  Running it multiple times is safe (idempotent).

```bash
# Install using the current kubeconfig context
watchctl install

# Install with an explicit kubeconfig
watchctl install --kubeconfig ~/.kube/config

# Install with a custom webhook image
watchctl install --image ghcr.io/my-org/early-watch:v1.2.3
```

**Optional flags**

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `""` | Path to kubeconfig; falls back to in-cluster config. |
| `--image` | `early-watch:latest` | Container image for the webhook Deployment. |

### `watchctl approve`

The `approve` subcommand is used together with an `ApprovalCheck` rule.  It signs the resource's canonical path with your RSA private key and patches the signature as an annotation onto the resource.  The webhook then verifies the signature before allowing the operation.

```bash
watchctl approve \
  --private-key /path/to/private-key.pem \
  --group "" \
  --version v1 \
  --resource configmaps \
  --namespace default \
  --name my-config
```

**Required flags**

| Flag | Description |
|------|-------------|
| `--private-key` | Path to a PEM-encoded RSA private key (PKCS#1 or PKCS#8). |
| `--resource` | Plural resource name, e.g. `configmaps`, `deployments`. |
| `--name` | Name of the resource to approve. |

**Optional flags**

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `""` | API group (empty string for core resources). |
| `--version` | `v1` | API version. |
| `--namespace` | `""` | Namespace (leave empty for cluster-scoped resources). |
| `--annotation-key` | `earlywatch.io/approved` | Annotation key to write the signature to. |
| `--kubeconfig` | `""` | Path to kubeconfig; falls back to in-cluster config. |


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

4. **Apply sample ChangeValidators**:
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

5. **Test it** — try deleting a Service while matching Pods are running and observe the denial.

---

## Development

### Linting

This project uses [golangci-lint](https://golangci-lint.run/) to enforce code style and catch common issues. The linter configuration lives in `.golangci.yml` and the full style guide is at [`docs/style-guide.md`](docs/style-guide.md).

**Set up git hooks and install the linter (one-time):**

```sh
./scripts/install-hooks.sh
```

This configures git to run the linter automatically on every `git commit` and `git push`.

**Run the linter:**

```sh
./scripts/lint.sh
```

**Auto-fix formatting and style issues** (runs `gofmt -s`, `goimports`, and `golangci-lint --fix`):

```sh
./scripts/fix.sh
```

Linting is also enforced in CI — the `Lint` workflow runs on every pull request targeting `main`.

---

## License

See [LICENSE](LICENSE).
