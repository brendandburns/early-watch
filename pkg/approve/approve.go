// Package approve provides the core logic for signing a Kubernetes resource's
// canonical path with an RSA private key and writing the resulting signature
// as an annotation on the resource.
package approve

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	internalpatch "github.com/brendandburns/early-watch/pkg/internal/patch"
	ewwebhook "github.com/brendandburns/early-watch/pkg/webhook"
)

// Options holds the parameters for an approve operation.
type Options struct {
	PrivateKeyPath string
	Kubeconfig     string
	Group          string
	Version        string
	Resource       string
	Namespace      string
	Name           string
	AnnotationKey  string
}

// Run executes the approve logic: it signs the resource path and patches the
// approval annotation onto the resource in the cluster.
func Run(opts Options) error {
	privKey, err := LoadRSAPrivateKey(opts.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("loading private key: %w", err)
	}

	path := ewwebhook.ResourcePath(opts.Group, opts.Version, opts.Resource, opts.Namespace, opts.Name)

	sig, err := SignResourcePath(privKey, path)
	if err != nil {
		return fmt.Errorf("signing resource path: %w", err)
	}

	sigB64 := base64.StdEncoding.EncodeToString(sig)

	dynClient, err := BuildDynamicClient(opts.Kubeconfig)
	if err != nil {
		return fmt.Errorf("building Kubernetes client: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    opts.Group,
		Version:  opts.Version,
		Resource: opts.Resource,
	}

	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, opts.AnnotationKey, sigB64)

	var resourceClient dynamic.ResourceInterface
	if opts.Namespace != "" {
		resourceClient = dynClient.Resource(gvr).Namespace(opts.Namespace)
	} else {
		resourceClient = dynClient.Resource(gvr)
	}

	if _, err := resourceClient.Patch(
		context.Background(),
		opts.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("annotating resource: %w", err)
	}

	location := opts.Name
	if opts.Namespace != "" {
		location = opts.Namespace + "/" + opts.Name
	}
	fmt.Printf("Successfully annotated %s %s with approval signature for path %q\n",
		opts.Resource, location, path)

	return nil
}

// LoadRSAPrivateKey reads and parses a PEM-encoded RSA private key from disk.
// It supports both PKCS#1 ("RSA PRIVATE KEY") and PKCS#8 ("PRIVATE KEY") formats.
func LoadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %q: %w", path, err)
	}
	return ParseRSAPrivateKey(data)
}

// ParseRSAPrivateKey parses a PEM-encoded RSA private key from the given bytes.
// It supports both PKCS#1 ("RSA PRIVATE KEY") and PKCS#8 ("PRIVATE KEY") formats.
func ParseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in key data")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		// PKCS#1
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing PKCS#1 private key: %w", err)
		}
		return key, nil
	case "PRIVATE KEY":
		// PKCS#8
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing PKCS#8 private key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not an RSA private key")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q; expected RSA PRIVATE KEY or PRIVATE KEY", block.Type)
	}
}

// SignResourcePath computes the RSA-PSS SHA-256 signature of the canonical
// resource path string.
func SignResourcePath(key *rsa.PrivateKey, path string) ([]byte, error) {
	digest := sha256.Sum256([]byte(path))
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		return nil, fmt.Errorf("RSA-PSS signing: %w", err)
	}
	return sig, nil
}

// BuildDynamicClient creates a Kubernetes dynamic client from the given
// kubeconfig path. When kubeconfig is empty it falls back to in-cluster
// configuration.
func BuildDynamicClient(kubeconfig string) (dynamic.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building REST config: %w", err)
	}
	return dynamic.NewForConfig(cfg)
}

// DefaultChangeApprovalAnnotation is the annotation key used for UPDATE
// (change) approvals when ChangeOptions.AnnotationKey is empty.
const DefaultChangeApprovalAnnotation = "earlywatch.io/change-approved"

// SignPatch computes the RSA-PSS SHA-256 signature of the given canonical
// patch JSON bytes.  The patch must already be in its canonical (normalized)
// form so that the signature can be reproduced deterministically.
func SignPatch(key *rsa.PrivateKey, patchJSON []byte) ([]byte, error) {
	digest := sha256.Sum256(patchJSON)
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		return nil, fmt.Errorf("RSA-PSS signing: %w", err)
	}
	return sig, nil
}

