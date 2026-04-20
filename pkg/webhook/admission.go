// Package webhook implements the EarlyWatch admission webhook handler.
package webhook

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	"github.com/brendandburns/early-watch/pkg/auditmonitor"
)

// AdmissionHandler handles admission webhook requests by evaluating
// ChangeValidator rules registered in the cluster.
type AdmissionHandler struct {
	Client        client.Client
	DynamicClient dynamic.Interface
	Decoder       admission.Decoder
}

// Handle is the main admission webhook entry point.  It is called by
// controller-runtime for every intercepted admission request.
func (h *AdmissionHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues(
		"operation", req.Operation,
		"resource", req.Resource.Resource,
		"namespace", req.Namespace,
		"name", req.Name,
	)

	logger.Info("Evaluating admission request")

	// List all ChangeValidators in the same namespace as the subject resource.
	guardList := &ewv1alpha1.ChangeValidatorList{}
	if err := h.Client.List(ctx, guardList, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		logger.Error(err, "Failed to list ChangeValidators")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Also fetch cluster-scoped guards (namespace == "").
	if req.Namespace != "" {
		clusterGuardList := &ewv1alpha1.ChangeValidatorList{}
		if err := h.Client.List(ctx, clusterGuardList, &client.ListOptions{}); err != nil {
			logger.Error(err, "Failed to list cluster-scoped ChangeValidators")
			return admission.Errored(http.StatusInternalServerError, err)
		}
		guardList.Items = append(guardList.Items, clusterGuardList.Items...)
	}

	for _, guard := range guardList.Items {
		if !appliesToRequest(&guard, req) {
			continue
		}

		logger.Info("Evaluating ChangeValidator", "guard", guard.Name)

		for _, rule := range guard.Spec.Rules {
			violated, message, err := h.evaluateRule(ctx, rule, req)
			if err != nil {
				logger.Error(err, "Error evaluating rule", "rule", rule.Name)
				return admission.Errored(http.StatusInternalServerError, err)
			}
			if violated {
				logger.Info("Request denied by ChangeValidator rule",
					"guard", guard.Name,
					"rule", rule.Name,
					"message", message,
				)
				return admission.Denied(message)
			}
		}
	}

	return admission.Allowed("no ChangeValidator rules violated")
}

// appliesToRequest returns true when the guard matches the admission request's
// resource type and operation.
func appliesToRequest(guard *ewv1alpha1.ChangeValidator, req admission.Request) bool {
	subj := guard.Spec.Subject

	// Match API group.
	reqGroup := req.Resource.Group
	if subj.APIGroup != reqGroup {
		return false
	}

	// Match resource kind.
	if !strings.EqualFold(subj.Resource, req.Resource.Resource) {
		return false
	}

	// Match specific resource names if the guard is scoped to a list of names.
	if len(subj.Names) > 0 {
		nameSet := make(map[string]struct{}, len(subj.Names))
		for _, n := range subj.Names {
			nameSet[n] = struct{}{}
		}
		if _, ok := nameSet[req.Name]; !ok {
			return false
		}
	}

	// Match operation.
	for _, op := range guard.Spec.Operations {
		if admissionv1.Operation(op) == req.Operation {
			return true
		}
	}
	return false
}

// evaluateRule evaluates a single GuardRule against the admission request.
// It returns (violated, message, error).
func (h *AdmissionHandler) evaluateRule(
	ctx context.Context,
	rule ewv1alpha1.GuardRule,
	req admission.Request,
) (bool, string, error) {
	switch rule.Type {
	case ewv1alpha1.RuleTypeExistingResources:
		if rule.ExistingResources == nil {
			return false, "", fmt.Errorf("rule %q has type ExistingResources but no existingResources config", rule.Name)
		}
		return h.evaluateExistingResources(ctx, *rule.ExistingResources, rule.Message, req)

	case ewv1alpha1.RuleTypeExpressionCheck:
		if rule.ExpressionCheck == nil {
			return false, "", fmt.Errorf("rule %q has type ExpressionCheck but no expressionCheck config", rule.Name)
		}
		return evaluateExpression(*rule.ExpressionCheck, rule.Message, req)

	case ewv1alpha1.RuleTypeNameReferenceCheck:
		if rule.NameReferenceCheck == nil {
			return false, "", fmt.Errorf("rule %q has type NameReferenceCheck but no nameReferenceCheck config", rule.Name)
		}
		return h.evaluateNameReferenceCheck(ctx, *rule.NameReferenceCheck, rule.Message, req)

	case ewv1alpha1.RuleTypeApprovalCheck:
		if rule.ApprovalCheck == nil {
			return false, "", fmt.Errorf("rule %q has type ApprovalCheck but no approvalCheck config", rule.Name)
		}
		return evaluateApprovalCheck(*rule.ApprovalCheck, rule.Message, req)

	case ewv1alpha1.RuleTypeAnnotationCheck:
		if rule.AnnotationCheck == nil {
			return false, "", fmt.Errorf("rule %q has type AnnotationCheck but no annotationCheck config", rule.Name)
		}
		return evaluateAnnotationCheck(*rule.AnnotationCheck, rule.Message, req)

	case ewv1alpha1.RuleTypeManualTouchCheck:
		if rule.ManualTouchCheck == nil {
			return false, "", fmt.Errorf("rule %q has type ManualTouchCheck but no manualTouchCheck config", rule.Name)
		}
		return h.evaluateManualTouchCheck(ctx, *rule.ManualTouchCheck, rule.Message, req)

	case ewv1alpha1.RuleTypeCheckLock:
		return evaluateCheckLock(rule.CheckLock, rule.Message, req)

	case ewv1alpha1.RuleTypeServicePodSelectorCheck:
		if rule.ServicePodSelectorCheck == nil {
			return false, "", fmt.Errorf("rule %q has type ServicePodSelectorCheck but no ServicePodSelectorCheck config", rule.Name)
		}
		return h.evaluateServicePodSelectorCheck(ctx, rule.Message, req)

	default:
		return false, "", fmt.Errorf("unknown rule type %q in rule %q", rule.Type, rule.Name)
	}
}

// renderMessage processes a message template by replacing mustache-style
// {{variable}} placeholders with values from the admission request.
//
// Supported variables:
//
//	{{name}}      – the name of the resource being acted on
//	{{namespace}} – the namespace of the resource (empty for cluster-scoped)
//	{{resource}}  – the plural resource type, e.g. "services"
//	{{operation}} – the admission operation, e.g. "DELETE"
//	{{apiGroup}}  – the API group, e.g. "apps" (empty for core resources)
func renderMessage(message string, req admission.Request) string {
	r := strings.NewReplacer(
		"{{name}}", req.Name,
		"{{namespace}}", req.Namespace,
		"{{resource}}", req.Resource.Resource,
		"{{operation}}", string(req.Operation),
		"{{apiGroup}}", req.Resource.Group,
	)
	return r.Replace(message)
}

// evaluateExistingResources queries the cluster for resources that depend on
// the subject and returns true (violated) when any are found.
func (h *AdmissionHandler) evaluateExistingResources(
	ctx context.Context,
	check ewv1alpha1.ExistingResourcesCheck,
	message string,
	req admission.Request,
) (bool, string, error) {
	// Determine the label selector to use.
	var sel labels.Selector
	var err error

	switch {
	case check.LabelSelectorFromField != "":
		// For DELETE requests, the object being deleted is in OldObject rather
		// than Object (which is nil for deletes).
		raw := req.Object.Raw
		if len(raw) == 0 {
			raw = req.OldObject.Raw
		}
		// Extract selector from the subject object's field.
		sel, err = selectorFromField(raw, check.LabelSelectorFromField)
		if err != nil {
			return false, "", fmt.Errorf("extracting selector from field %q: %w", check.LabelSelectorFromField, err)
		}
	case check.LabelSelector != nil:
		sel, err = metav1.LabelSelectorAsSelector(check.LabelSelector)
		if err != nil {
			return false, "", fmt.Errorf("converting label selector: %w", err)
		}
	default:
		sel = labels.Everything()
	}

	// Determine namespace scope.
	namespace := ""
	if check.SameNamespace == nil || *check.SameNamespace {
		namespace = req.Namespace
		// For cluster-scoped resources such as Namespace objects, req.Namespace
		// is empty but req.Name holds the name of the resource being acted on
		// (e.g. the namespace being deleted).  When the subject resource IS a
		// namespace, use req.Name so that we search inside that namespace.
		if namespace == "" && strings.EqualFold(req.Resource.Resource, "namespaces") {
			namespace = req.Name
		}
	}

	// Build GVR for the dependent resource.
	gvr := schema.GroupVersionResource{
		Group:    check.APIGroup,
		Version:  "v1",
		Resource: check.Resource,
	}

	// Query the cluster using the dynamic client.
	listOpts := metav1.ListOptions{
		LabelSelector: sel.String(),
	}

	result, err := h.DynamicClient.Resource(gvr).Namespace(namespace).List(ctx, listOpts)
	if err != nil {
		return false, "", fmt.Errorf("listing %s: %w", check.Resource, err)
	}

	if len(result.Items) > 0 {
		return true, renderMessage(message, req), nil
	}

	return false, "", nil
}

// evaluateNameReferenceCheck lists resources of each specified type and denies
// the request if any resource references the subject by name at the configured
// field paths.
func (h *AdmissionHandler) evaluateNameReferenceCheck(
	ctx context.Context,
	check ewv1alpha1.NameReferenceCheck,
	message string,
	req admission.Request,
) (bool, string, error) {
	namespace := ""
	if check.SameNamespace == nil || *check.SameNamespace {
		namespace = req.Namespace
	}

	for _, res := range check.Resources {
		version := res.Version
		if version == "" {
			version = "v1"
		}
		gvr := schema.GroupVersionResource{
			Group:    res.APIGroup,
			Version:  version,
			Resource: res.Resource,
		}

		result, err := h.DynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, "", fmt.Errorf("listing %s: %w", res.Resource, err)
		}

		for _, item := range result.Items {
			for _, fieldPath := range res.NameFields {
				if nameExistsAtPath(item.Object, strings.Split(fieldPath, "."), req.Name) {
					return true, renderMessage(message, req), nil
				}
			}
		}
	}

	return false, "", nil
}

