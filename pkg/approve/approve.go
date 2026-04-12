// Package approve provides the core logic for signing a Kubernetes resource's
// canonical path with an RSA private key and writing the resulting signature
// as an annotation on the resource.
package approve

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

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
