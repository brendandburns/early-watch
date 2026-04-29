// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ccv,categories=earlywatch
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=`.spec.subject.resource`
// +kubebuilder:printcolumn:name="Operations",type=string,JSONPath=`.spec.operations`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterChangeValidator is the Schema for the clusterchangevalidators API.
// A ClusterChangeValidator defines a set of default safety rules that the
// EarlyWatch admission controller evaluates cluster-wide before allowing a
// change to a Kubernetes resource.  Unlike the namespaced ChangeValidator, a
// ClusterChangeValidator applies across all namespaces unless restricted by
// the subject's NamespaceSelector field.
type ClusterChangeValidator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChangeValidatorSpec   `json:"spec,omitempty"`
	Status ChangeValidatorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterChangeValidatorList contains a list of ClusterChangeValidator.
type ClusterChangeValidatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterChangeValidator `json:"items"`
}
