# Go Style Guide

This document describes the coding conventions used in **early-watch**.
Following these guidelines keeps the codebase consistent and makes reviews
easier.  They extend (rather than replace) the official
[Effective Go](https://go.dev/doc/effective_go) and
[Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
guides.

---

## Toolchain

| Tool | Purpose |
|------|---------|
| `gofmt` | Canonical formatting — run automatically via golangci-lint |
| `goimports` | Manages import groups — run automatically via golangci-lint |
| `golangci-lint` | Aggregates all linters; config lives in `.golangci.yml` |

Run the linter before every commit:

```sh
golangci-lint run --config .golangci.yml ./...
```

Install the provided git hooks (runs the linter on every `git commit` and
`git push`) with:

```sh
./scripts/install-hooks.sh
```

---

## Formatting

* **Never commit unformatted code.**  `gofmt -s` is enforced by CI.
* Indentation uses **tabs**, not spaces.
* Lines have no hard limit, but keep them readable (≤ 120 characters is a
  good guideline).

---

## Imports

Group imports in three blocks, separated by a blank line:

```go
import (
    // 1. Standard library
    "context"
    "fmt"

    // 2. Third-party packages
    "sigs.k8s.io/controller-runtime/pkg/client"

    // 3. Internal packages (github.com/brendandburns/early-watch/...)
    ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)
```

`goimports` enforces this grouping automatically.

---

## Naming

* Follow the standard Go naming conventions — see
  [Go Code Review Comments § Naming](https://github.com/golang/go/wiki/CodeReviewComments#package-names).
* **Packages** — short, lowercase, single words; no underscores or mixedCase
  (`webhook`, not `admissionWebhook`).
* **Exported names** — use `MixedCaps`; avoid stuttering
  (`webhook.Handler`, not `webhook.WebhookHandler`).
* **Error variables** — prefix with `Err` (`ErrNotFound`).
* **Interfaces** — use the `-er` suffix for single-method interfaces
  (`Validator`, `Lister`).
* **Acronyms** — keep them fully capitalised (`HTTPClient`, `APIServer`).

---

## Comments

* Every exported symbol **must** have a doc comment.
* Comments must be full sentences ending with a period (enforced by `godot`).
* Comments should describe *what* and *why*, not *how*.
* Package-level doc comments go in a dedicated `doc.go` file for larger
  packages, or at the top of the main file for small ones.

```go
// AdmissionHandler handles admission webhook requests by evaluating
// ChangeValidator rules registered in the cluster.
type AdmissionHandler struct { ... }
```

---

## Error Handling

* **Never ignore errors.**  `errcheck` enforces this.
* Wrap errors with context using `fmt.Errorf("…: %w", err)`.
* Return early on errors — do not nest happy-path code in `if err == nil`
  blocks.
* Use `errors.Is` / `errors.As` for type-safe error comparisons.

```go
result, err := doSomething(ctx)
if err != nil {
    return fmt.Errorf("doSomething: %w", err)
}
```

---

## Context

* All functions that perform I/O or make network calls **must** accept a
  `context.Context` as their first argument (enforced by `noctx`).
* Never store a `Context` inside a struct; pass it explicitly.

---

## Testing

* Test files live alongside the package they test (`foo_test.go`).
* Use `_test` package suffix for black-box tests.
* Table-driven tests are preferred for multiple input/output cases.
* Test helper functions should call `t.Helper()` as their first statement.

---

## Concurrency

* Prefer channels over shared memory when passing ownership of data.
* Protect shared mutable state with `sync.Mutex` or `sync.RWMutex`.
* Document which goroutine owns which resource.

---

## Kubernetes-specific Conventions

* Use `sigs.k8s.io/controller-runtime/pkg/log` for structured logging; never
  use `fmt.Print*` for operational output.
* Log at `Info` for normal events and `Error` for failures; include relevant
  key-value pairs.
* Use `client.Object` / `client.ObjectList` interfaces rather than concrete
  types where possible.
* Finalizers and annotations must be defined as typed constants, not bare
  string literals.

---

## CI Enforcement

The linting workflow (`.github/workflows/lint.yml`) runs on every pull request
targeting `main`.  A PR cannot be merged until the lint job passes.