// nameExistsAtPath reports whether the given name appears as a string value at
// the end of the dot-split path parts within obj.  Array elements encountered
// along the path are all traversed so that references nested inside slices
// (e.g. volumes, containers, envFrom) are detected without needing wildcard
// syntax in the field path.
func nameExistsAtPath(current interface{}, parts []string, name string) bool {
	if len(parts) == 0 {
		str, ok := current.(string)
		return ok && str == name
	}

	switch v := current.(type) {
	case map[string]interface{}:
		next, ok := v[parts[0]]
		if !ok {
			return false
		}
		return nameExistsAtPath(next, parts[1:], name)
	case []interface{}:
		// Traverse every element without consuming a path part so that array
		// elements are treated transparently.
		for _, elem := range v {
			if nameExistsAtPath(elem, parts, name) {
				return true
			}
		}
	}
	return false
}

// selectorFromField extracts a label selector from a dot-separated field path
// in the raw JSON object.  The field value is expected to be a
// map[string]string (e.g. a Kubernetes selector map).
func selectorFromField(raw []byte, fieldPath string) (labels.Selector, error) {
	// Unmarshal to a generic map.
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("unmarshalling object: %w", err)
	}

	// Navigate the field path.
	parts := strings.Split(fieldPath, ".")
	current := obj
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return nil, fmt.Errorf("field %q not found at path segment %q", fieldPath, strings.Join(parts[:i+1], "."))
		}
		if i == len(parts)-1 {
			// This should be the selector map.
			selectorMap, ok := val.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("field %q is not a map", fieldPath)
			}
			labelMap := make(map[string]string, len(selectorMap))
			for k, v := range selectorMap {
				str, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("selector value for key %q is not a string", k)
				}
				labelMap[k] = str
			}
			return labels.SelectorFromSet(labelMap), nil
		}
		nested, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("field segment %q is not an object", strings.Join(parts[:i+1], "."))
		}
		current = nested
	}

	return labels.Everything(), nil
}

