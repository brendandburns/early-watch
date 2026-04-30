# Architecture

This document explains how EarlyWatch works end-to-end and describes the layout of the source tree.

---

## How It Works

EarlyWatch runs as a Kubernetes [validating admission webhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/).  When a user or automation submits a change to the API server, the webhook is invoked before the change is persisted.  EarlyWatch looks up all `ChangeValidator` objects that match the resource being changed, evaluates their rules against the live cluster state, and either allows or denies the request.

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
              ├─ lists ChangeValidators for "services" + "DELETE" (same namespace)
              ├─ lists ClusterChangeValidators for "services" + "DELETE" (all namespaces)
              │
              ├─ evaluates rules against cluster state
              │     (e.g. queries matching Pods)
              ▼
    DENY: "This Service cannot be deleted because Pods that
           match its label selector are still running."
```

### Audit Monitor

EarlyWatch also ships an **audit monitor** component (`cmd/audit-monitor`) that watches the Kubernetes audit log for manual operations (e.g. `kubectl` commands) and records them as `ManualTouchEvent` custom resources.  These events can be queried by the `ManualTouchCheck` rule type to prevent automation from overwriting an operator's manual change.

---

## Custom Resources

EarlyWatch introduces four custom resource types in the `earlywatch.io/v1alpha1` API group:

| Kind | Short name | Scope | Purpose |
|------|-----------|-------|---------|
| `ChangeValidator` | `cv` | Namespaced | Defines the resources to protect and the rules to enforce within a single namespace. |
| `ClusterChangeValidator` | `ccv` | Cluster | Defines cluster-wide safety rules that apply across all namespaces (optionally restricted by `namespaceSelector`). |
| `ManualTouchMonitor` | `mtm` | Namespaced | Declares which resources and operations the audit monitor should watch for manual touches. |
| `ManualTouchEvent` | `mte` | Namespaced | Records a single detected manual touch; written by the audit monitor. |

---

## Source Tree

```
early-watch/
├── cmd/
│   ├── audit-monitor/          # Audit monitor server entry point
│   ├── watchctl/               # watchctl CLI (cobra root + subcommands)
│   └── webhook/                # Admission webhook server entry point
├── pkg/
│   ├── apis/
│   │   └── earlywatch/
│   │       └── v1alpha1/       # CRD Go type definitions and generated DeepCopy
│   ├── approve/                # Core approve logic (RSA-PSS sign + annotate)
│   ├── auditmonitor/           # Audit log detector, handler, and event recorder
│   ├── install/                # Install/uninstall logic with embedded manifests
│   └── webhook/                # Admission webhook handler and rule evaluators
├── config/
│   ├── crd/bases/              # Generated CRD YAML manifests
│   ├── rbac/                   # ClusterRole, ClusterRoleBinding, ServiceAccount
│   ├── webhook/                # Deployment, Service, ValidatingWebhookConfiguration
│   └── samples/                # Example ChangeValidator and ManualTouchMonitor objects
├── docs/                       # Project documentation (you are here)
├── scripts/                    # Developer helper scripts (lint, fix, install-hooks)
├── test/
│   └── e2e/                    # End-to-end tests
├── .golangci.yml               # golangci-lint configuration
├── Dockerfile                  # Webhook server container image
└── go.mod
```

---

## Request Evaluation Flow

1. The API server sends an `AdmissionReview` request to the webhook.
2. The webhook handler (`pkg/webhook/admission.go`) extracts the resource group, version, and resource name from the request.
3. It lists all `ChangeValidator` objects in the same namespace as the subject resource whose `spec.subject` matches the incoming resource and whose `spec.operations` includes the current operation.
4. Optional filters (`subject.names`, `subject.namespaceSelector`) are applied.
5. Each matching `ChangeValidator`'s rules are evaluated in order.  The first rule violation produces a denial response; if all rules pass, the webhook continues.
6. It then lists all `ClusterChangeValidator` objects and evaluates those whose `spec.subject` matches the request.  When a `namespaceSelector` is present the webhook fetches the namespace's labels and skips validators that do not match.
7. The first rule violation across any `ClusterChangeValidator` produces a denial response; if all pass, the request is allowed.
8. The denial message is returned verbatim to the user via `kubectl` or the API client.
