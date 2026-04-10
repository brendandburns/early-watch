// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
// +groupName=earlywatch.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChangeGuardSpec defines the desired state of ChangeGuard.
type ChangeGuardSpec struct {
	// Subject describes the Kubernetes resource that this guard watches.
	Subject SubjectResource `json:"subject"`

	// Operations is the list of admission operations this guard intercepts.
	// Valid values are: CREATE, UPDATE, DELETE, CONNECT.
	// +kubebuilder:validation:MinItems=1
	Operations []OperationType `json:"operations"`

	// Rules is the list of safety checks to evaluate when an intercepted
	// operation is received.  All rules are evaluated; if any rule is
	// violated the request is denied.
	// +kubebuilder:validation:MinItems=1
	Rules []GuardRule `json:"rules"`
}

// SubjectResource identifies the Kubernetes resource type this guard protects.
type SubjectResource struct {
	// APIGroup is the API group for the resource, e.g. "" for core, "apps" for Deployments.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Resource is the plural resource name, e.g. "services", "deployments".
	Resource string `json:"resource"`

	// NamespaceSelector optionally restricts this guard to namespaces that
	// match the given label selector.  When omitted the guard applies to
	// all namespaces.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// OperationType represents a Kubernetes admission operation.
// +kubebuilder:validation:Enum=CREATE;UPDATE;DELETE;CONNECT
type OperationType string

const (
	OperationCreate  OperationType = "CREATE"
	OperationUpdate  OperationType = "UPDATE"
	OperationDelete  OperationType = "DELETE"
	OperationConnect OperationType = "CONNECT"
)

// GuardRule is a single safety check within a ChangeGuard.
type GuardRule struct {
	// Name is a human-readable identifier for this rule.
	Name string `json:"name"`

	// Type selects the kind of check to perform.
	// +kubebuilder:validation:Enum=ExistingResources;ExpressionCheck
	Type RuleType `json:"type"`

	// ExistingResources configures a check that queries the cluster for
	// resources related to the subject and denies the request when any
	// matching resources are found.
	// Required when Type is ExistingResources.
	// +optional
	ExistingResources *ExistingResourcesCheck `json:"existingResources,omitempty"`

	// ExpressionCheck evaluates a CEL expression against the admission
	// request and denies the request when the expression returns true.
	// Required when Type is ExpressionCheck.
	// +optional
	ExpressionCheck *ExpressionCheck `json:"expressionCheck,omitempty"`

	// Message is the human-readable denial message returned to the user
	// when this rule is violated.
	Message string `json:"message"`
}

// RuleType identifies the kind of safety check a GuardRule performs.
type RuleType string

const (
	// RuleTypeExistingResources denies the request when related resources
	// exist in the cluster (e.g. pods that match a service's selector).
	RuleTypeExistingResources RuleType = "ExistingResources"

	// RuleTypeExpressionCheck denies the request when a CEL expression
	// evaluates to true against the admission request object.
	RuleTypeExpressionCheck RuleType = "ExpressionCheck"
)

// ExistingResourcesCheck describes a check that looks for dependent
// resources in the cluster and blocks the operation if any are found.
type ExistingResourcesCheck struct {
	// APIGroup is the API group of the dependent resource.
	// Use "" for core resources such as Pods.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Resource is the plural name of the dependent resource type,
	// e.g. "pods", "replicasets".
	Resource string `json:"resource"`

	// LabelSelectorFromField is a dot-separated JSON path into the
	// subject resource's spec that contains a map[string]string
	// to use as a label selector when querying dependent resources.
	// For example, "spec.selector" reads the selector from a Service.
	// +optional
	LabelSelectorFromField string `json:"labelSelectorFromField,omitempty"`

	// LabelSelector is a static label selector used when querying
	// dependent resources.  Mutually exclusive with LabelSelectorFromField.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// SameNamespace, when true, restricts the lookup to the same namespace
	// as the subject resource.  Defaults to true.
	// +kubebuilder:default=true
	// +optional
	SameNamespace *bool `json:"sameNamespace,omitempty"`
}

// ExpressionCheck evaluates a CEL expression to decide whether to deny
// the admission request.
type ExpressionCheck struct {
	// Expression is a Common Expression Language (CEL) expression.
	// The expression receives the admission request object as "request"
	// and must return a boolean.  When true, the request is denied.
	// Example: "request.operation == 'DELETE' && request.object == null"
	Expression string `json:"expression"`
}

// ChangeGuardStatus defines the observed state of ChangeGuard.
type ChangeGuardStatus struct {
	// Conditions represent the latest available observations of the
	// ChangeGuard's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation that the controller
	// has processed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cg,categories=earlywatch
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=`.spec.subject.resource`
// +kubebuilder:printcolumn:name="Operations",type=string,JSONPath=`.spec.operations`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ChangeGuard is the Schema for the changeguards API.
// A ChangeGuard defines a set of safety rules that the EarlyWatch
// admission controller evaluates before allowing a change to a
// Kubernetes resource.
type ChangeGuard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChangeGuardSpec   `json:"spec,omitempty"`
	Status ChangeGuardStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ChangeGuardList contains a list of ChangeGuard.
type ChangeGuardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ChangeGuard `json:"items"`
}