// evaluateCheckLock denies a DELETE request when the subject resource carries
// the earlywatch.io/lock annotation.  When cfg is non-nil and LockOnMutate is
// true the check is also applied to UPDATE requests.  For all other operations
// the check is a no-op.
//
// Special case for UPDATE with LockOnMutate: if the only change between the
// old and new object is the removal or clearing of the earlywatch.io/lock
// annotation itself, the UPDATE is allowed so that operators can always
// unlock a resource.
func evaluateCheckLock(cfg *ewv1alpha1.CheckLockRule, message string, req admission.Request) (bool, string, error) {
	lockOnMutate := cfg != nil && cfg.LockOnMutate != nil && *cfg.LockOnMutate

	switch req.Operation {
	case admissionv1.Delete:
		// continue below
	case admissionv1.Update:
		if !lockOnMutate {
			return false, "", nil
		}
		// Special case: always allow an UPDATE whose only change is removing
		// or clearing the earlywatch.io/lock annotation.  This ensures that
		// an operator can always unlock a resource, even when lockOnMutate
		// is true.
		if len(req.OldObject.Raw) > 0 && len(req.Object.Raw) > 0 {
			onlyLock, err := isOnlyLockAnnotationChange(req.OldObject.Raw, req.Object.Raw)
			if err != nil {
				return false, "", fmt.Errorf("comparing old and new object for lock annotation change: %w", err)
			}
			if onlyLock {
				return false, "", nil
			}
		}
		// continue below
	default:
		return false, "", nil
	}

	// For DELETE requests the object being deleted is in OldObject.
	// For UPDATE requests the pre-update state is also in OldObject.
	raw := req.OldObject.Raw
	if len(raw) == 0 {
		// Fall back to Object in case the webhook is configured to populate it.
		raw = req.Object.Raw
	}
	if len(raw) == 0 {
		return false, "", nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, "", fmt.Errorf("unmarshalling object for lock check: %w", err)
	}

	metadata, _ := obj["metadata"].(map[string]interface{})
	if metadata == nil {
		return false, "", nil
	}
	annotations, _ := metadata["annotations"].(map[string]interface{})
	if annotations == nil {
		return false, "", nil
	}
	if v, locked := annotations[ewv1alpha1.LockAnnotation]; locked && v != "" {
		return true, message, nil
	}
	return false, "", nil
}

