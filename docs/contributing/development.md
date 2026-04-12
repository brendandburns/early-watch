# Development Guide

This guide covers how to build, test, lint, and contribute to EarlyWatch.

---

## Prerequisites

- Go 1.21+
- [golangci-lint](https://golangci-lint.run/) (installed automatically by the setup script below)

---

## Initial Setup

Install the git hooks and the linter in one step:

```sh
./scripts/install-hooks.sh
```

This configures git to run the linter automatically on every `git commit` and `git push`.

---

## Build

```bash
# Admission webhook server
go build ./cmd/webhook/...

# watchctl CLI
go build ./cmd/watchctl/...

# Audit monitor
go build ./cmd/audit-monitor/...
```

---

## Run Tests

Unit tests:

```bash
go test ./pkg/... -v -count=1
```

All tests (including any integration tests):

```bash
go test ./...
```

Tests use the fake Kubernetes client (`sigs.k8s.io/controller-runtime/pkg/client/fake`) and do not require a live cluster.

---

## Linting

This project uses [golangci-lint](https://golangci-lint.run/) with the configuration in `.golangci.yml`.  See [style-guide.md](../style-guide.md) for the coding conventions enforced by the linter.

**Run the linter:**

```sh
./scripts/lint.sh
```

**Auto-fix formatting and style issues** (runs `gofmt -s`, `goimports`, and `golangci-lint --fix`):

```sh
./scripts/fix.sh
```

**Run the linter directly:**

```sh
golangci-lint run --config .golangci.yml ./...
```

Linting is also enforced in CI — the `Lint` workflow runs on every pull request targeting `main`.

---

## Pre-PR Checklist

Before opening or finalising a pull request, ensure all of the following pass:

```bash
# 1. Unit tests
go test ./pkg/... -v -count=1

# 2. Linter
golangci-lint run --config .golangci.yml ./...
```

Do not submit a pull request if either command reports failures.

---

## CI Workflows

| Workflow | Trigger | What it does |
|----------|---------|--------------|
| `Lint` | Pull request targeting `main` | Runs `golangci-lint` |
| `Unit Tests` | Pull request targeting `main` | Runs `go test ./pkg/... -v -count=1` |

Workflow definitions live in `.github/workflows/`.
