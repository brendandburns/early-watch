// Package install provides the core logic for applying and removing the
// EarlyWatch infrastructure manifests (CRD, RBAC, webhook) on a Kubernetes
// cluster.
package install

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"

	internalapply "github.com/brendandburns/early-watch/pkg/internal/apply"
)

// UninstallOptions holds the parameters for an uninstall operation.
type UninstallOptions struct {
	// Kubeconfig is the path to a kubeconfig file. Falls back to in-cluster
	// config when empty.
	Kubeconfig string
	// Namespace is the Kubernetes namespace that EarlyWatch was installed into.
	// Defaults to defaultNamespace ("early-watch-system") when empty.
	Namespace string
}

// Uninstall removes all EarlyWatch infrastructure resources from the cluster
// described by opts.Kubeconfig, printing progress to stdout.  Resources are
// deleted in reverse manifest order so that higher-level objects (e.g. the
// ValidatingWebhookConfiguration) are removed before lower-level ones (e.g.
// the CRD), minimizing the window during which the webhook could intercept
// its own teardown.  Resources that no longer exist are silently skipped.
func Uninstall(opts UninstallOptions) error {
	if opts.Namespace == "" {
		opts.Namespace = defaultNamespace
	}

	cfg, err := internalapply.BuildRESTConfig(opts.Kubeconfig)
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

	entries, err := fs.ReadDir(manifestsFS, "manifests")
	if err != nil {
		return fmt.Errorf("reading embedded manifests directory: %w", err)
	}

	// Collect every resource reference in forward manifest order, then delete
	// them in reverse so teardown proceeds cleanly.
	type resourceRef struct {
		obj     *unstructured.Unstructured
		mapping *meta.RESTMapping
	}

	var resources []resourceRef

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := "manifests/" + entry.Name()
		data, err := manifestsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded manifest %q: %w", path, err)
		}

		// Substitute namespace placeholder so resource names resolve correctly.
		if opts.Namespace != defaultNamespace {
			data = bytes.ReplaceAll(data, []byte(defaultNamespace), []byte(opts.Namespace))
		}

		decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
		for {
			rawObj := make(map[string]interface{})
			if err := decoder.Decode(&rawObj); err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("decoding manifest %q: %w", entry.Name(), err)
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
				// Skip only when the kind is genuinely not registered in the
				// cluster's discovery so that partial installations can still
				// be cleaned up.  Any other error (e.g., auth or network)
				// is surfaced to the caller.
				if meta.IsNoMatchError(err) {
					fmt.Printf("Skipping %s %q (resource type not available in cluster)\n", gvk.Kind, obj.GetName())
					continue
				}
				return fmt.Errorf("resolving REST mapping for %s %q: %w", gvk.Kind, internalapply.ResourceDisplayName(obj), err)
			}

			resources = append(resources, resourceRef{obj: obj, mapping: mapping})
		}
	}

	// Delete in reverse order.
	for i := len(resources) - 1; i >= 0; i-- {
		ref := resources[i]
		obj := ref.obj
		mapping := ref.mapping

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

		if err := resourceClient.Delete(ctx, obj.GetName(), metav1.DeleteOptions{}); err != nil {
			if kerrors.IsNotFound(err) {
				fmt.Printf("Skipped %s %q (not found)\n", obj.GetKind(), internalapply.ResourceDisplayName(obj))
				continue
			}
			return fmt.Errorf("deleting %s %q: %w", obj.GetKind(), internalapply.ResourceDisplayName(obj), err)
		}

		fmt.Printf("Deleted %s %q\n", obj.GetKind(), internalapply.ResourceDisplayName(obj))
	}

	// Delete the namespace last (it was created programmatically by install,
	// not from a manifest).  Resources inside the namespace are already gone
	// at this point, so the namespace should terminate quickly.
	if err := dynClient.Resource(namespaceGVR).Delete(ctx, opts.Namespace, metav1.DeleteOptions{}); err != nil {
		if !kerrors.IsNotFound(err) {
			return fmt.Errorf("deleting namespace %q: %w", opts.Namespace, err)
		}
		fmt.Printf("Skipped Namespace %q (not found)\n", opts.Namespace)
	} else {
		fmt.Printf("Deleted Namespace %q\n", opts.Namespace)
	}

	fmt.Println("EarlyWatch uninstallation complete.")
	return nil
}
