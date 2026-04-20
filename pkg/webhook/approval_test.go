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

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
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
		t.Fatalf("marshaling public key: %v", err)
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
	want := "v1/namespaces/my-ns"
	if got != want {
		t.Errorf("ResourcePath() = %q; want %q", got, want)
	}
}

func TestResourcePath_CoreNamespaced(t *testing.T) {
	got := ResourcePath("", "v1", "configmaps", "kube-system", "my-cm")
	want := "v1/namespaces/kube-system/configmaps/my-cm"
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

// ---------------------------------------------------------------------------
// evaluateApprovalCheck — UPDATE (change approval) tests
// ---------------------------------------------------------------------------

// signPatch signs the given patch bytes with RSA-PSS SHA-256 and returns the
// base64-encoded signature.
func signPatchBytes(t *testing.T, key *rsa.PrivateKey, patchJSON []byte) string {
	t.Helper()
	digest := sha256.Sum256(patchJSON)
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		t.Fatalf("signing patch: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// makeApprovalUpdateRequest builds an admission.Request for an UPDATE operation where
// oldAnnotations are on OldObject and newAnnotations are on Object.
func makeApprovalUpdateRequest(
	group, version, resource, namespace, name string,
	oldAnnotations, newAnnotations map[string]string,
	oldData, newData map[string]interface{},
) admission.Request {
	makeObj := func(annotations map[string]string, data map[string]interface{}) []byte {
		obj := map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": annotations,
			},
		}
		if data != nil {
			obj["data"] = data
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			panic(err)
		}
		return raw
	}

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  version,
				Resource: resource,
			},
			Namespace: namespace,
			Name:      name,
			OldObject: runtime.RawExtension{Raw: makeObj(oldAnnotations, oldData)},
			Object:    runtime.RawExtension{Raw: makeObj(newAnnotations, newData)},
		},
	}
}

func TestEvaluateApprovalCheck_Update_NoAnnotation(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	req := makeApprovalUpdateRequest("", "v1", "configmaps", "default", "my-cm",
		nil, nil,
		map[string]interface{}{"key": "old"},
		map[string]interface{}{"key": "new"},
	)

	violated, msg, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation when change-approval annotation is absent")
	}
	if msg == "" {
		t.Error("expected non-empty denial message")
	}
}

func TestEvaluateApprovalCheck_Update_ValidSignature(t *testing.T) {
	privKey, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	oldData := map[string]interface{}{"key": "old"}
	newData := map[string]interface{}{"key": "new"}

	// Build the JSON objects the same way the webhook will see them.
	oldObj := map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]string{}}, "data": oldData}
	newObj := map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]string{}}, "data": newData}
	oldJSON, _ := json.Marshal(oldObj)
	newJSON, _ := json.Marshal(newObj)

	patchJSON := mustComputePatch(t, oldJSON, newJSON, defaultChangeApprovalAnnotation)
	sig := signPatchBytes(t, privKey, patchJSON)

	// The annotation is on the new (incoming) object, not the old one.
	req := makeApprovalUpdateRequest("", "v1", "configmaps", "default", "my-cm",
		nil,
		map[string]string{defaultChangeApprovalAnnotation: sig},
		oldData, newData,
	)

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected no violation for valid change-approval signature")
	}
}

func TestEvaluateApprovalCheck_Update_InvalidSignature(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	// Bad signature is on the new (incoming) object.
	req := makeApprovalUpdateRequest("", "v1", "configmaps", "default", "my-cm",
		nil,
		map[string]string{defaultChangeApprovalAnnotation: base64.StdEncoding.EncodeToString([]byte("bad-sig"))},
		map[string]interface{}{"key": "old"},
		map[string]interface{}{"key": "new"},
	)

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation for invalid signature")
	}
}

func TestEvaluateApprovalCheck_Update_WrongKey(t *testing.T) {
	privKey1, _ := generateTestKeyPair(t)
	_, pubPEM2 := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM2}

	oldData := map[string]interface{}{"key": "old"}
	newData := map[string]interface{}{"key": "new"}

	oldObj := map[string]interface{}{"data": oldData}
	newObj := map[string]interface{}{"data": newData}
	oldJSON, _ := json.Marshal(oldObj)
	newJSON, _ := json.Marshal(newObj)

	patchJSON := mustComputePatch(t, oldJSON, newJSON, defaultChangeApprovalAnnotation)
	sig := signPatchBytes(t, privKey1, patchJSON) // signed with key1 but check uses key2

	// Signature (from wrong key) is on the new (incoming) object.
	req := makeApprovalUpdateRequest("", "v1", "configmaps", "default", "my-cm",
		nil,
		map[string]string{defaultChangeApprovalAnnotation: sig},
		oldData, newData,
	)

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation when change signature was made with a different key")
	}
}

