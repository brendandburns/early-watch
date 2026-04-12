// Package install provides the core logic for applying the EarlyWatch
// infrastructure manifests (CRD, RBAC, webhook) onto a Kubernetes cluster.
package install

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

		if err := internalapply.ApplyManifest(ctx, dynClient, mapper, data, entry.Name(), func(obj *unstructured.Unstructured) {
			injectAnnotation(obj, CreatedByAnnotation, managedByValue)
		}); err != nil {
			return err
		}
	}

	fmt.Println("EarlyWatch installation complete.")
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
