// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManualTouchEventSpec records a single detected manual touch operation.
type ManualTouchEventSpec struct {
	// Timestamp is the time the operation was received by the API server.
	Timestamp metav1.Time `json:"timestamp"`

	// User is the Kubernetes username that performed the operation
	// (from the audit event's user.username field).
	User string `json:"user"`

	// UserAgent is the raw User-Agent string sent with the request,
	// e.g. "kubectl/v1.29.0 (linux/amd64)".
	UserAgent string `json:"userAgent"`

	// Operation is the HTTP verb of the operation: DELETE, CREATE, or UPDATE.
	Operation string `json:"operation"`

	// APIGroup is the API group of the touched resource (empty for core resources).
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Resource is the plural resource type, e.g. "services", "deployments".
	Resource string `json:"resource"`

	// ResourceName is the name of the specific resource that was touched.
	ResourceName string `json:"resourceName"`

	// ResourceNamespace is the namespace of the touched resource.
	// Empty for cluster-scoped resources.
	// +optional
	ResourceNamespace string `json:"resourceNamespace,omitempty"`

	// SourceIP is the originating IP address recorded in the audit event.
	// +optional
	SourceIP string `json:"sourceIP,omitempty"`

	// AuditID is the Kubernetes audit event ID, useful for cross-referencing
	// with the raw audit log.
	AuditID string `json:"auditID"`

	// MonitorName is the name of the ManualTouchMonitor that generated this
	// event.
	MonitorName string `json:"monitorName"`

	// MonitorNamespace is the namespace of the ManualTouchMonitor that
	// generated this event.
	MonitorNamespace string `json:"monitorNamespace"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=mte,categories=earlywatch
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.spec.user`
// +kubebuilder:printcolumn:name="Operation",type=string,JSONPath=`.spec.operation`
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=`.spec.resource`
// +kubebuilder:printcolumn:name="ResourceName",type=string,JSONPath=`.spec.resourceName`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.resourceNamespace`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ManualTouchEvent records a single manual touch that was detected by the
// EarlyWatch audit monitor.  Operators can query these with
// `kubectl get manualtouchevents`.
type ManualTouchEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ManualTouchEventSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ManualTouchEventList contains a list of ManualTouchEvent.
type ManualTouchEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManualTouchEvent `json:"items"`
}