// ChangeOptions holds the parameters for a change-approval (UPDATE) operation.
type ChangeOptions struct {
	// PrivateKeyPath is the path to the PEM-encoded RSA private key file.
	PrivateKeyPath string
	// Kubeconfig is the path to a kubeconfig file.  Falls back to in-cluster
	// config when empty.
	Kubeconfig string
	// Group, Version, Resource, Namespace, Name identify the resource to approve.
	Group     string
	Version   string
	Resource  string
	Namespace string
	Name      string
	// AnnotationKey is the annotation to write the change-approval signature
	// to on the existing resource.  Defaults to "earlywatch.io/change-approved".
	AnnotationKey string
	// NewResourceFile is the path to a YAML or JSON file containing the
	// desired new state of the resource.
	NewResourceFile string
}

// RunChange approves a modification to a Kubernetes resource.  It:
//  1. Fetches the current resource from the cluster.
//  2. Reads the desired new state from NewResourceFile (YAML or JSON).
//  3. Computes the normalised JSON merge patch between the two states.
//  4. Signs the SHA-256 hash of the canonical patch JSON.
//  5. Writes the signature as a change-approval annotation on the existing
//     resource in the cluster.
//
// The admission webhook will later verify this annotation when the UPDATE is
// submitted, ensuring the actual change matches the pre-approved patch.
func RunChange(opts ChangeOptions) error {
	privKey, err := LoadRSAPrivateKey(opts.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("loading private key: %w", err)
	}

	dynClient, err := BuildDynamicClient(opts.Kubeconfig)
	if err != nil {
		return fmt.Errorf("building Kubernetes client: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    opts.Group,
		Version:  opts.Version,
		Resource: opts.Resource,
	}

	var resourceClient dynamic.ResourceInterface
	if opts.Namespace != "" {
		resourceClient = dynClient.Resource(gvr).Namespace(opts.Namespace)
	} else {
		resourceClient = dynClient.Resource(gvr)
	}

	// Fetch the current (old) resource from the cluster.
	current, err := resourceClient.Get(context.Background(), opts.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fetching current resource: %w", err)
	}

	oldJSON, err := json.Marshal(current.Object)
	if err != nil {
		return fmt.Errorf("marshaling current resource: %w", err)
	}

	// Read the new resource from the file (YAML or JSON).
	fileData, err := os.ReadFile(opts.NewResourceFile)
	if err != nil {
		return fmt.Errorf("reading new resource file %q: %w", opts.NewResourceFile, err)
	}

	var newObj map[string]interface{}
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(fileData), 4096)
	if err := decoder.Decode(&newObj); err != nil {
		return fmt.Errorf("decoding new resource file %q: %w", opts.NewResourceFile, err)
	}

	newJSON, err := json.Marshal(newObj)
	if err != nil {
		return fmt.Errorf("marshaling new resource: %w", err)
	}

	annotationKey := opts.AnnotationKey
	if annotationKey == "" {
		annotationKey = DefaultChangeApprovalAnnotation
	}

	// Compute the normalised merge patch (strips server-managed fields and
	// the change-approval annotation itself from both sides).
	patchJSON, err := internalpatch.ComputeNormalizedMergePatch(oldJSON, newJSON, []string{annotationKey})
	if err != nil {
		return fmt.Errorf("computing merge patch: %w", err)
	}

	// Sign the patch.
	sig, err := SignPatch(privKey, patchJSON)
	if err != nil {
		return fmt.Errorf("signing patch: %w", err)
	}

	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Annotate the existing resource with the change-approval signature so
	// that the admission webhook can verify it when the UPDATE is applied.
	mergePatch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, annotationKey, sigB64)
	if _, err := resourceClient.Patch(
		context.Background(),
		opts.Name,
		types.MergePatchType,
		[]byte(mergePatch),
		metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("annotating resource: %w", err)
	}

	location := opts.Name
	if opts.Namespace != "" {
		location = opts.Namespace + "/" + opts.Name
	}
	fmt.Printf("Successfully annotated %s %s with change-approval signature\n",
		opts.Resource, location)

	return nil
}
