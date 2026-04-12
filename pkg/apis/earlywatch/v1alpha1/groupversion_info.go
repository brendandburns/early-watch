// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "earlywatch.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&ChangeValidator{}, &ChangeValidatorList{})
	SchemeBuilder.Register(&ManualTouchMonitor{}, &ManualTouchMonitorList{})
	SchemeBuilder.Register(&ManualTouchEvent{}, &ManualTouchEventList{})
}

// Resource returns a GroupResource for the given resource string.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}
