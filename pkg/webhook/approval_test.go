package webhook

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// generateTestKeyPair generates a 2048-bit RSA key pair for testing.
func generateTestKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("marshalling public key: %v", err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubDER,
	}))
	return privKey, pubPEM
}

// signPath signs the given path with RSA-PSS SHA-256 and returns the
// base64-encoded signature.
func signPath(t *testing.T, key *rsa.PrivateKey, path string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(path))
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		t.Fatalf("signing path: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// makeRequestWithAnnotations builds an admission.Request whose Object carries
// the given annotations.
func makeRequestWithAnnotations(
	operation admissionv1.Operation,
	group, version, resource, namespace, name string,
	annotations map[string]string,
) admission.Request {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: operation,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  version,
				Resource: resource,
			},
			Namespace: namespace,
			Name:      name,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// --- ResourcePath tests ---

func TestResourcePath_Namespaced(t *testing.T) {
	got := ResourcePath("apps", "v1", "deployments", "default", "my-app")
	want := "apps/v1/namespaces/default/deployments/my-app"
	if got != want {
		t.Errorf("ResourcePath() = %q; want %q", got, want)
	}
}

func TestResourcePath_ClusterScoped(t *testing.T) {
	got := ResourcePath("", "v1", "namespaces", "", "my-ns")
	want := "/v1/namespaces/my-ns"
	if got != want {
		t.Errorf("ResourcePath() = %q; want %q", got, want)
	}
}

func TestResourcePath_CoreNamespaced(t *testing.T) {
	got := ResourcePath("", "v1", "configmaps", "kube-system", "my-cm")
	want := "/v1/namespaces/kube-system/configmaps/my-cm"
	if got != want {
		t.Errorf("ResourcePath() = %q; want %q", got, want)
	}
}

// --- evaluateApprovalCheck tests ---

func TestEvaluateApprovalCheck_NoAnnotation(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm", nil)

	violated, msg, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation when approval annotation is absent")
	}
	if msg == "" {
		t.Error("expected non-empty denial message")
	}
}

func TestEvaluateApprovalCheck_ValidSignature(t *testing.T) {
	privKey, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	path := ResourcePath("", "v1", "configmaps", "default", "my-cm")
	sig := signPath(t, privKey, path)

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm",
		map[string]string{defaultApprovalAnnotation: sig})

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected no violation for valid approval signature")
	}
}

func TestEvaluateApprovalCheck_InvalidSignature(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm",
		map[string]string{defaultApprovalAnnotation: base64.StdEncoding.EncodeToString([]byte("not-a-real-sig"))})

	violated, msg, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation for invalid signature")
	}
	if msg == "" {
		t.Error("expected non-empty denial message")
	}
}

func TestEvaluateApprovalCheck_WrongKey(t *testing.T) {
	privKey1, _ := generateTestKeyPair(t)
	_, pubPEM2 := generateTestKeyPair(t) // different key pair

	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM2}

	path := ResourcePath("", "v1", "configmaps", "default", "my-cm")
	sig := signPath(t, privKey1, path) // signed with key1

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm",
		map[string]string{defaultApprovalAnnotation: sig})

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation when signature was made with a different key")
	}
}

func TestEvaluateApprovalCheck_WrongResourcePath(t *testing.T) {
	privKey, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	// Sign a different resource's path.
	wrongPath := ResourcePath("", "v1", "configmaps", "default", "other-cm")
	sig := signPath(t, privKey, wrongPath)

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm",
		map[string]string{defaultApprovalAnnotation: sig})

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation when signature covers a different resource path")
	}
}

func TestEvaluateApprovalCheck_InvalidBase64(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm",
		map[string]string{defaultApprovalAnnotation: "!!!not-base64!!!"})

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation for invalid base64 annotation value")
	}
}

func TestEvaluateApprovalCheck_CustomAnnotationKey(t *testing.T) {
	privKey, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{
		PublicKey:     pubPEM,
		AnnotationKey: "my-org/approved",
	}

	path := ResourcePath("apps", "v1", "deployments", "default", "my-app")
	sig := signPath(t, privKey, path)

	req := makeRequestWithAnnotations(admissionv1.Delete, "apps", "v1", "deployments", "default", "my-app",
		map[string]string{"my-org/approved": sig})

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected no violation with valid signature on custom annotation key")
	}
}

func TestEvaluateApprovalCheck_DeleteUsesOldObject(t *testing.T) {
	privKey, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	path := ResourcePath("", "v1", "configmaps", "default", "my-cm")
	sig := signPath(t, privKey, path)

	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{defaultApprovalAnnotation: sig},
		},
	}
	raw, _ := json.Marshal(obj)

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			},
			Namespace: "default",
			Name:      "my-cm",
			// Object is nil for DELETE; signature is in OldObject.
			OldObject: runtime.RawExtension{Raw: raw},
		},
	}

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected no violation when valid signature is in OldObject")
	}
}

func TestEvaluateApprovalCheck_CustomMessage(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}
	customMsg := "You must get approval first"

	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm", nil)

	violated, msg, err := evaluateApprovalCheck(check, customMsg, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation")
	}
	if msg != customMsg {
		t.Errorf("message = %q; want %q", msg, customMsg)
	}
}

func TestEvaluateApprovalCheck_InvalidPublicKey(t *testing.T) {
	check := ewv1alpha1.ApprovalCheck{PublicKey: "not-a-pem"}
	req := makeRequestWithAnnotations(admissionv1.Delete, "", "v1", "configmaps", "default", "my-cm", nil)

	_, _, err := evaluateApprovalCheck(check, "", req)
	if err == nil {
		t.Error("expected error for invalid public key")
	}
}
