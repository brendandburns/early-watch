// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManualTouchMonitorSpec defines which resources and operations are watched for
// manual (kubectl) touches.
type ManualTouchMonitorSpec struct {
	// Subjects is the list of Kubernetes resource types to monitor.
	// +kubebuilder:validation:MinItems=1
	Subjects []MonitorSubject `json:"subjects"`

	// Operations is the list of operations to flag as manual touches.
	// Valid values: DELETE, CREATE, UPDATE.
	// +kubebuilder:validation:MinItems=1
	Operations []MonitorOperationType `json:"operations"`

	// UserAgentPatterns is a list of regular expressions that identify
	// "manual" user agents.  A request is considered a manual touch when
	// its User-Agent matches at least one pattern.  Defaults to
	// ["^kubectl/"] when the list is empty.
	// +optional
	UserAgentPatterns []string `json:"userAgentPatterns,omitempty"`

	// ExcludeServiceAccounts is a list of Kubernetes service-account
	// usernames (in the form "system:serviceaccount:<ns>:<name>") whose
	// operations should never be flagged as manual touches.
	// +optional
	ExcludeServiceAccounts []string `json:"excludeServiceAccounts,omitempty"`

	// Alerting configures optional notification sinks that receive a
	// message whenever a manual touch is detected.
	// +optional
	Alerting *AlertingConfig `json:"alerting,omitempty"`
}

// MonitorSubject identifies a Kubernetes resource type to monitor.
type MonitorSubject struct {
	// APIGroup is the API group for the resource.
	// Use "" for core resources (Pods, Services, …).
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Resource is the plural resource name, e.g. "services", "deployments".
	Resource string `json:"resource"`

	// NamespaceSelector optionally restricts monitoring to namespaces whose
	// labels match this selector.  When omitted the monitor applies to all
	// namespaces.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// MonitorOperationType is an operation verb that can trigger manual-touch
// detection.
// +kubebuilder:validation:Enum=DELETE;CREATE;UPDATE
type MonitorOperationType string

const (
	MonitorOperationDelete MonitorOperationType = "DELETE"
	MonitorOperationCreate MonitorOperationType = "CREATE"
	MonitorOperationUpdate MonitorOperationType = "UPDATE"
)

// AlertingConfig holds optional notification sink configuration.
type AlertingConfig struct {
	// SlackWebhookURL is a Slack incoming-webhook URL.  When set, the
	// audit monitor POSTs a message to this URL for every detected manual
	// touch.
	// +optional
	SlackWebhookURL string `json:"slackWebhookURL,omitempty"`

	// PrometheusLabels is an optional set of static labels that are added
	// to the earlywatch_manual_touch_total Prometheus counter for events
	// detected by this monitor.
	// +optional
	PrometheusLabels map[string]string `json:"prometheusLabels,omitempty"`
}

// ManualTouchMonitorStatus defines the observed state of ManualTouchMonitor.
type ManualTouchMonitorStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation processed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// TouchesDetected is the total number of manual touch events recorded
	// by this monitor since it was created.
	// +optional
	TouchesDetected int64 `json:"touchesDetected,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mtm,categories=earlywatch
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Touches",type=integer,JSONPath=`.status.touchesDetected`

// ManualTouchMonitor is the Schema for the manualtouchmonitors API.
// It declares which resources and operations the audit monitor should
// watch for manual (kubectl) touches.
type ManualTouchMonitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManualTouchMonitorSpec   `json:"spec,omitempty"`
	Status ManualTouchMonitorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManualTouchMonitorList contains a list of ManualTouchMonitor.
type ManualTouchMonitorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManualTouchMonitor `json:"items"`
}
