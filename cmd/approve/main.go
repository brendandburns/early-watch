// Package main is the entry point for the EarlyWatch approve tool.
//
// The approve tool signs a Kubernetes resource's canonical path with a local
// RSA private key and writes the resulting signature as an annotation on the
// resource.  This annotation is later verified by the ApprovalCheck rule in
// the EarlyWatch admission webhook.
//
// Usage:
//
//	approve \
//	  --private-key /path/to/private-key.pem \
//	  --group "" \
//	  --version v1 \
//	  --resource configmaps \
//	  --namespace default \
//	  --name my-config \
//	  [--annotation-key earlywatch.io/approved] \
//	  [--kubeconfig ~/.kube/config]
package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	ewwebhook "github.com/brendandburns/early-watch/pkg/webhook"
)

func main() {
	var (
		privateKeyPath string
		kubeconfig     string
		group          string
		version        string
		resource       string
		namespace      string
		name           string
		annotationKey  string
	)

	flag.StringVar(&privateKeyPath, "private-key", "", "Path to the PEM-encoded RSA private key file (required).")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	flag.StringVar(&group, "group", "", "API group of the resource (e.g. \"\" for core, \"apps\" for Deployments).")
	flag.StringVar(&version, "version", "v1", "API version of the resource (e.g. \"v1\", \"v1beta1\").")
	flag.StringVar(&resource, "resource", "", "Plural resource name (e.g. \"configmaps\", \"deployments\") (required).")
	flag.StringVar(&namespace, "namespace", "", "Namespace of the resource. Leave empty for cluster-scoped resources.")
	flag.StringVar(&name, "name", "", "Name of the resource (required).")
	flag.StringVar(&annotationKey, "annotation-key", "earlywatch.io/approved",
		"Annotation key to write the signature to.")

	flag.Parse()

	if privateKeyPath == "" {
		fmt.Fprintln(os.Stderr, "error: --private-key is required")
		flag.Usage()
		os.Exit(1)
	}
	if resource == "" {
		fmt.Fprintln(os.Stderr, "error: --resource is required")
		flag.Usage()
		os.Exit(1)
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "error: --name is required")
		flag.Usage()
		os.Exit(1)
	}

	// Load the private key.
	privKey, err := loadRSAPrivateKey(privateKeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading private key: %v\n", err)
		os.Exit(1)
	}

	// Compute the canonical resource path.
	path := ewwebhook.ResourcePath(group, version, resource, namespace, name)

	// Sign the path with RSA-PSS SHA-256.
	sig, err := signResourcePath(privKey, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error signing resource path: %v\n", err)
		os.Exit(1)
	}

	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Build the Kubernetes dynamic client.
	dynClient, err := buildDynamicClient(kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	// Patch the annotation onto the resource.
	gvr := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}

	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, annotationKey, sigB64)

	var resourceClient dynamic.ResourceInterface
	if namespace != "" {
		resourceClient = dynClient.Resource(gvr).Namespace(namespace)
	} else {
		resourceClient = dynClient.Resource(gvr)
	}

	if _, err := resourceClient.Patch(
		context.Background(),
		name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	); err != nil {
		fmt.Fprintf(os.Stderr, "error annotating resource: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully annotated %s %s/%s with approval signature for path %q\n",
		resource, namespace, name, path)
}

// loadRSAPrivateKey reads and parses a PEM-encoded RSA private key from disk.
func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %q: %w", path, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %q", path)
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
			return nil, fmt.Errorf("key in %q is not an RSA private key", path)
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q in %q; expected RSA PRIVATE KEY or PRIVATE KEY", block.Type, path)
	}
}

// signResourcePath computes the RSA-PSS SHA-256 signature of the canonical
// resource path string.
func signResourcePath(key *rsa.PrivateKey, path string) ([]byte, error) {
	digest := sha256.Sum256([]byte(path))
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		return nil, fmt.Errorf("RSA-PSS signing: %w", err)
	}
	return sig, nil
}

// buildDynamicClient creates a Kubernetes dynamic client from the given
// kubeconfig path.  When kubeconfig is empty it falls back to in-cluster
// configuration.
func buildDynamicClient(kubeconfig string) (dynamic.Interface, error) {
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
