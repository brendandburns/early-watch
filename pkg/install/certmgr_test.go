package install

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestWebhookDNSNames verifies that the expected SAN entries are returned for
// a given service and namespace.
func TestWebhookDNSNames(t *testing.T) {
	names := webhookDNSNames("my-svc", "my-ns")
	if len(names) != 2 {
		t.Fatalf("expected 2 DNS names, got %d", len(names))
	}
	if names[0] != "my-svc.my-ns.svc" {
		t.Errorf("names[0] = %q, want %q", names[0], "my-svc.my-ns.svc")
	}
	if names[1] != "my-svc.my-ns.svc.cluster.local" {
		t.Errorf("names[1] = %q, want %q", names[1], "my-svc.my-ns.svc.cluster.local")
	}
}

// TestGenerateSelfSignedCert verifies that generateSelfSignedCert returns
// valid PEM-encoded CA cert, serving cert, and private key for the given DNS
// names.
func TestGenerateSelfSignedCert(t *testing.T) {
	dnsNames := webhookDNSNames("my-svc", "my-ns")
	caCertPEM, certPEM, keyPEM, err := generateSelfSignedCert(dnsNames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caCertPEM) == 0 {
		t.Error("expected non-empty CA cert PEM")
	}
	if len(certPEM) == 0 {
		t.Error("expected non-empty cert PEM")
	}
	if len(keyPEM) == 0 {
		t.Error("expected non-empty key PEM")
	}

	// Verify that the serving cert is signed by the CA.
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		t.Fatal("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		t.Fatal("failed to decode serving cert PEM")
	}
	servingCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse serving cert: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := servingCert.Verify(x509.VerifyOptions{
		DNSName: dnsNames[0],
		Roots:   pool,
	}); err != nil {
		t.Errorf("serving cert does not verify against CA: %v", err)
	}
}

// TestUpsertWebhookSecret_Create verifies that upsertWebhookSecret creates a
// new Secret when none exists.
func TestUpsertWebhookSecret_Create(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	certPEM := []byte("cert")
	keyPEM := []byte("key")
	if err := upsertWebhookSecret(context.Background(), clientset, "test-ns", certPEM, keyPEM); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	secret, err := clientset.CoreV1().Secrets("test-ns").Get(context.Background(), webhookSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Secret to exist: %v", err)
	}
	if string(secret.Data[corev1.TLSCertKey]) != string(certPEM) {
		t.Errorf("tls.crt = %q, want %q", secret.Data[corev1.TLSCertKey], certPEM)
	}
	if string(secret.Data[corev1.TLSPrivateKeyKey]) != string(keyPEM) {
		t.Errorf("tls.key = %q, want %q", secret.Data[corev1.TLSPrivateKeyKey], keyPEM)
	}
}

// TestUpsertWebhookSecret_Update verifies that upsertWebhookSecret updates an
// existing Secret when one is already present.
func TestUpsertWebhookSecret_Update(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSecretName, Namespace: "test-ns"},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte("old-cert"),
			corev1.TLSPrivateKeyKey: []byte("old-key"),
		},
	}
	clientset := fake.NewSimpleClientset(existing)

	newCert := []byte("new-cert")
	newKey := []byte("new-key")
	if err := upsertWebhookSecret(context.Background(), clientset, "test-ns", newCert, newKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	secret, err := clientset.CoreV1().Secrets("test-ns").Get(context.Background(), webhookSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Secret to exist: %v", err)
	}
	if string(secret.Data[corev1.TLSCertKey]) != string(newCert) {
		t.Errorf("tls.crt = %q, want %q", secret.Data[corev1.TLSCertKey], newCert)
	}
}

// TestPatchWebhookCABundle_Happy verifies that patchWebhookCABundle correctly
// sets the caBundle on all webhook entries.
func TestPatchWebhookCABundle_Happy(t *testing.T) {
	sideEffectsNone := admissionv1.SideEffectClassNone
	webhookCfg := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: webhookConfigName},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name:                    "validate.earlywatch.io",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             &sideEffectsNone,
				ClientConfig:            admissionv1.WebhookClientConfig{},
			},
		},
	}
	clientset := fake.NewSimpleClientset(webhookCfg)

	caBundle := []byte("ca-data")
	if err := patchWebhookCABundle(context.Background(), clientset, caBundle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.Background(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected ValidatingWebhookConfiguration to exist: %v", err)
	}
	if len(updated.Webhooks) == 0 {
		t.Fatal("expected at least one webhook entry")
	}
	if string(updated.Webhooks[0].ClientConfig.CABundle) != string(caBundle) {
		t.Errorf("caBundle = %q, want %q", updated.Webhooks[0].ClientConfig.CABundle, caBundle)
	}
}

// TestPatchWebhookCABundle_NotFound verifies that an error is returned when
// the ValidatingWebhookConfiguration does not exist.
func TestPatchWebhookCABundle_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	if err := patchWebhookCABundle(context.Background(), clientset, []byte("ca")); err == nil {
		t.Fatal("expected an error for missing webhook config, got nil")
	}
}

// TestProvisionWebhookCert_HappyPath verifies the end-to-end happy-path of
// provisionWebhookCertWithClient using a fake Kubernetes client.
func TestProvisionWebhookCert_HappyPath(t *testing.T) {
	const ns = "test-ns"

	sideEffectsNone := admissionv1.SideEffectClassNone
	webhookCfg := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: webhookConfigName},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name:                    "validate.earlywatch.io",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             &sideEffectsNone,
				ClientConfig:            admissionv1.WebhookClientConfig{},
			},
		},
	}

	clientset := fake.NewSimpleClientset(webhookCfg)

	if err := provisionWebhookCertWithClient(context.Background(), clientset, ns); err != nil {
		t.Fatalf("provisionWebhookCertWithClient returned error: %v", err)
	}

	// Verify the Secret was created with TLS data.
	secret, err := clientset.CoreV1().Secrets(ns).Get(context.Background(), webhookSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected webhook Secret: %v", err)
	}
	if len(secret.Data[corev1.TLSCertKey]) == 0 {
		t.Error("expected tls.crt to be set in Secret")
	}
	if len(secret.Data[corev1.TLSPrivateKeyKey]) == 0 {
		t.Error("expected tls.key to be set in Secret")
	}

	// Verify the caBundle was patched into the ValidatingWebhookConfiguration.
	vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.Background(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected ValidatingWebhookConfiguration: %v", err)
	}
	if len(vwc.Webhooks) == 0 || len(vwc.Webhooks[0].ClientConfig.CABundle) == 0 {
		t.Error("expected caBundle to be set in ValidatingWebhookConfiguration")
	}
}
