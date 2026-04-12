package install

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestGenerateWebhookCerts verifies that generateWebhookCerts produces a
// valid CA certificate and a server TLS certificate that is signed by that CA
// and contains the expected DNS SANs.
func TestGenerateWebhookCerts(t *testing.T) {
	certs, err := generateWebhookCerts()
	if err != nil {
		t.Fatalf("generateWebhookCerts: %v", err)
	}

	if len(certs.caCert) == 0 {
		t.Fatal("caCert is empty")
	}
	if len(certs.tlsCert) == 0 {
		t.Fatal("tlsCert is empty")
	}
	if len(certs.tlsKey) == 0 {
		t.Fatal("tlsKey is empty")
	}

	// Parse CA cert.
	caBlock, _ := pem.Decode(certs.caCert)
	if caBlock == nil {
		t.Fatal("could not decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}
	if !caCert.IsCA {
		t.Error("CA cert IsCA = false, want true")
	}

	// Parse server cert.
	serverBlock, _ := pem.Decode(certs.tlsCert)
	if serverBlock == nil {
		t.Fatal("could not decode server cert PEM")
	}
	serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing server cert: %v", err)
	}

	// Verify server cert is signed by the CA.
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)
	if _, err := serverCert.Verify(x509.VerifyOptions{
		Roots:     caPool,
		DNSName:   webhookServiceName + "." + systemNamespace + ".svc",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("verifying server cert: %v", err)
	}

	// Verify all expected SANs are present.
	for _, san := range webhookDNSNames() {
		found := false
		for _, dnsName := range serverCert.DNSNames {
			if dnsName == san {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected SAN %q not found in server cert; got %v", san, serverCert.DNSNames)
		}
	}

	// Verify the key and cert form a valid TLS pair.
	if _, err := tls.X509KeyPair(certs.tlsCert, certs.tlsKey); err != nil {
		t.Errorf("tlsCert and tlsKey do not form a valid TLS key pair: %v", err)
	}
}

// TestInjectCABundle verifies that injectCABundle correctly sets the caBundle
// field on every webhook entry within an unstructured VWC object.
func TestInjectCABundle(t *testing.T) {
	// Build a minimal unstructured VWC with two webhooks.
	obj := buildUnstructuredVWC()

	caBundle := []byte("fake-ca-bundle")
	injectCABundle(obj, caBundle)

	wantB64 := base64.StdEncoding.EncodeToString(caBundle)

	webhooks, _, _ := unstructured.NestedSlice(obj.Object, "webhooks")
	for i, entry := range webhooks {
		wh, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("webhook[%d] is not a map", i)
		}
		cc, ok := wh["clientConfig"].(map[string]interface{})
		if !ok {
			t.Fatalf("webhook[%d].clientConfig is not a map", i)
		}
		got, ok := cc["caBundle"].(string)
		if !ok {
			t.Fatalf("webhook[%d].clientConfig.caBundle is not a string", i)
		}
		if got != wantB64 {
			t.Errorf("webhook[%d].clientConfig.caBundle = %q, want %q", i, got, wantB64)
		}
	}
}

// buildUnstructuredVWC returns a minimal unstructured ValidatingWebhookConfiguration.
func buildUnstructuredVWC() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "ValidatingWebhookConfiguration",
			"metadata":   map[string]interface{}{"name": "test-vwc"},
			"webhooks": []interface{}{
				map[string]interface{}{
					"name": "webhook1.example.com",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name":      "svc",
							"namespace": "default",
						},
					},
				},
				map[string]interface{}{
					"name": "webhook2.example.com",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name":      "svc2",
							"namespace": "default",
						},
					},
				},
			},
		},
	}
}