func TestEvaluateApprovalCheck_Update_CustomAnnotationKey(t *testing.T) {
	privKey, pubPEM := generateTestKeyPair(t)
	const customKey = "my-org/change-approved"
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM, ChangeAnnotationKey: customKey}

	oldData := map[string]interface{}{"k": "1"}
	newData := map[string]interface{}{"k": "2"}

	oldObj := map[string]interface{}{"data": oldData}
	newObj := map[string]interface{}{"data": newData}
	oldJSON, _ := json.Marshal(oldObj)
	newJSON, _ := json.Marshal(newObj)

	patchJSON := mustComputePatch(t, oldJSON, newJSON, customKey)
	sig := signPatchBytes(t, privKey, patchJSON)

	// Annotation is on the new (incoming) object.
	req := makeApprovalUpdateRequest("", "v1", "configmaps", "default", "my-cm",
		nil,
		map[string]string{customKey: sig},
		oldData, newData,
	)

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected no violation for valid change-approval with custom annotation key")
	}
}

func TestEvaluateApprovalCheck_Update_OldObjectMissing(t *testing.T) {
	_, pubPEM := generateTestKeyPair(t)
	check := ewv1alpha1.ApprovalCheck{PublicKey: pubPEM}

	// Object carries a (dummy) annotation so the code reaches the OldObject check.
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				defaultChangeApprovalAnnotation: base64.StdEncoding.EncodeToString([]byte("placeholder")),
			},
		},
		"data": map[string]interface{}{"k": "v"},
	}
	raw, _ := json.Marshal(obj)

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			Namespace: "default",
			Name:      "my-cm",
			Object:    runtime.RawExtension{Raw: raw},
			// OldObject intentionally absent.
		},
	}

	violated, _, err := evaluateApprovalCheck(check, "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected violation when OldObject is absent")
	}
}

// mustComputePatch is a test helper that calls the internal patch package.
func mustComputePatch(t *testing.T, oldJSON, newJSON []byte, stripAnnotation string) []byte {
	t.Helper()
	// Re-implement the same normalization logic inline for tests so that
	// test helpers do not import the internal package directly.  This keeps
	// the test hermetic while still computing the same bytes the webhook uses.
	stripAnnotations := []string{stripAnnotation}

	normalize := func(raw []byte) map[string]interface{} {
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("normalize unmarshal: %v", err)
		}
		meta, _ := obj["metadata"].(map[string]interface{})
		if meta != nil {
			for _, f := range []string{"resourceVersion", "generation", "uid", "creationTimestamp", "managedFields", "selfLink"} {
				delete(meta, f)
			}
			if annRaw, ok := meta["annotations"]; ok {
				if annRaw == nil {
					delete(meta, "annotations")
				} else {
					anns, _ := annRaw.(map[string]interface{})
					if anns != nil {
						for _, k := range stripAnnotations {
							delete(anns, k)
						}
						if len(anns) == 0 {
							delete(meta, "annotations")
						}
					}
				}
			}
			obj["metadata"] = meta
		}
		return obj
	}

	var computePatch func(src, dst map[string]interface{}) map[string]interface{}
	computePatch = func(src, dst map[string]interface{}) map[string]interface{} {
		patch := make(map[string]interface{})
		for k, dv := range dst {
			sv, exists := src[k]
			if !exists {
				patch[k] = dv
				continue
			}
			sm, sIsMap := sv.(map[string]interface{})
			dm, dIsMap := dv.(map[string]interface{})
			if sIsMap && dIsMap {
				sub := computePatch(sm, dm)
				if len(sub) > 0 {
					patch[k] = sub
				}
				continue
			}
			sb, _ := json.Marshal(sv)
			db, _ := json.Marshal(dv)
			if string(sb) != string(db) {
				patch[k] = dv
			}
		}
		for k := range src {
			if _, exists := dst[k]; !exists {
				patch[k] = nil
			}
		}
		return patch
	}

	baseOld := normalize(oldJSON)
	baseNew := normalize(newJSON)
	patch := computePatch(baseOld, baseNew)
	b, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	return b
}
