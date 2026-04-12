// Package install provides the core logic for applying the EarlyWatch
// infrastructure manifests (CRD, RBAC, webhook) onto a Kubernetes cluster.
package install

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// systemNamespace is the Kubernetes namespace where EarlyWatch is installed.
const systemNamespace = "early-watch-system"

// webhookServiceName is the name of the Service fronting the webhook Deployment.
const webhookServiceName = "early-watch-webhook-service"

// webhookTLSSecretName is the name of the Secret that stores the webhook TLS cert.
const webhookTLSSecretName = "early-watch-webhook-server-cert"

// objectModifier is an optional hook called on each parsed Kubernetes object
// before it is Server-Side Applied.  Implementations may mutate the object
// in-place (e.g. to inject dynamic values such as a CA bundle) and return an
// error to abort the apply if mutation fails.
type objectModifier func(*unstructured.Unstructured) error

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

	// Ensure the system namespace exists before applying any namespace-scoped
	// resources (e.g. the ServiceAccount in 01-rbac.yaml).
	if err := ensureNamespace(ctx, dynClient); err != nil {
		return err
	}

	// Generate TLS certificates for the webhook server.  The CA certificate is
	// later injected into the ValidatingWebhookConfiguration so the API server
	// can verify the webhook's TLS certificate without relying on cert-manager.
	certs, err := generateWebhookCerts()
	if err != nil {
		return fmt.Errorf("generating webhook TLS certificates: %w", err)
	}

	// Store the TLS certificate and key in a Secret so the webhook Deployment
	// can mount them.
	if err := applyTLSSecret(ctx, dynClient, certs); err != nil {
		return err
	}

	// Modifier that injects the generated CA bundle into the
	// ValidatingWebhookConfiguration before it is Server-Side Applied.
	modifier := func(obj *unstructured.Unstructured) error {
		if obj.GetKind() == "ValidatingWebhookConfiguration" {
			return injectCABundle(obj, certs.caCert)
		}
		return nil
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

		if err := applyManifest(ctx, dynClient, mapper, data, entry.Name(), modifier); err != nil {
			return err
		}
	}

	fmt.Println("EarlyWatch installation complete.")
	return nil
}

// applyManifest splits a potentially multi-document YAML file and applies
// each document to the cluster using Server-Side Apply.  If modifier is
// non-nil it is called on each parsed object before it is applied.
func applyManifest(
	ctx context.Context,
	dynClient dynamic.Interface,
	mapper meta.RESTMapper,
	data []byte,
	filename string,
	modifier objectModifier,
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

		// Allow the caller to mutate the object before it is applied.
		if modifier != nil {
			if err := modifier(obj); err != nil {
				return fmt.Errorf("modifying %s %q: %w", gvk.Kind, obj.GetName(), err)
			}
		}

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

// ensureNamespace applies a Server-Side Apply patch to create (or confirm the
// existence of) the EarlyWatch system namespace.  This must be called before
// any namespace-scoped resources (such as the ServiceAccount in the RBAC
// manifest) are applied.
func ensureNamespace(ctx context.Context, dynClient dynamic.Interface) error {
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": systemNamespace,
			"labels": map[string]interface{}{
				"app.kubernetes.io/name": "early-watch",
			},
			"annotations": map[string]interface{}{
				CreatedByAnnotation: managedByValue,
			},
		},
	}
	jsonData, err := json.Marshal(nsObj)
	if err != nil {
		return fmt.Errorf("marshalling Namespace %q: %w", systemNamespace, err)
	}
	if _, err := dynClient.Resource(nsGVR).Patch(
		ctx, systemNamespace, types.ApplyPatchType, jsonData,
		metav1.PatchOptions{FieldManager: fieldManager, Force: boolPtr(true)},
	); err != nil {
		return fmt.Errorf("ensuring Namespace %q: %w", systemNamespace, err)
	}
	fmt.Printf("Applied Namespace %q\n", systemNamespace)
	return nil
}

// applyTLSSecret creates or updates the Kubernetes Secret that holds the
// webhook server's TLS certificate and private key.
func applyTLSSecret(ctx context.Context, dynClient dynamic.Interface, certs *webhookCerts) error {
	secretGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}
	secretObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      webhookTLSSecretName,
			"namespace": systemNamespace,
			"annotations": map[string]interface{}{
				CreatedByAnnotation: managedByValue,
			},
		},
		"type": "kubernetes.io/tls",
		// Secret.data values are []byte; json.Marshal base64-encodes them.
		"data": map[string]interface{}{
			"tls.crt": certs.tlsCert,
			"tls.key": certs.tlsKey,
		},
	}
	jsonData, err := json.Marshal(secretObj)
	if err != nil {
		return fmt.Errorf("marshalling TLS Secret: %w", err)
	}
	if _, err := dynClient.Resource(secretGVR).Namespace(systemNamespace).Patch(
		ctx, webhookTLSSecretName, types.ApplyPatchType, jsonData,
		metav1.PatchOptions{FieldManager: fieldManager, Force: boolPtr(true)},
	); err != nil {
		return fmt.Errorf("applying TLS Secret %q: %w", webhookTLSSecretName, err)
	}
	fmt.Printf("Applied Secret %q\n", systemNamespace+"/"+webhookTLSSecretName)
	return nil
}

// injectCABundle sets the caBundle field on every webhook entry inside a
// ValidatingWebhookConfiguration unstructured object.  caCert must be a
// PEM-encoded CA certificate; it is base64-encoded before being stored so
// that json.Marshal produces the correct Kubernetes API wire format.
// Returns an error if the webhooks field is missing or cannot be updated.
func injectCABundle(obj *unstructured.Unstructured, caCert []byte) error {
	webhooks, found, err := unstructured.NestedSlice(obj.Object, "webhooks")
	if err != nil {
		return fmt.Errorf("reading webhooks field from %s %q: %w", obj.GetKind(), obj.GetName(), err)
	}
	if !found {
		return fmt.Errorf("%s %q has no webhooks field", obj.GetKind(), obj.GetName())
	}

	// base64-encode the PEM bytes: this is what the Kubernetes API stores in
	// the caBundle string field.
	caBundleB64 := base64.StdEncoding.EncodeToString(caCert)

	for i := range webhooks {
		wh, ok := webhooks[i].(map[string]interface{})
		if !ok {
			continue
		}
		cc, ok := wh["clientConfig"].(map[string]interface{})
		if !ok {
			cc = make(map[string]interface{})
		}
		cc["caBundle"] = caBundleB64
		wh["clientConfig"] = cc
		webhooks[i] = wh
	}
	if err := unstructured.SetNestedSlice(obj.Object, webhooks, "webhooks"); err != nil {
		return fmt.Errorf("setting webhooks on %s %q: %w", obj.GetKind(), obj.GetName(), err)
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }
