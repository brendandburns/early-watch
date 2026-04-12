package apply

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// patchRecord captures the arguments of a single Patch call.
type patchRecord struct {
	namespace string
	name      string
	patchType types.PatchType
}

// newRecordingDynamic returns a fake dynamic client that records all Patch calls
// in the given slice. If patchErr is non-nil, Patch calls return that error
// instead of recording.
func newRecordingDynamic(records *[]patchRecord, patchErr error) *dynamicfake.FakeDynamicClient {
	fdc := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	fdc.PrependReactor("patch", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if patchErr != nil {
			return true, nil, patchErr
		}
		pa := action.(k8stesting.PatchAction)
		*records = append(*records, patchRecord{
			namespace: pa.GetNamespace(),
			name:      pa.GetName(),
			patchType: pa.GetPatchType(),
		})
		return true, &unstructured.Unstructured{}, nil
	})
	return fdc
}

// changeValidatorGVK is the GVK used for most tests.
var changeValidatorGVK = schema.GroupVersionKind{
	Group:   "earlywatch.io",
	Version: "v1alpha1",
	Kind:    "ChangeValidator",
}

// newNamespacedMapper returns a REST mapper that resolves changeValidatorGVK to
// a namespace-scoped resource.
func newNamespacedMapper() *meta.DefaultRESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(changeValidatorGVK, meta.RESTScopeNamespace)
	return m
}

// newClusterScopedMapper returns a REST mapper that resolves changeValidatorGVK
// to a cluster-scoped resource.
func newClusterScopedMapper() *meta.DefaultRESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(changeValidatorGVK, meta.RESTScopeRoot)
	return m
}

// errorRESTMapper is a meta.RESTMapper whose RESTMapping always returns an error.
type errorRESTMapper struct{ err error }

func (e *errorRESTMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, e.err
}
func (e *errorRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, e.err
}
func (e *errorRESTMapper) ResourceFor(_ schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, e.err
}
func (e *errorRESTMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, e.err
}
func (e *errorRESTMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, e.err
}
func (e *errorRESTMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	return nil, e.err
}
func (e *errorRESTMapper) AbbreviatedKindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, e.err
}
func (e *errorRESTMapper) ResourceSingularizer(_ string) (string, error) {
	return "", e.err
}

// validCVYAML is a minimal ChangeValidator document for use in ApplyManifest tests.
const validCVYAML = `
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: test-validator
  namespace: test-ns
spec:
  subject:
    resource: services
  operations:
    - DELETE
  rules:
    - name: r
      type: ExpressionCheck
      expressionCheck:
        expression: "operation == 'DELETE'"
      message: msg
`

// validCVNoNamespaceYAML is a ChangeValidator without an explicit namespace.
const validCVNoNamespaceYAML = `
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: test-validator
spec:
  subject:
    resource: services
  operations:
    - DELETE
  rules:
    - name: r
      type: ExpressionCheck
      expressionCheck:
        expression: "operation == 'DELETE'"
      message: msg
`

// --- BoolPtr tests ---

func TestBoolPtr_True(t *testing.T) {
	v := BoolPtr(true)
	if v == nil {
		t.Fatal("expected non-nil pointer")
	}
	if !*v {
		t.Errorf("got %v, want true", *v)
	}
}

func TestBoolPtr_False(t *testing.T) {
	v := BoolPtr(false)
	if v == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *v {
		t.Errorf("got %v, want false", *v)
	}
}

// --- ResourceDisplayName tests ---

func TestResourceDisplayName_Namespaced(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("my-cv")
	obj.SetNamespace("production")

	got := ResourceDisplayName(obj)
	want := "production/my-cv"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResourceDisplayName_ClusterScoped(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("my-cv")

	got := ResourceDisplayName(obj)
	want := "my-cv"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResourceDisplayName_EmptyName(t *testing.T) {
	obj := &unstructured.Unstructured{}
	got := ResourceDisplayName(obj)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// --- ApplyManifest tests ---

func TestApplyManifest_ValidNamespacedResource(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVYAML), "test.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 patch call, got %d", len(records))
	}
	if records[0].name != "test-validator" {
		t.Errorf("patch name: got %q, want %q", records[0].name, "test-validator")
	}
	if records[0].namespace != "test-ns" {
		t.Errorf("patch namespace: got %q, want %q", records[0].namespace, "test-ns")
	}
	if records[0].patchType != types.ApplyPatchType {
		t.Errorf("patch type: got %v, want ApplyPatchType", records[0].patchType)
	}
}

func TestApplyManifest_NamespaceDefaultsToDefault(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVNoNamespaceYAML), "test.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 patch call, got %d", len(records))
	}
	if records[0].namespace != "default" {
		t.Errorf("namespace: got %q, want %q", records[0].namespace, "default")
	}
}

