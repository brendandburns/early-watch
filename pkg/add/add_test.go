package add

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// changeValidatorGVK is the GVK used for most applyManifest tests.
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

// --- collectYAMLFiles tests ---

func TestCollectYAMLFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(f, []byte("test"), 0600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	got, err := collectYAMLFiles(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != f {
		t.Errorf("got %v, want [%s]", got, f)
	}
}

func TestCollectYAMLFiles_DirectoryWithYAMLAndYML(t *testing.T) {
	dir := t.TempDir()
	files := []string{"a.yaml", "b.yml", "c.yaml"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	// Add a non-YAML file that should be excluded.
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("x"), 0600); err != nil {
		t.Fatalf("writing skip.txt: %v", err)
	}

	got, err := collectYAMLFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d files, want 3: %v", len(got), got)
	}
	for _, f := range got {
		ext := filepath.Ext(f)
		if ext != ".yaml" && ext != ".yml" {
			t.Errorf("unexpected file in result: %s", f)
		}
	}
}

func TestCollectYAMLFiles_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	got, err := collectYAMLFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result for empty directory, got %v", got)
	}
}

func TestCollectYAMLFiles_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory that should NOT be descended into.
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Place a YAML inside the subdir; it must NOT appear in the result.
	if err := os.WriteFile(filepath.Join(sub, "nested.yaml"), []byte("x"), 0600); err != nil {
		t.Fatalf("writing nested.yaml: %v", err)
	}
	// A YAML at the top level should still be found.
	top := filepath.Join(dir, "top.yaml")
	if err := os.WriteFile(top, []byte("x"), 0600); err != nil {
		t.Fatalf("writing top.yaml: %v", err)
	}

	got, err := collectYAMLFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != top {
		t.Errorf("got %v, want [%s]", got, top)
	}
}

func TestCollectYAMLFiles_NonExistentPath(t *testing.T) {
	_, err := collectYAMLFiles("/no/such/path/ever")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// --- resourceDisplayName tests ---

func TestResourceDisplayName_Namespaced(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("my-cv")
	obj.SetNamespace("production")

	got := resourceDisplayName(obj)
	want := "production/my-cv"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResourceDisplayName_ClusterScoped(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("my-cv")

	got := resourceDisplayName(obj)
	want := "my-cv"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- applyManifest tests ---

// validCVYAML is a minimal ChangeValidator document for use in applyManifest tests.
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

func TestApplyManifest_ValidNamespacedResource(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVYAML), "test.yaml")
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

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVNoNamespaceYAML), "test.yaml")
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

func TestApplyManifest_ClusterScopedResourceOmitsNamespace(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := applyManifest(context.Background(), fdc, newClusterScopedMapper(), []byte(validCVYAML), "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 patch call, got %d", len(records))
	}
	// For cluster-scoped resources the namespace passed to the client must be empty.
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

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte(multiDoc), "multi.yaml")
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

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte("---\n"), "empty.yaml")
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

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte(noKind), "nokind.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no patch calls for document without kind, got %d", len(records))
	}
}

func TestApplyManifest_RESTMapperError(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)
	errMapper := &errorRESTMapper{err: fmt.Errorf("no mapping found")}

	err := applyManifest(context.Background(), fdc, errMapper, []byte(validCVYAML), "test.yaml")
	if err == nil {
		t.Fatal("expected error from REST mapper, got nil")
	}
	if len(records) != 0 {
		t.Errorf("expected no patch calls when mapper fails, got %d", len(records))
	}
}

func TestApplyManifest_PatchError(t *testing.T) {
	patchErr := fmt.Errorf("server unavailable")
	records := []patchRecord{}
	fdc := newRecordingDynamic(&records, patchErr)

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte(validCVYAML), "test.yaml")
	if err == nil {
		t.Fatal("expected error from Patch call, got nil")
	}
}

func TestApplyManifest_InvalidYAML(t *testing.T) {
	var records []patchRecord
	fdc := newRecordingDynamic(&records, nil)

	err := applyManifest(context.Background(), fdc, newNamespacedMapper(), []byte(":{invalid yaml"), "bad.yaml")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}
