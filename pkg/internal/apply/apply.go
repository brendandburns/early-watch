// Package apply provides shared utilities for applying Kubernetes manifests
// via Server-Side Apply, used by both the install and add commands.
package apply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// FieldManager is the field manager name used for Server-Side Apply.
const FieldManager = "watchctl"

// Manifest splits a potentially multi-document YAML file and applies
// each document to the cluster using Server-Side Apply. The optional preApply
// function is called on each decoded object before it is applied; pass nil if
// no pre-processing is needed.
func Manifest(
	ctx context.Context,
	dynClient dynamic.Interface,
	mapper meta.RESTMapper,
	data []byte,
	filename string,
	preApply func(*unstructured.Unstructured),
) error {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		rawObj := make(map[string]interface{})
		if err := decoder.Decode(&rawObj); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decoding manifest %q: %w", filename, err)
		}
		if len(rawObj) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{Object: rawObj}
		gvk := obj.GroupVersionKind()
		if gvk.Kind == "" {
			continue
		}

		if obj.GetName() == "" {
			return fmt.Errorf("resource %s in %q is missing metadata.name", gvk.String(), filename)
		}

		if preApply != nil {
			preApply(obj)
		}

		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("mapping resource %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		var resourceClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns := obj.GetNamespace()
			if ns == "" {
				ns = "default"
				obj.SetNamespace(ns)
			}
			resourceClient = dynClient.Resource(mapping.Resource).Namespace(ns)
		} else {
			resourceClient = dynClient.Resource(mapping.Resource)
		}

		jsonData, err := json.Marshal(obj.Object)
		if err != nil {
			return fmt.Errorf("marshaling resource %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		if _, err := resourceClient.Patch(
			ctx,
			obj.GetName(),
			types.ApplyPatchType,
			jsonData,
			metav1.PatchOptions{FieldManager: FieldManager, Force: BoolPtr(true)},
		); err != nil {
			return fmt.Errorf("applying %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		fmt.Printf("Applied %s %q\n", gvk.Kind, ResourceDisplayName(obj))
	}
	return nil
}

// ResourceDisplayName returns a display name for an object, including its
// namespace when present.
func ResourceDisplayName(obj *unstructured.Unstructured) string {
	ns := obj.GetNamespace()
	if ns != "" {
		return ns + "/" + obj.GetName()
	}
	return obj.GetName()
}

// BuildRESTConfig returns a *rest.Config from the given kubeconfig path,
// falling back to in-cluster config when kubeconfig is empty.
func BuildRESTConfig(kubeconfig string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return cfg, nil
}

// BoolPtr returns a pointer to the bool value b.
func BoolPtr(b bool) *bool { return &b }
