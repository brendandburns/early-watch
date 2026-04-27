// Package v1alpha1 contains API Schema definitions for the earlywatch.io v1alpha1 API group.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "earlywatch.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion,
		&ChangeValidator{}, &ChangeValidatorList{},
		&ManualTouchMonitor{}, &ManualTouchMonitorList{},
		&ManualTouchEvent{}, &ManualTouchEventList{},
	)
	return nil
}

// Resource returns a GroupResource for the given resource string.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}
