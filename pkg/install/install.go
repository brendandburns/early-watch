// Package install provides the core logic for applying the EarlyWatch
// infrastructure manifests (CRD, RBAC, webhook) onto a Kubernetes cluster.
package install

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"

	internalapply "github.com/brendandburns/early-watch/pkg/internal/apply"
)

//go:embed manifests
var manifestsFS embed.FS

// defaultWebhookImage is the container image used by the webhook Deployment
// when no override is provided.
const defaultWebhookImage = "early-watch:latest"

// defaultNamespace is the Kubernetes namespace used for EarlyWatch resources
// when no override is provided via Options.Namespace.
const defaultNamespace = "early-watch-system"

// namespaceGVR is the GroupVersionResource for Namespace objects.
var namespaceGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}

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
	// Namespace is the Kubernetes namespace to install EarlyWatch into.
	// Defaults to defaultNamespace ("early-watch-system") when empty.
	Namespace string
}

// Run applies all EarlyWatch infrastructure manifests to the cluster
// described by opts.Kubeconfig, printing progress to stdout.
func Run(opts Options) error {
	if opts.Image == "" {
		opts.Image = defaultWebhookImage
	}
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

	// Create the target namespace before applying any manifests so that
	// namespace-scoped resources (ServiceAccount, Deployment, Service) can
	// be created successfully.  An existing namespace is not an error.
	if err := ensureNamespace(ctx, dynClient, opts.Namespace); err != nil {
		return err
	}

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
		// Substitute namespace placeholder so all resource references point to
		// the configured namespace.
		if opts.Namespace != defaultNamespace {
			data = bytes.ReplaceAll(data, []byte(defaultNamespace), []byte(opts.Namespace))
		}

		if err := internalapply.ApplyManifest(ctx, dynClient, mapper, data, entry.Name(), func(obj *unstructured.Unstructured) {
			injectAnnotation(obj, CreatedByAnnotation, managedByValue)
		}); err != nil {
			return err
		}
	}

	fmt.Println("EarlyWatch installation complete.")
	return nil
}

// ensureNamespace creates the named namespace, stamped with the managed-by
// annotation.  If the namespace already exists the call is a no-op.
func ensureNamespace(ctx context.Context, dynClient dynamic.Interface, name string) error {
	ns := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": name,
			"labels": map[string]interface{}{
				"app.kubernetes.io/name": "early-watch",
			},
			"annotations": map[string]interface{}{
				CreatedByAnnotation: managedByValue,
			},
		},
	}}

	_, err := dynClient.Resource(namespaceGVR).Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			fmt.Printf("Namespace %q already exists, skipping creation\n", name)
			return nil
		}
		return fmt.Errorf("creating namespace %q: %w", name, err)
	}
	fmt.Printf("Created Namespace %q\n", name)
	return nil
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