// isOnlyLockAnnotationChange reports whether the only difference between
// oldRaw and newRaw is the removal or clearing of the earlywatch.io/lock
// annotation.  It strips the lock annotation key from both objects and
// compares the JSON-encoded results; if they are equal, no other fields
// changed.
func isOnlyLockAnnotationChange(oldRaw, newRaw []byte) (bool, error) {
	var oldObj, newObj map[string]interface{}
	if err := json.Unmarshal(oldRaw, &oldObj); err != nil {
		return false, fmt.Errorf("unmarshalling old object: %w", err)
	}
	if err := json.Unmarshal(newRaw, &newObj); err != nil {
		return false, fmt.Errorf("unmarshalling new object: %w", err)
	}

	stripLockAnnotation(oldObj)
	stripLockAnnotation(newObj)

	oldJSON, err := json.Marshal(oldObj)
	if err != nil {
		return false, fmt.Errorf("marshaling old object: %w", err)
	}
	newJSON, err := json.Marshal(newObj)
	if err != nil {
		return false, fmt.Errorf("marshaling new object: %w", err)
	}
	return bytes.Equal(oldJSON, newJSON), nil
}

// stripLockAnnotation removes the earlywatch.io/lock annotation from an
// unmarshalled object's metadata in place.  If the annotations map becomes
// empty after removal, the "annotations" key is also deleted so that the
// comparison in isOnlyLockAnnotationChange is not skewed by an empty map
// versus an absent key.
func stripLockAnnotation(obj map[string]interface{}) {
	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	annotations, ok := metadata["annotations"].(map[string]interface{})
	if !ok {
		return
	}
	delete(annotations, ewv1alpha1.LockAnnotation)
	if len(annotations) == 0 {
		delete(metadata, "annotations")
	}
}

// evaluateExpression evaluates a CEL expression check against the admission
// request.  This is a simplified implementation that currently only supports
// checking the operation field.  A production implementation would use the
// k8s.io/apiserver/pkg/cel package.
func evaluateExpression(check ewv1alpha1.ExpressionCheck, message string, req admission.Request) (bool, string, error) {
	// Simplified expression evaluation: check common patterns.
	// For a production implementation, wire in the CEL runtime.
	expr := strings.TrimSpace(check.Expression)

	// Build a minimal context map for evaluation.
	ctx := map[string]interface{}{
		"operation": string(req.Operation),
		"namespace": req.Namespace,
		"name":      req.Name,
	}

	result, err := evalSimpleExpression(expr, ctx)
	if err != nil {
		return false, "", fmt.Errorf("evaluating expression %q: %w", expr, err)
	}
	if result {
		return true, renderMessage(message, req), nil
	}
	return false, "", nil
}

