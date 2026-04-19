package install

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// newDeployment returns a minimal Unstructured Deployment for use in tests.
func newDeployment(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": "test-ns",
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
	}
}

// TestInjectImagePullSecret_AddNew verifies that a secret is added when the
// imagePullSecrets list is empty.
func TestInjectImagePullSecret_AddNew(t *testing.T) {
	obj := newDeployment("webhook")
	injectImagePullSecret(obj, "my-secret")

	secrets, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "imagePullSecrets")
	if err != nil || !found {
		t.Fatalf("imagePullSecrets not found: err=%v found=%v", err, found)
	}
	if len(secrets) != 1 {
		t.Fatalf("expected 1 imagePullSecret, got %d", len(secrets))
	}
	m, ok := secrets[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected type for imagePullSecrets entry: %T", secrets[0])
	}
	if m["name"] != "my-secret" {
		t.Errorf("imagePullSecrets[0].name = %q, want %q", m["name"], "my-secret")
	}
}

// TestInjectImagePullSecret_NoDuplicate verifies that calling injectImagePullSecret
// twice with the same secret name does not create a duplicate entry.
func TestInjectImagePullSecret_NoDuplicate(t *testing.T) {
	obj := newDeployment("webhook")
	injectImagePullSecret(obj, "my-secret")
	injectImagePullSecret(obj, "my-secret")

	secrets, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "imagePullSecrets")
	if err != nil || !found {
		t.Fatalf("imagePullSecrets not found: err=%v found=%v", err, found)
	}
	if len(secrets) != 1 {
		t.Errorf("expected 1 imagePullSecret after duplicate inject, got %d", len(secrets))
	}
}

// TestInjectImagePullSecret_PreservesExisting verifies that a second distinct
// secret is appended without removing the first.
func TestInjectImagePullSecret_PreservesExisting(t *testing.T) {
	obj := newDeployment("webhook")
	injectImagePullSecret(obj, "first-secret")
	injectImagePullSecret(obj, "second-secret")

	secrets, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "imagePullSecrets")
	if err != nil || !found {
		t.Fatalf("imagePullSecrets not found: err=%v found=%v", err, found)
	}
	if len(secrets) != 2 {
		t.Fatalf("expected 2 imagePullSecrets, got %d", len(secrets))
	}
}
