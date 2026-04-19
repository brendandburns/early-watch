// Package patch provides internal utilities for computing and normalising JSON
// merge patches between Kubernetes resource objects.  These utilities are
// shared by the admission webhook (verification) and the approve CLI command
// (signing).
package patch

import (
	"encoding/json"
	"fmt"
)

// serverManagedFields lists the metadata fields that Kubernetes manages
// automatically and must be excluded from patch computation so that the
// resulting patch only reflects user-visible changes.
var serverManagedFields = []string{
	"resourceVersion",
	"generation",
	"uid",
	"creationTimestamp",
	"managedFields",
	"selfLink",
}

// ComputeNormalizedMergePatch computes a canonical RFC 7396 JSON merge patch
// from oldJSON to newJSON.  Before computing the patch, server-managed
// metadata fields and the annotation keys listed in stripAnnotations are
// stripped from both objects so that the resulting patch only captures
// user-visible intent.
//
// The returned bytes are the JSON-encoded merge patch, suitable for signing
// or verification.  Go's encoding/json marshals map keys in sorted order, so
// the output is deterministic.
func ComputeNormalizedMergePatch(oldJSON, newJSON []byte, stripAnnotations []string) ([]byte, error) {
	baseOld, err := normalizeForPatch(oldJSON, stripAnnotations)
	if err != nil {
		return nil, fmt.Errorf("normalizing old object: %w", err)
	}
	baseNew, err := normalizeForPatch(newJSON, stripAnnotations)
	if err != nil {
		return nil, fmt.Errorf("normalizing new object: %w", err)
	}

	patch, err := computeMergePatch(baseOld, baseNew)
	if err != nil {
		return nil, fmt.Errorf("computing merge patch: %w", err)
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("marshaling patch: %w", err)
	}
	return patchJSON, nil
}

// normalizeForPatch returns the object decoded from raw with server-managed
// metadata fields removed and the specified annotation keys deleted from
// metadata.annotations.  An empty or null annotations map is removed entirely
// so that the resulting object is clean for patch computation.
func normalizeForPatch(raw []byte, stripAnnotations []string) (map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("unmarshalling object: %w", err)
	}

	metadata, _ := obj["metadata"].(map[string]interface{})
	if metadata != nil {
		for _, field := range serverManagedFields {
			delete(metadata, field)
		}

		if annotationsRaw, ok := metadata["annotations"]; ok {
			if annotationsRaw == nil {
				// JSON null – remove the key so both sides are comparable.
				delete(metadata, "annotations")
			} else {
				annotations, _ := annotationsRaw.(map[string]interface{})
				if annotations != nil {
					for _, key := range stripAnnotations {
						delete(annotations, key)
					}
					if len(annotations) == 0 {
						delete(metadata, "annotations")
					}
				}
			}
		}

		obj["metadata"] = metadata
	}

	return obj, nil
}

// computeMergePatch creates a JSON merge patch (RFC 7396) representing the
// changes needed to transform src into dst.  The result contains only changed
// keys; keys removed in dst appear with a nil value (JSON null).
//
// Nested maps are recursed so that the patch only includes the minimal set of
// changed fields.  Non-map values are compared by their JSON representation to
// ensure deterministic equality checks for numbers, booleans, and slices.
func computeMergePatch(src, dst map[string]interface{}) (map[string]interface{}, error) {
	patch := make(map[string]interface{})

	for k, dstVal := range dst {
		srcVal, exists := src[k]
		if !exists {
			patch[k] = dstVal
			continue
		}

		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstMap, dstIsMap := dstVal.(map[string]interface{})
		if srcIsMap && dstIsMap {
			subPatch, err := computeMergePatch(srcMap, dstMap)
			if err != nil {
				return nil, err
			}
			if len(subPatch) > 0 {
				patch[k] = subPatch
			}
			continue
		}

		// Compare by JSON representation for deterministic equality.
		srcBytes, err := json.Marshal(srcVal)
		if err != nil {
			return nil, fmt.Errorf("marshaling source value for key %q: %w", k, err)
		}
		dstBytes, err := json.Marshal(dstVal)
		if err != nil {
			return nil, fmt.Errorf("marshaling destination value for key %q: %w", k, err)
		}
		if string(srcBytes) != string(dstBytes) {
			patch[k] = dstVal
		}
	}

	// Keys present in src but absent in dst are deletions (null in merge patch).
	for k := range src {
		if _, exists := dst[k]; !exists {
			patch[k] = nil
		}
	}

	return patch, nil
}
