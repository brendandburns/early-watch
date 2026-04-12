# Copilot Code Review Instructions

This repository implements **EarlyWatch**, a Kubernetes admission controller written in Go.

## Focus Areas

When reviewing pull requests, prioritize the following:

### Correctness and Safety
- Admission webhook handlers must always return a well-formed `admission.Response`. Ensure every code path returns a fully populated allow, deny, or error response rather than an incomplete default response.
- Rule evaluation logic must be exhaustive — verify there are no unhandled rule types that could silently allow unsafe operations, and use the `RuleType` enum (or equivalent source-of-truth definition) rather than a partial inline list when reviewing implemented checks.
- Label selector construction from resource fields must correctly handle nil or empty selectors.

### Kubernetes API Usage
- Verify that all Kubernetes client calls use appropriate context propagation.
- List operations must specify namespace correctly — cluster-scoped vs. namespace-scoped resources must not be confused.
- Ensure informers and caches are used where appropriate instead of direct API calls in hot paths.

### Error Handling
- Errors from Kubernetes API calls must be propagated and not swallowed.
- Denial messages returned to users must be clear, actionable, and avoid exposing internal implementation details.

### Testing
- New rule types or webhook handlers must include unit tests covering both allow and deny paths.
- Tests should use the fake Kubernetes client (`sigs.k8s.io/controller-runtime/pkg/client/fake`) and not depend on a live cluster.

### Code Style
- Follow standard Go conventions (`gofmt`, `go vet`).
- Keep function signatures idiomatic; avoid returning multiple errors.
- Exported types and functions must have doc comments.

## Pre-PR Checklist

**CRITICAL: You must run the linter and tests before opening or pushing a pull request. Do not open or update a pull request without first running and fixing all linting errors locally.**

Before opening or finalizing a pull request, always run the following commands locally and ensure they all pass:

1. **Linters**: `golangci-lint run --config .golangci.yml ./...`
2. **Unit tests**: `go test ./pkg/... -v -count=1`

If either command fails, fix all reported issues before pushing. **Do not submit a pull request if either of these commands reports failures or formatting issues.**

## Common Lint Errors to Avoid

These errors appear repeatedly in CI failures. Pay close attention to them:

### errcheck — always check error return values
The `errcheck` linter requires that every function returning an `error` has its error value checked. This applies even to standard library I/O functions that are often ignored in example code.

**Bad** (will fail `errcheck`):
```go
fmt.Fprintln(w, "header")
fmt.Fprintf(w, "%s\n", value)
tw.Flush()
w.Close()
```

**Good** (check or explicitly discard each error):
```go
if _, err := fmt.Fprintln(w, "header"); err != nil {
    return err
}
if _, err := fmt.Fprintf(w, "%s\n", value); err != nil {
    return err
}
if err := tw.Flush(); err != nil {
    return err
}
if err := w.Close(); err != nil {
    return err
}
```

### revive — avoid repetitive exported names
Exported names must not repeat the package name. Go callers already qualify names with the package, so repeating it is redundant.

**Bad**: `func ApplyManifest(...)` in package `apply` → called as `apply.ApplyManifest`  
**Good**: `func Manifest(...)` in package `apply` → called as `apply.Manifest`

### unused — remove unused declarations
Delete any constants, variables, or imports that are defined but never used.

### misspell — use US English spelling
All comments and string literals must use American English. Common traps:
- `minimising` → `minimizing`
- `initialising` → `initializing`
- `colour` → `color`
- `behaviour` → `behavior`