// evalSimpleExpression provides basic expression evaluation for common
// admission request checks.  This is intentionally minimal; replace with
// a CEL engine for production use.
func evalSimpleExpression(expr string, ctx map[string]interface{}) (bool, error) {
	// Support simple equality checks of the form: key == 'value'
	eqParts := strings.SplitN(expr, "==", 2)
	if len(eqParts) == 2 {
		key := strings.TrimSpace(eqParts[0])
		val := strings.Trim(strings.TrimSpace(eqParts[1]), "'\"")
		actual, ok := ctx[key]
		if !ok {
			return false, fmt.Errorf("unknown field %q in expression", key)
		}
		return fmt.Sprintf("%v", actual) == val, nil
	}
	return false, fmt.Errorf("unsupported expression syntax: %q; only 'field == value' is supported", expr)
}

// defaultApprovalAnnotation is the annotation key used when ApprovalCheck.AnnotationKey is empty.
const defaultApprovalAnnotation = "earlywatch.io/approved"

// ResourcePath returns the canonical path string for a Kubernetes resource,
// used as the message that must be signed for an approval annotation.
//
// Format:
//
//	<group>/<version>/namespaces/<namespace>/<resource>/<name>   (namespaced, named group)
//	<version>/namespaces/<namespace>/<resource>/<name>           (namespaced, core group)
//	<group>/<version>/<resource>/<name>                          (cluster-scoped, named group)
//	<version>/<resource>/<name>                                  (cluster-scoped, core group)
func ResourcePath(group, version, resource, namespace, name string) string {
	prefix := version
	if group != "" {
		prefix = group + "/" + version
	}
	if namespace != "" {
		return fmt.Sprintf("%s/namespaces/%s/%s/%s", prefix, namespace, resource, name)
	}
	return fmt.Sprintf("%s/%s/%s", prefix, resource, name)
}

// evaluateApprovalCheck verifies that the resource being acted on carries a
// valid approval annotation.  The annotation value must be the base64-encoded
// RSA-PSS SHA-256 signature of the resource's canonical path (as returned by
// ResourcePath), signed with the private key corresponding to the public key
// configured in the rule.
//
// The check is violated (returns true) when:
//   - the annotation is absent, or
//   - the annotation value is not a valid base64-encoded signature.
func evaluateApprovalCheck(check ewv1alpha1.ApprovalCheck, message string, req admission.Request) (bool, string, error) {
	// Parse the public key.
	pubKey, err := parseRSAPublicKey(check.PublicKey)
	if err != nil {
		return false, "", fmt.Errorf("parsing public key: %w", err)
	}

	// Determine annotation key.
	annotationKey := check.AnnotationKey
	if annotationKey == "" {
		annotationKey = defaultApprovalAnnotation
	}

	// For DELETE requests the object being deleted is in OldObject.
	raw := req.Object.Raw
	if len(raw) == 0 {
		raw = req.OldObject.Raw
	}

	// Extract annotations from the raw object.
	var meta struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return false, "", fmt.Errorf("unmarshalling object metadata: %w", err)
	}

	sigB64, ok := meta.Metadata.Annotations[annotationKey]
	if !ok || sigB64 == "" {
		if message == "" {
			message = fmt.Sprintf("resource must carry a valid approval annotation %q before this operation is permitted", annotationKey)
		}
		return true, message, nil
	}

	// Decode the base64 signature.
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		if message == "" {
			message = fmt.Sprintf("approval annotation %q contains an invalid base64 value", annotationKey)
		}
		return true, message, nil
	}

	// Compute the expected resource path.
	path := ResourcePath(
		req.Resource.Group,
		req.Resource.Version,
		req.Resource.Resource,
		req.Namespace,
		req.Name,
	)

	// Verify the RSA-PSS SHA-256 signature.
	digest := sha256.Sum256([]byte(path))
	if err := rsa.VerifyPSS(pubKey, crypto.SHA256, digest[:], sig, nil); err != nil {
		if message == "" {
			message = fmt.Sprintf("approval annotation %q contains an invalid signature for resource path %q", annotationKey, path)
		}
		return true, message, nil
	}

	return false, "", nil
}

