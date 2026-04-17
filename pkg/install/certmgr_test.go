package install

import (
	"context"
	"encoding/pem"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
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

// TestDeleteCSRIfExists_NotFound verifies that deleteCSRIfExists is a no-op
// when no CSR with the given name exists.
func TestDeleteCSRIfExists_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	if err := deleteCSRIfExists(context.Background(), clientset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteCSRIfExists_Exists verifies that an existing CSR is deleted.
func TestDeleteCSRIfExists_Exists(t *testing.T) {
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: csrName},
	}
	clientset := fake.NewSimpleClientset(csr)
	if err := deleteCSRIfExists(context.Background(), clientset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := clientset.CertificatesV1().CertificateSigningRequests().Get(context.Background(), csrName, metav1.GetOptions{})
	if err == nil {
		t.Error("expected CSR to be deleted, but it still exists")
	}
}

// TestWaitForCertificate_ImmediatelyAvailable verifies that waitForCertificate
// returns the certificate when it is already populated.
func TestWaitForCertificate_ImmediatelyAvailable(t *testing.T) {
	certPEM := []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n")
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: csrName},
		Status:     certificatesv1.CertificateSigningRequestStatus{Certificate: certPEM},
	}
	clientset := fake.NewSimpleClientset(csr)

	got, err := waitForCertificate(context.Background(), clientset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(certPEM) {
		t.Errorf("got cert %q, want %q", got, certPEM)
	}
}

// TestWaitForCertificate_Timeout verifies that waitForCertificate returns an
// error when the context is canceled before the certificate is available.
func TestWaitForCertificate_Timeout(t *testing.T) {
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: csrName},
		// Status.Certificate deliberately left empty.
	}
	clientset := fake.NewSimpleClientset(csr)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := waitForCertificate(ctx, clientset)
	if err == nil {
		t.Fatal("expected an error on timeout, got nil")
	}
}

// TestClusterCABundle_FromRESTConfig verifies that the CA is read from the
// REST config's TLSClientConfig when present.
func TestClusterCABundle_FromRESTConfig(t *testing.T) {
	caData := []byte("-----BEGIN CERTIFICATE-----\ncluster-ca\n-----END CERTIFICATE-----\n")
	cfg := &rest.Config{
		TLSClientConfig: rest.TLSClientConfig{CAData: caData},
	}
	clientset := fake.NewSimpleClientset()

	got, err := clusterCABundle(context.Background(), clientset, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(caData) {
		t.Errorf("got CA %q, want %q", got, caData)
	}
}

// TestClusterCABundle_FromConfigMap verifies that the CA is fetched from the
// kube-root-ca.crt ConfigMap when the REST config has no CA data.
func TestClusterCABundle_FromConfigMap(t *testing.T) {
	caData := "-----BEGIN CERTIFICATE-----\nroot-ca\n-----END CERTIFICATE-----\n"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-root-ca.crt", Namespace: "kube-system"},
		Data:       map[string]string{"ca.crt": caData},
	}
	clientset := fake.NewSimpleClientset(cm)
	cfg := &rest.Config{} // no CAData

	got, err := clusterCABundle(context.Background(), clientset, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != caData {
		t.Errorf("got CA %q, want %q", got, caData)
	}
}

// TestClusterCABundle_MissingConfigMap verifies that an error is returned when
// neither the REST config nor the ConfigMap provides a CA.
func TestClusterCABundle_MissingConfigMap(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	cfg := &rest.Config{}

	_, err := clusterCABundle(context.Background(), clientset, cfg)
	if err == nil {
		t.Fatal("expected an error, got nil")
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
// provisionWebhookCert using a fake Kubernetes client.  The test simulates the
// controller-manager signing the CSR by populating Status.Certificate after
// the approval update is recorded.
func TestProvisionWebhookCert_HappyPath(t *testing.T) {
	const ns = "test-ns"

	// Seed the fake cluster with the ValidatingWebhookConfiguration and the
	// kube-root-ca.crt ConfigMap that provisionWebhookCert depends on.
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
	caCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-root-ca.crt", Namespace: "kube-system"},
		Data:       map[string]string{"ca.crt": "ca-pem-data"},
	}

	clientset := fake.NewSimpleClientset(webhookCfg, caCM)

	// Intercept UpdateApproval calls: when a CSR approval is recorded, inject
	// a signed certificate into the CSR status so that waitForCertificate
	// returns immediately.
	fakeCert := fakeCertPEM(t)
	clientset.PrependReactor("update", "certificatesigningrequests", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ua := action.(k8stesting.UpdateAction)
		obj := ua.GetObject().(*certificatesv1.CertificateSigningRequest)
		// Simulate the controller-manager signing the cert.
		obj.Status.Certificate = fakeCert
		return false, obj, nil
	})

	cfg := &rest.Config{} // no CAData — will fall back to ConfigMap

	if err := provisionWebhookCertWithClient(context.Background(), clientset, cfg, ns); err != nil {
		t.Fatalf("provisionWebhookCert returned error: %v", err)
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

// fakeCertPEM returns a minimal PEM-encoded certificate for use in tests.
func fakeCertPEM(t *testing.T) []byte {
	t.Helper()
	block := &pem.Block{Type: "CERTIFICATE", Bytes: []byte("fake-cert-der")}
	return pem.EncodeToMemory(block)
}
