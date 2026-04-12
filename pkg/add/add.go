// Package add provides the core logic for applying ChangeValidator YAML
// manifests from a file or directory path onto a Kubernetes cluster.
package add

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// fieldManager is the field manager name used for Server-Side Apply.
const fieldManager = "watchctl"

// Options holds the parameters for an add operation.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Falls back to in-cluster
	// config when empty.
	Kubeconfig string
	// Path is the path to a YAML file or a directory containing YAML files.
	Path string
}

// Run applies all ChangeValidator manifests found at opts.Path to the cluster
// described by opts.Kubeconfig, printing progress to stdout.
func Run(opts Options) error {
	cfg, err := buildRESTConfig(opts.Kubeconfig)
	if err != nil {
		return fmt.Errorf("building REST config: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

	ctx := context.Background()

	files, err := collectYAMLFiles(opts.Path)
	if err != nil {
		return err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", file, err)
		}
		if err := applyManifest(ctx, dynClient, mapper, data, filepath.Base(file)); err != nil {
			return err
		}
	}

	return nil
}

// collectYAMLFiles returns the list of YAML files to process for the given
// path. If path is a directory, all files ending in .yaml or .yml within that
// directory (non-recursively) are returned. If path is a file, a single-element
// slice containing that path is returned.
func collectYAMLFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("accessing path %q: %w", path, err)
	}

	if !info.IsDir() {
		return []string{path}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", path, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			files = append(files, filepath.Join(path, name))
		}
	}
	return files, nil
}

// applyManifest splits a potentially multi-document YAML file and applies
// each document to the cluster using Server-Side Apply.
func applyManifest(
	ctx context.Context,
	dynClient dynamic.Interface,
	mapper meta.RESTMapper,
	data []byte,
	filename string,
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

		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("mapping resource %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		jsonData, err := json.Marshal(obj.Object)
		if err != nil {
			return fmt.Errorf("marshaling resource %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		var resourceClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns := obj.GetNamespace()
			if ns == "" {
				ns = "default"
			}
			resourceClient = dynClient.Resource(mapping.Resource).Namespace(ns)
		} else {
			resourceClient = dynClient.Resource(mapping.Resource)
		}

		if _, err := resourceClient.Patch(
			ctx,
			obj.GetName(),
			types.ApplyPatchType,
			jsonData,
			metav1.PatchOptions{FieldManager: fieldManager, Force: boolPtr(true)},
		); err != nil {
			return fmt.Errorf("applying %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		fmt.Printf("Applied %s %q\n", gvk.Kind, resourceDisplayName(obj))
	}
	return nil
}

// resourceDisplayName returns a display name for an object, including its
// namespace when present.
func resourceDisplayName(obj *unstructured.Unstructured) string {
	ns := obj.GetNamespace()
	if ns != "" {
		return ns + "/" + obj.GetName()
	}
	return obj.GetName()
}

// buildRESTConfig returns a *rest.Config from the given kubeconfig path,
// falling back to in-cluster config when kubeconfig is empty.
func buildRESTConfig(kubeconfig string) (*rest.Config, error) {
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

func boolPtr(b bool) *bool { return &b }
