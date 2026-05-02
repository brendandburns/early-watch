# Change Validator Examples

This directory contains one example `ChangeValidator` manifest for each validator type, plus an example `ClusterChangeValidator`.

| Kind | Validator Type / Description | Example |
|------|------------------------------|---------|
| `ChangeValidator` | `ExistingResources` | [existing-resources.yaml](existing-resources.yaml) |
| `ChangeValidator` | `NameReferenceCheck` | [name-reference-check.yaml](name-reference-check.yaml) |
| `ChangeValidator` | `AnnotationCheck` | [annotation-check.yaml](annotation-check.yaml) |
| `ChangeValidator` | `ApprovalCheck` | [approval-check.yaml](approval-check.yaml) |
| `ChangeValidator` | `CheckLock` | [check-lock.yaml](check-lock.yaml) |
| `ChangeValidator` | `ExpressionCheck` | [expression-check.yaml](expression-check.yaml) |
| `ChangeValidator` | `ManualTouchCheck` | [manual-touch-check.yaml](manual-touch-check.yaml) |
| `ChangeValidator` | `ServicePodSelectorCheck` | [service-pod-selector-check.yaml](service-pod-selector-check.yaml) |
| `ChangeValidator` | `DataKeySafetyCheck` | [data-key-safety-check.yaml](data-key-safety-check.yaml) |
| `ClusterChangeValidator` | Cluster-wide service protection (namespaceSelector) | [cluster-change-validator.yaml](cluster-change-validator.yaml) |
