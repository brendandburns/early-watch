// Package webhook implements the EarlyWatch admission webhook handler.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// AdmissionHandler handles admission webhook requests by evaluating
// ChangeValidator rules registered in the cluster.
type AdmissionHandler struct {
	Client        client.Client
	DynamicClient dynamic.Interface
	Decoder       *admission.Decoder
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
