package patch

import (
	"encoding/json"
	"testing"
)

func TestComputeNormalizedMergePatch_SimpleChange(t *testing.T) {
	old := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm","resourceVersion":"123","managedFields":[]},"data":{"key":"old"}}`
	new := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm"},"data":{"key":"new"}}`

	got, err := ComputeNormalizedMergePatch([]byte(old), []byte(new), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(got, &patch); err != nil {
		t.Fatalf("unmarshaling patch: %v", err)
	}
	data, _ := patch["data"].(map[string]interface{})
	if data == nil {
		t.Fatal("expected data key in patch")
	}
	if data["key"] != "new" {
		t.Errorf("patch data.key = %v; want %q", data["key"], "new")
	}
	// resourceVersion must not appear in the patch.
	metadata, _ := patch["metadata"].(map[string]interface{})
	if metadata != nil {
		if _, ok := metadata["resourceVersion"]; ok {
			t.Error("patch must not contain metadata.resourceVersion")
		}
	}
}

func TestComputeNormalizedMergePatch_StripsAnnotations(t *testing.T) {
	old := `{"metadata":{"annotations":{"earlywatch.io/change-approved":"sig","user-annotation":"keep"}},"data":{"x":"1"}}`
	new := `{"metadata":{"annotations":{"user-annotation":"keep"}},"data":{"x":"2"}}`

	got, err := ComputeNormalizedMergePatch([]byte(old), []byte(new), []string{"earlywatch.io/change-approved"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(got, &patch); err != nil {
		t.Fatalf("unmarshaling patch: %v", err)
	}
	// The change-approved annotation is stripped from both sides before patch
	// computation, so only the data change should appear in the patch.
	data, _ := patch["data"].(map[string]interface{})
	if data == nil || data["x"] != "2" {
		t.Errorf("expected data.x=2 in patch, got %v", patch)
	}
	metadata, _ := patch["metadata"].(map[string]interface{})
	if metadata != nil {
		annotations, _ := metadata["annotations"].(map[string]interface{})
		if annotations != nil {
			if _, ok := annotations["earlywatch.io/change-approved"]; ok {
				t.Error("stripped annotation must not appear in patch")
			}
		}
	}
}

func TestComputeNormalizedMergePatch_KeyRemoved(t *testing.T) {
	old := `{"data":{"a":"1","b":"2"}}`
	new := `{"data":{"a":"1"}}`

	got, err := ComputeNormalizedMergePatch([]byte(old), []byte(new), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(got, &patch); err != nil {
		t.Fatalf("unmarshaling patch: %v", err)
	}
	data, _ := patch["data"].(map[string]interface{})
	if data == nil {
		t.Fatal("expected data in patch")
	}
	// "b" should appear with null value (deletion).
	bVal, ok := data["b"]
	if !ok {
		t.Error("expected key 'b' in patch to signal deletion")
	}
	if bVal != nil {
		t.Errorf("expected null for deleted key 'b', got %v", bVal)
	}
}

func TestComputeNormalizedMergePatch_NoChange(t *testing.T) {
	obj := `{"metadata":{"name":"cm","resourceVersion":"1"},"data":{"k":"v"}}`

	got, err := ComputeNormalizedMergePatch([]byte(obj), []byte(obj), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(got, &patch); err != nil {
		t.Fatalf("unmarshaling patch: %v", err)
	}
	if len(patch) != 0 {
		t.Errorf("expected empty patch for identical objects, got %v", patch)
	}
}

func TestComputeNormalizedMergePatch_ServerFieldsStripped(t *testing.T) {
	old := `{"metadata":{"name":"cm","resourceVersion":"1","generation":2,"uid":"abc","managedFields":[{"manager":"kubectl"}],"selfLink":"/api/v1/cm"},"data":{"k":"old"}}`
	new := `{"metadata":{"name":"cm","resourceVersion":"2","generation":3,"uid":"abc"},"data":{"k":"new"}}`

	got, err := ComputeNormalizedMergePatch([]byte(old), []byte(new), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var patch map[string]interface{}
	if err := json.Unmarshal(got, &patch); err != nil {
		t.Fatalf("unmarshaling patch: %v", err)
	}
	// Only the data change should appear; server-managed fields must be absent.
	if metadata, ok := patch["metadata"]; ok {
		metaMap, _ := metadata.(map[string]interface{})
		for _, field := range []string{"resourceVersion", "generation", "uid", "managedFields", "selfLink"} {
			if _, present := metaMap[field]; present {
				t.Errorf("server-managed field %q must not appear in patch", field)
			}
		}
	}
	data, _ := patch["data"].(map[string]interface{})
	if data == nil || data["k"] != "new" {
		t.Errorf("expected data.k=new in patch, got %v", patch)
	}
}

func TestComputeNormalizedMergePatch_Deterministic(t *testing.T) {
	old := `{"data":{"a":"1","b":"2"}}`
	new := `{"data":{"a":"x","b":"y"}}`

	got1, err := ComputeNormalizedMergePatch([]byte(old), []byte(new), nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	got2, err := ComputeNormalizedMergePatch([]byte(old), []byte(new), nil)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(got1) != string(got2) {
		t.Errorf("patch is not deterministic:\n  got1: %s\n  got2: %s", got1, got2)
	}
}
