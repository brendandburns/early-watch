// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
// +groupName=earlywatch.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChangeValidatorSpec defines the desired state of ChangeValidator.
type ChangeValidatorSpec struct {
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

	// Names optionally restricts this guard to only resources whose name is
	// in the list.  When omitted (or empty) the guard applies to all
	// resources of the given type.  For namespace-deletion guards this lets
	// you protect a specific set of namespaces rather than every namespace.
	// +optional
	Names []string `json:"names,omitempty"`

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
	// OperationCreate represents a CREATE admission operation.
	OperationCreate OperationType = "CREATE"
	// OperationUpdate represents an UPDATE admission operation.
	OperationUpdate OperationType = "UPDATE"
	// OperationDelete represents a DELETE admission operation.
	OperationDelete OperationType = "DELETE"
	// OperationConnect represents a CONNECT admission operation.
	OperationConnect OperationType = "CONNECT"
)

// GuardRule is a single safety check within a ChangeValidator.
type GuardRule struct {
	// Name is a human-readable identifier for this rule.
	Name string `json:"name"`

	// Type selects the kind of check to perform.
	// +kubebuilder:validation:Enum=ExistingResources;ExpressionCheck;NameReferenceCheck;ApprovalCheck;AnnotationCheck;CheckLock;ManualTouchCheck;ServicePodSelectorCheck;DataKeySafetyCheck
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

	// NameReferenceCheck checks whether the subject resource is referenced by
	// name in other cluster resources and denies the request when any such
	// references are found.
	// Required when Type is NameReferenceCheck.
	// +optional
	NameReferenceCheck *NameReferenceCheck `json:"nameReferenceCheck,omitempty"`

	// ApprovalCheck verifies that the resource carries a valid approval
	// annotation signed with the private key corresponding to the configured
	// public key.  The annotation value must be the base64-encoded RSA
	// signature of the resource's full path.
	// Required when Type is ApprovalCheck.
	// +optional
	ApprovalCheck *ApprovalCheck `json:"approvalCheck,omitempty"`

	// AnnotationCheck denies the request when the subject resource does not
	// carry a required annotation (with an optional specific value).  Use
	// this to implement a "confirm delete" pattern.
	// Required when Type is AnnotationCheck.
	// +optional
	AnnotationCheck *AnnotationCheck `json:"annotationCheck,omitempty"`

	// ManualTouchCheck denies the request when a recent manual touch
	// (kubectl operation) has been recorded for the same resource within
	// a configurable look-back window.  Use this to prevent automation
	// from overwriting an operator's manual change.
	// Required when Type is ManualTouchCheck.
	// +optional
	ManualTouchCheck *ManualTouchCheck `json:"manualTouchCheck,omitempty"`

	// CheckLock optionally configures the CheckLock rule.  When omitted the
	// rule applies only to DELETE operations (the default behavior).
	// +optional
	CheckLock *CheckLockRule `json:"checkLock,omitempty"`

	// ServicePodSelectorCheck denies an UPDATE to a Service when the service
	// previously selected at least one Pod but would select no Pods after the
	// change.  Headless services that have no selector are exempt.
	// Required when Type is ServicePodSelectorCheck.
	// +optional
	ServicePodSelectorCheck *ServicePodSelectorCheck `json:"servicePodSelectorCheck,omitempty"`

	// DataKeySafetyCheck prevents UPDATE requests that remove a data key
	// from a ConfigMap or Secret when that specific key is still referenced
	// by another resource.
	// Required when Type is DataKeySafetyCheck.
	// +optional
	DataKeySafetyCheck *DataKeySafetyCheck `json:"dataKeySafetyCheck,omitempty"`

	// Message is the human-readable denial message returned to the user
	// when this rule is violated.
	Message string `json:"message"`
}

// AnnotationCheck denies the request unless the subject resource has a
// specific annotation (and optionally a specific value for that annotation).
// For DELETE requests the annotation is read from the object being deleted
// (OldObject in the admission request).
type AnnotationCheck struct {
	// AnnotationKey is the annotation key that must be present on the
	// resource.  For example: "earlywatch.io/confirm-delete".
	AnnotationKey string `json:"annotationKey"`

	// AnnotationValue, if specified, is the exact value that the annotation
	// must have.  When omitted, any value (including the empty string) is
	// accepted as long as the key is present.
	// +optional
	AnnotationValue *string `json:"annotationValue,omitempty"`
}

// LockAnnotation is the annotation key that, when present on a resource,
// prevents it from being deleted (and optionally mutated).  Any non-empty
// annotation value is treated as a lock.
const LockAnnotation = "earlywatch.io/lock"