// evaluateAnnotationCheck denies the admission request unless the subject
// resource carries the required annotation (and, when AnnotationValue is set,
// the annotation has exactly that value).
//
// For DELETE requests the object being deleted is available in OldObject
// rather than Object, so both are inspected.
func evaluateAnnotationCheck(check ewv1alpha1.AnnotationCheck, message string, req admission.Request) (bool, string, error) {
	// Prefer Object; fall back to OldObject for DELETE requests.
	raw := req.Object.Raw
	if len(raw) == 0 {
		raw = req.OldObject.Raw
	}
	if len(raw) == 0 {
		// No object data available – treat as annotation absent.
		return true, message, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, "", fmt.Errorf("unmarshalling object for annotation check: %w", err)
	}

	// Navigate metadata.annotations.
	metadataRaw, ok := obj["metadata"]
	if !ok || metadataRaw == nil {
		// Object has no metadata; treat annotations as absent.
		return true, message, nil
	}
	metadata, ok := metadataRaw.(map[string]interface{})
	if !ok {
		return false, "", fmt.Errorf("object metadata is not a map (got %T)", metadataRaw)
	}

	annotationsRaw, ok := metadata["annotations"]
	if !ok || annotationsRaw == nil {
		// No annotations present; treat annotation as absent.
		return true, message, nil
	}
	annotations, ok := annotationsRaw.(map[string]interface{})
	if !ok {
		return false, "", fmt.Errorf("object metadata.annotations is not a map (got %T)", annotationsRaw)
	}

	val, present := annotations[check.AnnotationKey]
	if !present {
		return true, message, nil
	}

	if check.AnnotationValue != nil {
		valStr, _ := val.(string)
		if valStr != *check.AnnotationValue {
			return true, message, nil
		}
	}

	return false, "", nil
}

// evaluateManualTouchCheck denies the request when a ManualTouchEvent has been
// recorded for the same resource within the configured look-back window.
// This prevents automated pipelines from overwriting an operator's manual change.
func (h *AdmissionHandler) evaluateManualTouchCheck(
	ctx context.Context,
	check ewv1alpha1.ManualTouchCheck,
	message string,
	req admission.Request,
) (bool, string, error) {
	// Parse the window duration; default to 1 hour.
	windowDuration := time.Hour
	if check.WindowDuration != "" {
		d, err := time.ParseDuration(check.WindowDuration)
		if err != nil {
			return false, "", fmt.Errorf("invalid windowDuration %q: %w", check.WindowDuration, err)
		}
		windowDuration = d
	}

	eventNamespace := check.EventNamespace
	if eventNamespace == "" {
		eventNamespace = auditmonitor.DefaultEventNamespace
	}

	// List ManualTouchEvents for this resource using label selectors.
	labelSel := labels.SelectorFromSet(labels.Set{
		auditmonitor.LabelResource:          req.Resource.Resource,
		auditmonitor.LabelResourceNamespace: req.Namespace,
		auditmonitor.LabelResourceName:      req.Name,
		auditmonitor.LabelAPIGroup:          req.Resource.Group,
	})

	mteList := &ewv1alpha1.ManualTouchEventList{}
	if err := h.Client.List(ctx, mteList, &client.ListOptions{
		Namespace:     eventNamespace,
		LabelSelector: labelSel,
	}); err != nil {
		return false, "", fmt.Errorf("listing ManualTouchEvents: %w", err)
	}

	cutoff := time.Now().Add(-windowDuration)

	for _, mte := range mteList.Items {
		if mte.Spec.Timestamp.After(cutoff) {
			return true, renderMessage(message, req), nil
		}
	}

	return false, "", nil
}

// parseRSAPublicKey decodes a PEM-encoded RSA public key in PKIX
// (SubjectPublicKeyInfo) format.
func parseRSAPublicKey(pemData string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in public key data")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing PKIX public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not an RSA key")
	}
	return rsaPub, nil
}