func TestApplyManifest_NamespaceSetOnObjectWhenDefaulted(t *testing.T) {
	// Verify that when namespace is defaulted to "default", obj.SetNamespace is
	// called so the marshaled payload reflects the target namespace.
	var patchedData []byte
	fdc := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	fdc.PrependReactor("patch", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		pa := action.(k8stesting.PatchAction)
		patchedData = pa.GetPatch()
		return true, &unstructured.Unstructured{}, nil
	})

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVNoNamespaceYAML), "test.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The marshaled JSON must contain the defaulted namespace.
	if !contains(patchedData, `"default"`) {
		t.Errorf("expected patched payload to contain namespace %q, got: %s", "default", string(patchedData))
	}
}

// contains is a helper to check if a byte slice contains a substring.
func contains(data []byte, s string) bool {
	return len(data) > 0 && indexOf(data, []byte(s)) >= 0
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

func TestApplyManifest_ClusterScopedResourceOmitsNamespace(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newClusterScopedMapper(), []byte(validCVYAML), "test.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 patch call, got %d", len(records))
	}
	if records[0].namespace != "" {
		t.Errorf("expected empty namespace for cluster-scoped resource, got %q", records[0].namespace)
	}
}

func TestApplyManifest_MultiDocumentYAML(t *testing.T) {
	multiDoc := validCVYAML + "\n---\n" + `
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: second-validator
  namespace: test-ns
spec:
  subject:
    resource: deployments
  operations:
    - DELETE
  rules:
    - name: r
      type: ExpressionCheck
      expressionCheck:
        expression: "operation == 'DELETE'"
      message: msg
`
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(multiDoc), "multi.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 patch calls for 2-document YAML, got %d", len(records))
	}
}

func TestApplyManifest_EmptyDocument(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte("---\n"), "empty.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no patch calls for empty document, got %d", len(records))
	}
}

func TestApplyManifest_SkipsDocumentWithoutKind(t *testing.T) {
	noKind := `
apiVersion: earlywatch.io/v1alpha1
metadata:
  name: no-kind
`
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(noKind), "nokind.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no patch calls for document without kind, got %d", len(records))
	}
}

func TestApplyManifest_MissingName(t *testing.T) {
	noName := `
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  namespace: test-ns
`
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(noName), "noname.yaml", nil)
	if err == nil {
		t.Fatal("expected error for resource missing metadata.name, got nil")
	}
	if len(records) != 0 {
		t.Errorf("expected no patch calls when name is missing, got %d", len(records))
	}
}

func TestApplyManifest_MissingNameErrorContainsFilenameAndGVK(t *testing.T) {
	noName := `
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  namespace: test-ns
`
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(noName), "my-dir/validators.yaml", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !containsStr(errMsg, "my-dir/validators.yaml") {
		t.Errorf("error %q does not contain filename", errMsg)
	}
	if !containsStr(errMsg, "ChangeValidator") {
		t.Errorf("error %q does not contain GVK kind", errMsg)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findStr(s, sub))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestApplyManifest_RESTMapperError(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)
	errMapper := &errorRESTMapper{err: fmt.Errorf("no mapping found")}

	err := Manifest(context.Background(), fdc, errMapper, []byte(validCVYAML), "test.yaml", nil)
	if err == nil {
		t.Fatal("expected error from REST mapper, got nil")
	}
	if len(records) != 0 {
		t.Errorf("expected no patch calls when mapper fails, got %d", len(records))
	}
}

func TestApplyManifest_PatchError(t *testing.T) {
	patchErr := fmt.Errorf("server unavailable")
	var records []patchRecord
	fdc := newRecordingDynamic(&records, patchErr)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVYAML), "test.yaml", nil)
	if err == nil {
		t.Fatal("expected error from Patch call, got nil")
	}
}

func TestApplyManifest_InvalidYAML(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(":{invalid yaml"), "bad.yaml", nil)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestApplyManifest_PreApplyCallbackInvoked(t *testing.T) {
	// Verify the preApply callback is called for each object and can mutate
	// the object before it is applied.
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	var callCount int
	preApply := func(obj *unstructured.Unstructured) {
		callCount++
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["test/injected"] = "yes"
		obj.SetAnnotations(annotations)
	}

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVYAML), "test.yaml", preApply)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("preApply called %d times, want 1", callCount)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 patch call, got %d", len(records))
	}
}

func TestApplyManifest_PreApplyCallbackCalledForEachDocument(t *testing.T) {
	multiDoc := validCVYAML + "\n---\n" + `
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: second-validator
  namespace: test-ns
spec:
  subject:
    resource: deployments
  operations:
    - DELETE
  rules:
    - name: r
      type: ExpressionCheck
      expressionCheck:
        expression: "operation == 'DELETE'"
      message: msg
`
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	var callCount int
	preApply := func(_ *unstructured.Unstructured) { callCount++ }

	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(multiDoc), "multi.yaml", preApply)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("preApply called %d times, want 2", callCount)
	}
}

func TestApplyManifest_NilPreApplyDoesNotPanic(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	// Must not panic when preApply is nil.
	err := Manifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVYAML), "test.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