// CheckLockRule configures a CheckLock rule.  When omitted entirely the rule
// behaves with default settings (delete-only).
type CheckLockRule struct {
	// LockOnMutate, when true, extends the lock check to UPDATE operations in
	// addition to DELETE operations.  When false or omitted the check only
	// applies to DELETE requests, preserving the default behavior.
	// +optional
	LockOnMutate *bool `json:"lockOnMutate,omitempty"`
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

	// RuleTypeNameReferenceCheck denies the request when the subject resource
	// is referenced by name in other cluster resources.
	RuleTypeNameReferenceCheck RuleType = "NameReferenceCheck"

	// RuleTypeApprovalCheck denies the request unless the resource carries
	// a valid approval annotation whose value is the resource path signed
	// with the private key corresponding to the configured public key.
	RuleTypeApprovalCheck RuleType = "ApprovalCheck"

	// RuleTypeAnnotationCheck denies the request when the subject resource
	// does not have a required annotation (with an optional specific value).
	// Use this to implement a "confirm delete" pattern where a resource can
	// only be deleted after the operator adds a designated annotation.
	RuleTypeAnnotationCheck RuleType = "AnnotationCheck"

	// RuleTypeCheckLock denies a DELETE request (and optionally UPDATE requests)
	// when the subject resource carries the earlywatch.io/lock annotation.
	// Configure the optional checkLock field to extend the check to mutations.
	RuleTypeCheckLock RuleType = "CheckLock"

	// RuleTypeManualTouchCheck denies the request when a recent manual
	// touch (kubectl DELETE/CREATE/UPDATE) has been recorded for the same
	// resource within a configurable look-back window.
	RuleTypeManualTouchCheck RuleType = "ManualTouchCheck"

	// RuleTypeServicePodSelectorCheck denies an UPDATE to a Service when the
	// service previously selected at least one Pod but would select no Pods
	// after the change.  Headless services with no selector are exempt.
	RuleTypeServicePodSelectorCheck RuleType = "ServicePodSelectorCheck"

	// RuleTypeDataKeySafetyCheck denies an UPDATE when a data key is removed
	// from a ConfigMap or Secret that is still referenced by its specific key
	// name in another cluster resource (e.g. a Pod's configMapKeyRef or
	// secretKeyRef).
	RuleTypeDataKeySafetyCheck RuleType = "DataKeySafetyCheck"
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

// NameReferenceCheck describes a check that finds resources which reference
// the subject resource by name and blocks the operation when any such
// references are found.
type NameReferenceCheck struct {
	// Resources is the list of resource types to scan for references to the
	// subject resource.
	// +kubebuilder:validation:MinItems=1
	Resources []NameReferenceResource `json:"resources"`

	// SameNamespace, when true, restricts the lookup to the same namespace
	// as the subject resource.  Defaults to true.
	// +kubebuilder:default=true
	// +optional
	SameNamespace *bool `json:"sameNamespace,omitempty"`
}

// NameReferenceResource describes a single resource type to scan for
// references to the subject resource by name.
type NameReferenceResource struct {
	// APIGroup is the API group of the resource type to scan.
	// Use "" for core resources and "apps" for Deployments/DaemonSets.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Resource is the plural name of the resource type to scan,
	// e.g. "deployments", "daemonsets", "cronjobs".
	Resource string `json:"resource"`

	// Version is the API version of the resource type to scan.
	// Defaults to "v1" when omitted.
	// +optional
	Version string `json:"version,omitempty"`

	// NameFields is the list of dot-separated JSON field paths at which
	// the subject resource's name might appear.  Array elements encountered
	// along any path are traversed automatically.  For example, to detect a
	// ConfigMap volume reference use
	// "spec.template.spec.volumes.configMap.name".
	// +kubebuilder:validation:MinItems=1
	NameFields []string `json:"nameFields"`
}

// ChangeValidatorStatus defines the observed state of ChangeValidator.
type ChangeValidatorStatus struct {
	// Conditions represent the latest available observations of the
	// ChangeValidator's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation that the controller
	// has processed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cv,categories=earlywatch
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=`.spec.subject.resource`
// +kubebuilder:printcolumn:name="Operations",type=string,JSONPath=`.spec.operations`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ChangeValidator is the Schema for the changevalidators API.
// A ChangeValidator defines a set of safety rules that the EarlyWatch
// admission controller evaluates before allowing a change to a
// Kubernetes resource.
type ChangeValidator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChangeValidatorSpec   `json:"spec,omitempty"`
	Status ChangeValidatorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ChangeValidatorList contains a list of ChangeValidator.
type ChangeValidatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ChangeValidator `json:"items"`
}

// ManualTouchCheck denies the request when a ManualTouchEvent has been
// recorded for the same resource within the configured look-back window.
// This prevents automated pipelines from overwriting manual operator changes.
type ManualTouchCheck struct {
	// WindowDuration is the look-back period used to search for recent
	// manual touch events.  The value must be a Go duration string, e.g.
	// "30m", "2h", or "24h".  Defaults to "1h" when omitted.
	// +optional
	WindowDuration string `json:"windowDuration,omitempty"`

	// EventNamespace is the namespace where ManualTouchEvent resources are
	// stored.  Defaults to "early-watch-system" when omitted.
	// +optional
	EventNamespace string `json:"eventNamespace,omitempty"`
}

// ApprovalCheck configures a rule that requires the resource to carry a valid
// approval annotation before a change is permitted.
//
// For DELETE operations the annotation value must be the base64-encoded
// RSA-PSS SHA-256 signature of the resource's canonical path, signed with the
// private key corresponding to PublicKey.  The canonical path is computed as:
//
//	<group>/<version>/namespaces/<namespace>/<resource>/<name>   (namespaced)
//	<group>/<version>/<resource>/<name>                          (cluster-scoped)
//
// For UPDATE operations a separate annotation holds a base64-encoded
// RSA-PSS SHA-256 signature covering the JSON merge patch (RFC 7396) between
// the current and the proposed resource state.  Server-managed metadata fields
// (resourceVersion, generation, uid, creationTimestamp, managedFields,
// selfLink) and the change-approval annotation itself are excluded from the
// patch before signing so that the signature covers only user-visible intent.
// The pre-approval annotation must be placed on the existing resource before
// the UPDATE is applied (e.g. via watchctl approve change).
type ApprovalCheck struct {
	// PublicKey is the PEM-encoded RSA public key (PKIX/SubjectPublicKeyInfo
	// format) used to verify both delete- and change-approval signatures.
	PublicKey string `json:"publicKey"`

	// AnnotationKey is the annotation on the resource whose value holds the
	// base64-encoded delete-approval signature.
	// Defaults to "earlywatch.io/approved".
	// +optional
	// +kubebuilder:default="earlywatch.io/approved"
	AnnotationKey string `json:"annotationKey,omitempty"`

	// ChangeAnnotationKey is the annotation on the existing resource whose
	// value holds the base64-encoded change-approval (UPDATE) signature.
	// The annotation is read from the old object (the resource as it exists
	// in the cluster before the update) and must have been placed there by
	// watchctl approve change before the UPDATE is submitted.
	// Defaults to "earlywatch.io/change-approved".
	// +optional
	// +kubebuilder:default="earlywatch.io/change-approved"
	ChangeAnnotationKey string `json:"changeAnnotationKey,omitempty"`
}

// ServicePodSelectorCheck configures the service-to-pod selector safety check.
// It prevents a Service UPDATE from dropping all Pod references when the service
// previously had at least one matching Pod.  Headless services (spec.clusterIP
// == "None") that carry no selector are exempt from this check.
type ServicePodSelectorCheck struct{}

// DataKeySafetyCheck describes a check that prevents UPDATE requests from
// removing a data key from a ConfigMap or Secret when that specific key is
// still referenced (by both resource name and key name) in another cluster
// resource.
type DataKeySafetyCheck struct {
	// Resources is the list of resource types to scan for key references.
	// +kubebuilder:validation:MinItems=1
	Resources []DataKeyReferenceResource `json:"resources"`

	// SameNamespace, when true, restricts the lookup to the same namespace
	// as the subject resource.  Defaults to true.
	// +kubebuilder:default=true
	// +optional
	SameNamespace *bool `json:"sameNamespace,omitempty"`
}

// DataKeyReferenceResource describes a resource type to scan for references
// that pair a ConfigMap or Secret name with a specific data key.
type DataKeyReferenceResource struct {
	// APIGroup is the API group of the resource type to scan.
	// Use "" for core resources and "apps" for Deployments/DaemonSets.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Resource is the plural name of the resource type to scan,
	// e.g. "pods", "deployments".
	Resource string `json:"resource"`

	// Version is the API version of the resource type to scan.
	// Defaults to "v1" when omitted.
	// +optional
	Version string `json:"version,omitempty"`

	// KeyReferenceFields is the list of field-path descriptors that identify
	// locations in the resource where a ConfigMap or Secret name and a data
	// key are referenced together.
	// +kubebuilder:validation:MinItems=1
	KeyReferenceFields []KeyReferenceField `json:"keyReferenceFields"`
}

// KeyReferenceField describes a single location in a resource where a
// ConfigMap or Secret name and a specific data key appear together as
// sibling fields within the same JSON object.
type KeyReferenceField struct {
	// RefPath is the dot-separated JSON path to the object that contains
	// both the name and key sub-fields.  Array elements encountered along
	// the path are traversed automatically.
	// Example: "spec.template.spec.containers.env.valueFrom.configMapKeyRef"
	RefPath string `json:"refPath"`

	// NameSubField is the field name within the RefPath object that holds
	// the ConfigMap or Secret name.  Defaults to "name".
	// +optional
	NameSubField string `json:"nameSubField,omitempty"`

	// KeySubField is the field name within the RefPath object that holds
	// the data key.  Defaults to "key".
	// +optional
	KeySubField string `json:"keySubField,omitempty"`
}
