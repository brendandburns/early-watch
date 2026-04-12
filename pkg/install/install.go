// Package install provides the core logic for applying the EarlyWatch
// infrastructure manifests (CRD, RBAC, webhook) onto a Kubernetes cluster.
package install

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"

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

//go:embed manifests
var manifestsFS embed.FS

// defaultWebhookImage is the container image used by the webhook Deployment
// when no override is provided.
const defaultWebhookImage = "early-watch:latest"

// fieldManager is the field manager name used for Server-Side Apply.
const fieldManager = "watchctl"

// CreatedByAnnotation is the annotation key written onto every resource
// applied by "watchctl install". Its value identifies the tool that created
// the resource, making it easy to list or delete all managed resources later.
const CreatedByAnnotation = "earlywatch.io/created-by"

// managedByValue is the value written to ManagedByAnnotation.
const managedByValue = "watchctl"

// Options holds the parameters for an install operation.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Falls back to in-cluster
	// config when empty.
	Kubeconfig string
	// Image overrides the container image for the webhook Deployment.
	// Defaults to defaultWebhookImage when empty.
	Image string
}

// Run applies all EarlyWatch infrastructure manifests to the cluster
// described by opts.Kubeconfig, printing progress to stdout.
func Run(opts Options) error {
	if opts.Image == "" {
		opts.Image = defaultWebhookImage
	}

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

	// Apply manifests in order: CRD first, then RBAC, then webhook resources.
	entries, err := fs.ReadDir(manifestsFS, "manifests")
	if err != nil {
		return fmt.Errorf("reading embedded manifests directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := "manifests/" + entry.Name()
		data, err := manifestsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded manifest %q: %w", path, err)
		}

		// Optionally override the webhook image inside the deployment manifest.
		if opts.Image != defaultWebhookImage {
			data = bytes.ReplaceAll(data, []byte(defaultWebhookImage), []byte(opts.Image))
		}

		if err := applyManifest(ctx, dynClient, mapper, data, entry.Name()); err != nil {
			return err
		}
	}

	fmt.Println("EarlyWatch installation complete.")
	return nil
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

		// Stamp every resource with the managed-by annotation so it can be
		// identified and removed later.
		injectAnnotation(obj, CreatedByAnnotation, managedByValue)

		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("mapping resource %s %q: %w", gvk.Kind, obj.GetName(), err)
		}

		jsonData, err := json.Marshal(obj.Object)
		if err != nil {
			return fmt.Errorf("marshalling resource %s %q: %w", gvk.Kind, obj.GetName(), err)
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

// injectAnnotation adds or overwrites a single annotation on obj.
func injectAnnotation(obj *unstructured.Unstructured, key, value string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	obj.SetAnnotations(annotations)
}

func boolPtr(b bool) *bool { return &b }
