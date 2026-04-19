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
const defaultWebhookImage = "early-watch:v0.0.1"

// defaultAuditMonitorImage is the container image used by the audit-monitor
// Deployment when no override is provided.
const defaultAuditMonitorImage = "early-watch-audit-monitor:v0.0.1"

// defaultNamespace is the Kubernetes namespace used for EarlyWatch resources
// when no override is provided via Options.Namespace.
const defaultNamespace = "early-watch-system"

// namespaceGVR is the GroupVersionResource for Namespace objects.
var namespaceGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}

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
	// ManualTouchInstall, when true, additionally installs the audit-monitor
	// CRDs, RBAC, Deployment, and Service required for manual touch monitoring.
	// Defaults to false.
	ManualTouchInstall bool
	// AuditMonitorImage overrides the container image for the audit-monitor
	// Deployment. Only used when ManualTouchInstall is true.
	// Defaults to defaultAuditMonitorImage when empty.
	AuditMonitorImage string
	// APIServerCertSigning, when true, provisions the webhook TLS
	// certificate using the Kubernetes built-in
	// CertificateSigningRequest API and stores the result in a Secret.
	// When false, the install step relies on cert-manager (or another
	// external CA) to populate the Secret and inject the caBundle into the
	// ValidatingWebhookConfiguration. The CLI may default this to true, but
	// library callers using Options{} must set it explicitly.
	APIServerCertSigning bool
	// ImagePullSecret is the name of an existing Kubernetes Secret of type
	// kubernetes.io/dockerconfigjson that is added as an imagePullSecret to
	// every Deployment managed by the install. Leave empty when the cluster
	// can pull images without authentication.
	ImagePullSecret string
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
	if opts.AuditMonitorImage == "" {
		opts.AuditMonitorImage = defaultAuditMonitorImage
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

	// Apply core manifests in order: CRD first, then RBAC, then webhook resources.
	replacements := map[string]string{
		defaultWebhookImage: opts.Image,
		defaultNamespace:    opts.Namespace,
	}

	// Build the manifest mutator. It always stamps the managed-by annotation,
	// optionally strips the cert-manager annotation from the
	// ValidatingWebhookConfiguration, and optionally injects an
	// imagePullSecret into every Deployment.
	manifestMutator := func(obj *unstructured.Unstructured) {
		stampManagedBy(obj)
		if opts.APIServerCertSigning && obj.GetKind() == "ValidatingWebhookConfiguration" {
			removeCertManagerAnnotation(obj)
		}
		if opts.ImagePullSecret != "" && obj.GetKind() == "Deployment" {
			injectImagePullSecret(obj, opts.ImagePullSecret)
		}
	}

	if err := applyManifestDir(ctx, dynClient, mapper, "manifests", replacements, manifestMutator); err != nil {
		return err
	}

	// Optionally apply manual touch monitoring manifests.
	if opts.ManualTouchInstall {
		mtReplacements := map[string]string{
			defaultAuditMonitorImage: opts.AuditMonitorImage,
			defaultNamespace:         opts.Namespace,
		}
		mtMutator := func(obj *unstructured.Unstructured) {
			stampManagedBy(obj)
			if opts.ImagePullSecret != "" && obj.GetKind() == "Deployment" {
				injectImagePullSecret(obj, opts.ImagePullSecret)
			}
		}
		if err := applyManifestDir(ctx, dynClient, mapper, "manifests/manual-touch", mtReplacements, mtMutator); err != nil {
			return err
		}
	}

	// Provision the webhook TLS certificate using a self-signed CA and inject
	// the CA certificate into the ValidatingWebhookConfiguration caBundle so
	// the API server can verify the webhook server's certificate.
	if opts.APIServerCertSigning {
		fmt.Println("Provisioning webhook TLS certificate via self-signed CA...")
		if err := provisionWebhookCert(ctx, cfg, opts.Namespace); err != nil {
			return fmt.Errorf("provisioning webhook TLS certificate: %w", err)
		}
	}

	fmt.Println("EarlyWatch installation complete.")
	return nil
}

// applyManifestDir reads all manifest files from dir within the embedded FS,
// applies the given string replacements, and SSA-applies each manifest to the
// cluster. The replacements map is applied only when the old value differs from
// the new value. mutator, when non-nil, is called on each decoded object before
// it is applied; when nil a default mutator that stamps the managed-by
// annotation is used.
func applyManifestDir(ctx context.Context, dynClient dynamic.Interface, mapper *restmapper.DeferredDiscoveryRESTMapper, dir string, replacements map[string]string, mutator func(*unstructured.Unstructured)) error {
	if mutator == nil {
		mutator = stampManagedBy
	}

	entries, err := fs.ReadDir(manifestsFS, dir)
	if err != nil {
		return fmt.Errorf("reading embedded manifests directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := dir + "/" + entry.Name()
		data, err := manifestsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded manifest %q: %w", path, err)
		}

		for oldVal, newVal := range replacements {
			if oldVal != newVal {
				data = bytes.ReplaceAll(data, []byte(oldVal), []byte(newVal))
			}
		}

		if err := internalapply.Manifest(ctx, dynClient, mapper, data, entry.Name(), mutator); err != nil {
			return err
		}
	}
	return nil
}

// removeCertManagerAnnotation deletes the cert-manager CA-injection annotation
// from obj so that cert-manager does not overwrite the caBundle that the
// API-server cert signing path manages.
func removeCertManagerAnnotation(obj *unstructured.Unstructured) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return
	}
	delete(annotations, "cert-manager.io/inject-ca-from")
	obj.SetAnnotations(annotations)
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

// stampManagedBy adds the created-by annotation to obj so that resources
// applied by "watchctl install" can be identified later.
func stampManagedBy(obj *unstructured.Unstructured) {
	injectAnnotation(obj, CreatedByAnnotation, managedByValue)
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

// injectImagePullSecret adds secretName to the imagePullSecrets list of the
// pod template in obj (which must be a Deployment). If the secret is already
// present the list is left unchanged.
func injectImagePullSecret(obj *unstructured.Unstructured, secretName string) {
	existing, _, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "imagePullSecrets")
	if err != nil {
		existing = nil
	}
	for _, entry := range existing {
		if m, ok := entry.(map[string]interface{}); ok {
			if m["name"] == secretName {
				return
			}
		}
	}
	existing = append(existing, map[string]interface{}{"name": secretName})
	if err := unstructured.SetNestedSlice(obj.Object, existing, "spec", "template", "spec", "imagePullSecrets"); err != nil {
		fmt.Printf("warning: could not set imagePullSecrets on %s %q: %v\n", obj.GetKind(), obj.GetName(), err)
	}
}
