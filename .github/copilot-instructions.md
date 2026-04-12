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

Before opening or finalizing a pull request, always run the following commands locally and ensure they all pass:

1. **Unit tests**: `go test ./pkg/... -v -count=1`
2. **Formatting linters**: `golangci-lint run --config .golangci.yml ./...`

Do not submit a pull request if either of these commands reports failures or formatting issues.