// evaluateServicePodSelectorCheck denies an UPDATE to a Service when the
// service previously selected at least one Pod but would select no Pods after
// the change.  The check is a no-op for non-UPDATE requests and for headless
// services (spec.clusterIP == "None").
func (h *AdmissionHandler) evaluateServicePodSelectorCheck(
	ctx context.Context,
	message string,
	req admission.Request,
) (bool, string, error) {
	// Only applicable to UPDATE operations.
	if req.Operation != admissionv1.Update {
		return false, "", nil
	}

	oldRaw := req.OldObject.Raw
	if len(oldRaw) == 0 {
		return false, "", nil
	}

	// Parse the old service to get its selector and clusterIP.
	oldSelector, oldClusterIP, err := parseServiceSelectorAndClusterIP(oldRaw)
	if err != nil {
		return false, "", fmt.Errorf("parsing old service object: %w", err)
	}

	// If the old service had no selector (nil) it could not have matched any
	// Pods, so there is nothing to protect.  Note: an empty map ({}) is a
	// valid selector that matches all Pods and is handled below.
	if oldSelector == nil {
		return false, "", nil
	}

	// Headless services are exempt from this check.
	if oldClusterIP == "None" {
		return false, "", nil
	}

	// Check whether the old service actually had matching Pods.
	oldHasPods, err := h.serviceHasMatchingPods(ctx, req.Namespace, oldSelector)
	if err != nil {
		return false, "", fmt.Errorf("checking pods for old service selector: %w", err)
	}
	if !oldHasPods {
		// Previously no matching Pods – the change is safe.
		return false, "", nil
	}

	// Old service had matching Pods.  Check if the new service also selects Pods.
	newRaw := req.Object.Raw
	if len(newRaw) == 0 {
		return false, "", nil
	}

	newSelector, _, err := parseServiceSelectorAndClusterIP(newRaw)
	if err != nil {
		return false, "", fmt.Errorf("parsing new service object: %w", err)
	}

	// Absent selector (nil) on the new service means no Pods will be selected.
	// An empty map ({}) is a valid selector that matches all Pods, so it is
	// not treated as absent.
	if newSelector == nil {
		return true, renderMessage(message, req), nil
	}

	newHasPods, err := h.serviceHasMatchingPods(ctx, req.Namespace, newSelector)
	if err != nil {
		return false, "", fmt.Errorf("checking pods for new service selector: %w", err)
	}
	if !newHasPods {
		return true, renderMessage(message, req), nil
	}

	return false, "", nil
}

// serviceHasMatchingPods reports whether any Pods in the given namespace match
// the provided label selector map.
func (h *AdmissionHandler) serviceHasMatchingPods(
	ctx context.Context,
	namespace string,
	selectorMap map[string]string,
) (bool, error) {
	sel := labels.SelectorFromSet(labels.Set(selectorMap))
	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}
	result, err := h.DynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: sel.String(),
		Limit:         1,
	})
	if err != nil {
		return false, fmt.Errorf("listing Pods: %w", err)
	}
	return len(result.Items) > 0, nil
}

// parseServiceSelectorAndClusterIP extracts spec.selector (as a
// map[string]string) and spec.clusterIP from a raw Service JSON object.
// It returns nil selector and empty clusterIP when the fields are absent.
func parseServiceSelectorAndClusterIP(raw []byte) (map[string]string, string, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, "", fmt.Errorf("unmarshalling service: %w", err)
	}

	spec, _ := obj["spec"].(map[string]interface{})
	if spec == nil {
		return nil, "", nil
	}

	clusterIP, _ := spec["clusterIP"].(string)

	selectorRaw, ok := spec["selector"]
	if !ok || selectorRaw == nil {
		return nil, clusterIP, nil
	}

	selectorMap, ok := selectorRaw.(map[string]interface{})
	if !ok {
		return nil, clusterIP, fmt.Errorf("spec.selector is not a map")
	}

	result := make(map[string]string, len(selectorMap))
	for k, v := range selectorMap {
		str, ok := v.(string)
		if !ok {
			return nil, clusterIP, fmt.Errorf("selector value for key %q is not a string", k)
		}
		result[k] = str
	}

	return result, clusterIP, nil
}
