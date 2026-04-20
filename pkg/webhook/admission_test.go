package webhook

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// makeRequest builds an admission.Request for testing.
func makeRequest(operation admissionv1.Operation, group, resource, namespace, name string, obj interface{}) admission.Request {
	var rawObj []byte
	if obj != nil {
		var err error
		rawObj, err = json.Marshal(obj)
		if err != nil {
			panic(err)
		}
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: operation,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  "v1",
				Resource: resource,
			},
			Namespace: namespace,
			Name:      name,
			Object:    runtime.RawExtension{Raw: rawObj},
		},
	}
}

// --- appliesToRequest tests ---

func TestAppliesToRequest_Match(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "services",
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	if !appliesToRequest(guard, req) {
		t.Error("expected guard to apply to DELETE services request")
	}
}

func TestAppliesToRequest_WrongResource(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "services",
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeRequest(admissionv1.Delete, "", "pods", "default", "my-pod", nil)
	if appliesToRequest(guard, req) {
		t.Error("guard should NOT apply to a pods request")
	}
}

func TestAppliesToRequest_WrongOperation(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "services",
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeRequest(admissionv1.Update, "", "services", "default", "my-svc", nil)
	if appliesToRequest(guard, req) {
		t.Error("guard should NOT apply to an UPDATE request when only DELETE is listed")
	}
}

func TestAppliesToRequest_WrongAPIGroup(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "apps",
				Resource: "deployments",
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeRequest(admissionv1.Delete, "", "deployments", "default", "my-deploy", nil)
	if appliesToRequest(guard, req) {
		t.Error("guard should NOT apply: API group mismatch")
	}
}

// --- selectorFromField tests ---

func TestSelectorFromField_SimpleSelector(t *testing.T) {
	svcObj := map[string]interface{}{
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"app": "my-app",
				"env": "prod",
			},
		},
	}
	raw, _ := json.Marshal(svcObj)

	sel, err := selectorFromField(raw, "spec.selector")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lbls := map[string]string{"app": "my-app", "env": "prod"}
	if !sel.Matches(labelsFromMap(lbls)) {
		t.Error("selector should match the labels")
	}
}

func TestSelectorFromField_MissingField(t *testing.T) {
	svcObj := map[string]interface{}{
		"spec": map[string]interface{}{},
	}
	raw, _ := json.Marshal(svcObj)

	_, err := selectorFromField(raw, "spec.selector")
	if err == nil {
		t.Error("expected error for missing field")
	}
}

func TestSelectorFromField_NonMapValue(t *testing.T) {
	svcObj := map[string]interface{}{
		"spec": map[string]interface{}{
			"selector": "not-a-map",
		},
	}
	raw, _ := json.Marshal(svcObj)

	_, err := selectorFromField(raw, "spec.selector")
	if err == nil {
		t.Error("expected error when field value is not a map")
	}
}

// --- evalSimpleExpression tests ---

func TestEvalSimpleExpression_MatchOperation(t *testing.T) {
	ctx := map[string]interface{}{"operation": "DELETE"}
	result, err := evalSimpleExpression("operation == 'DELETE'", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected expression to evaluate to true")
	}
}

func TestEvalSimpleExpression_NoMatch(t *testing.T) {
	ctx := map[string]interface{}{"operation": "CREATE"}
	result, err := evalSimpleExpression("operation == 'DELETE'", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected expression to evaluate to false")
	}
}

func TestEvalSimpleExpression_UnknownField(t *testing.T) {
	ctx := map[string]interface{}{"operation": "DELETE"}
	_, err := evalSimpleExpression("namespace == 'default'", ctx)
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestEvalSimpleExpression_UnsupportedSyntax(t *testing.T) {
	ctx := map[string]interface{}{"operation": "DELETE"}
	_, err := evalSimpleExpression("operation != 'DELETE'", ctx)
	if err == nil {
		t.Error("expected error for unsupported expression syntax")
	}
}

func TestSelectorFromField_NonObjectIntermediate(t *testing.T) {
	// "spec" is a string, not an object, so navigating into "spec.selector" should fail.
	svcObj := map[string]interface{}{
		"spec": "not-an-object",
	}
	raw, _ := json.Marshal(svcObj)

	_, err := selectorFromField(raw, "spec.selector")
	if err == nil {
		t.Error("expected error when intermediate field segment is not an object")
	}
}

// --- evaluateExpression tests ---

func TestEvaluateExpression_Violated(t *testing.T) {
	check := ewv1alpha1.ExpressionCheck{Expression: "operation == 'DELETE'"}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	violated, msg, err := evaluateExpression(check, "deletion denied", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected expression to be violated")
	}
	if msg != "deletion denied" {
		t.Errorf("expected message %q, got %q", "deletion denied", msg)
	}
}

func TestEvaluateExpression_NotViolated(t *testing.T) {
	check := ewv1alpha1.ExpressionCheck{Expression: "operation == 'DELETE'"}
	req := makeRequest(admissionv1.Create, "", "services", "default", "my-svc", nil)

	violated, _, err := evaluateExpression(check, "deletion denied", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected expression NOT to be violated")
	}
}

func TestEvaluateExpression_Error(t *testing.T) {
	check := ewv1alpha1.ExpressionCheck{Expression: "operation != 'DELETE'"}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	_, _, err := evaluateExpression(check, "msg", req)
	if err == nil {
		t.Error("expected error for unsupported expression syntax")
	}
}

// --- evaluateRule tests ---

func TestEvaluateRule_ExpressionCheck_Violated(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:    "deny-delete",
		Type:    ewv1alpha1.RuleTypeExpressionCheck,
		Message: "delete not allowed",
		ExpressionCheck: &ewv1alpha1.ExpressionCheck{
			Expression: "operation == 'DELETE'",
		},
	}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	violated, msg, err := h.evaluateRule(context.Background(), rule, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected rule to be violated")
	}
	if msg != "delete not allowed" {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestEvaluateRule_ExpressionCheck_NotViolated(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:    "deny-delete",
		Type:    ewv1alpha1.RuleTypeExpressionCheck,
		Message: "delete not allowed",
		ExpressionCheck: &ewv1alpha1.ExpressionCheck{
			Expression: "operation == 'DELETE'",
		},
	}
	req := makeRequest(admissionv1.Create, "", "services", "default", "my-svc", nil)

	violated, _, err := h.evaluateRule(context.Background(), rule, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected rule NOT to be violated")
	}
}

func TestEvaluateRule_NilExpressionCheck(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:            "bad-rule",
		Type:            ewv1alpha1.RuleTypeExpressionCheck,
		Message:         "msg",
		ExpressionCheck: nil,
	}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for nil ExpressionCheck config")
	}
}

func TestEvaluateRule_NilExistingResources(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:              "bad-rule",
		Type:              ewv1alpha1.RuleTypeExistingResources,
		Message:           "msg",
		ExistingResources: nil,
	}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for nil ExistingResources config")
	}
}

func TestEvaluateRule_UnknownType(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:    "bad-rule",
		Type:    "UnknownType",
		Message: "msg",
	}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for unknown rule type")
	}
}

// --- Handle integration tests ---

// newHandlerScheme returns a runtime.Scheme with both earlywatch and core types registered.
func newHandlerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := ewv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme (earlywatch): %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme (corev1): %v", err)
	}
	return s
}

func TestHandle_NoGuards(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected allowed when no guards exist, got: %v", resp.Result)
	}
}

func TestHandle_ExpressionCheckDenied(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "deny-delete",
					Type:    ewv1alpha1.RuleTypeExpressionCheck,
					Message: "services cannot be deleted",
					ExpressionCheck: &ewv1alpha1.ExpressionCheck{
						Expression: "operation == 'DELETE'",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected request to be denied by ExpressionCheck rule")
	}
}

func TestHandle_ExpressionCheckAllowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "deny-delete",
					Type:    ewv1alpha1.RuleTypeExpressionCheck,
					Message: "services cannot be deleted",
					ExpressionCheck: &ewv1alpha1.ExpressionCheck{
						Expression: "operation == 'DELETE'",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	// CREATE is not in the guard's Operations list, so the guard does not apply.
	req := makeRequest(admissionv1.Create, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected allowed for CREATE (guard only covers DELETE): %v", resp.Result)
	}
}

func TestHandle_ExistingResourcesDenied(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
		},
	}
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "pods-still-running",
					Type:    ewv1alpha1.RuleTypeExistingResources,
					Message: "pods are still running",
					ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
						APIGroup: "",
						Resource: "pods",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected request to be denied because pods exist in namespace")
	}
}

func TestHandle_ExistingResourcesAllowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "pods-still-running",
					Type:    ewv1alpha1.RuleTypeExistingResources,
					Message: "pods are still running",
					ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
						APIGroup: "",
						Resource: "pods",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme) // no pods

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected allowed when no pods exist: %v", resp.Result)
	}
}

// makeDeleteRequest builds an admission.Request for a DELETE operation on a
// cluster-scoped resource (e.g. a Namespace).  The object being deleted is
// placed in OldObject because Object is nil for DELETE requests.
func makeDeleteRequest(group, resource, name string, obj interface{}) admission.Request {
	var rawObj []byte
	if obj != nil {
		var err error
		rawObj, err = json.Marshal(obj)
		if err != nil {
			panic(err)
		}
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  "v1",
				Resource: resource,
			},
			// Namespace is empty for cluster-scoped resources.
			Name:      name,
			OldObject: runtime.RawExtension{Raw: rawObj},
		},
	}
}

// namespaceObj returns a minimal Namespace object suitable for serialization.
func namespaceObj(name string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": name},
	}
}

// --- namespace deletion tests ---

// newNamespaceDeletionGuard builds a ChangeValidator that prevents deletion of
// namespaces while they still contain pods.  When names is non-empty the guard
// is restricted to only those named namespaces; when empty it applies to all.
func newNamespaceDeletionGuard(names []string) *ewv1alpha1.ChangeValidator {
	return &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "prevent-nonempty-ns-delete"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "namespaces",
				Names:    names,
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "namespace-must-be-empty",
					Type:    ewv1alpha1.RuleTypeExistingResources,
					Message: "namespace cannot be deleted because it still contains pods",
					ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
						APIGroup: "",
						Resource: "pods",
						// SameNamespace defaults to true; for namespace deletion
						// the handler will use req.Name as the namespace scope.
					},
				},
			},
		},
	}
}

// TestHandle_NamespaceDeletion_DeniedWhenNonEmpty verifies that deleting a
// non-empty namespace (one that still contains pods) is denied.
func TestHandle_NamespaceDeletion_DeniedWhenNonEmpty(t *testing.T) {
	scheme := newHandlerScheme(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-pod",
			Namespace: "my-ns",
		},
	}
	guard := newNamespaceDeletionGuard(nil) // applies to all namespaces

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeDeleteRequest("", "namespaces", "my-ns", namespaceObj("my-ns"))
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected namespace DELETE to be denied because the namespace still contains pods")
	}
}

// TestHandle_NamespaceDeletion_AllowedWhenEmpty verifies that deleting an
// empty namespace (no pods) is allowed.
func TestHandle_NamespaceDeletion_AllowedWhenEmpty(t *testing.T) {
	scheme := newHandlerScheme(t)

	guard := newNamespaceDeletionGuard(nil) // applies to all namespaces

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme) // no pods

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeDeleteRequest("", "namespaces", "my-ns", namespaceObj("my-ns"))
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected namespace DELETE to be allowed when namespace is empty: %v", resp.Result)
	}
}

// TestHandle_NamespaceDeletion_SpecificNames_DeniesListedNamespace verifies
// that a guard scoped to specific namespace names denies deletion of those
// namespaces when they are non-empty.
func TestHandle_NamespaceDeletion_SpecificNames_DeniesListedNamespace(t *testing.T) {
	scheme := newHandlerScheme(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-pod",
			Namespace: "protected-ns",
		},
	}
	guard := newNamespaceDeletionGuard([]string{"protected-ns", "also-protected"})

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeDeleteRequest("", "namespaces", "protected-ns", namespaceObj("protected-ns"))
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected DELETE of listed namespace to be denied because it still contains pods")
	}
}

// TestHandle_NamespaceDeletion_SpecificNames_AllowsUnlistedNamespace verifies
// that a guard scoped to specific namespace names does NOT block deletion of
// namespaces that are not in its Names list.
func TestHandle_NamespaceDeletion_SpecificNames_AllowsUnlistedNamespace(t *testing.T) {
	scheme := newHandlerScheme(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-pod",
			Namespace: "other-ns",
		},
	}
	// Guard only protects "protected-ns", not "other-ns".
	guard := newNamespaceDeletionGuard([]string{"protected-ns"})

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)

	h := &AdmissionHandler{
		Client:        fakeClient,
		DynamicClient: fakeDynamic,
	}

	req := makeDeleteRequest("", "namespaces", "other-ns", namespaceObj("other-ns"))
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected DELETE of unlisted namespace to be allowed: %v", resp.Result)
	}
}

// TestAppliesToRequest_NamesFilter_Match verifies that a guard with a Names
// list applies when the request name is in the list.
func TestAppliesToRequest_NamesFilter_Match(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "namespaces",
				Names:    []string{"prod", "staging"},
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeDeleteRequest("", "namespaces", "prod", nil)
	if !appliesToRequest(guard, req) {
		t.Error("expected guard to apply when request name is in Names list")
	}
}

// TestAppliesToRequest_NamesFilter_NoMatch verifies that a guard with a Names
// list does NOT apply when the request name is not in the list.
func TestAppliesToRequest_NamesFilter_NoMatch(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "namespaces",
				Names:    []string{"prod", "staging"},
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeDeleteRequest("", "namespaces", "dev", nil)
	if appliesToRequest(guard, req) {
		t.Error("expected guard NOT to apply when request name is not in Names list")
	}
}

// TestAppliesToRequest_NamesFilter_EmptyMatchesAll verifies that an empty
// Names list (omitted) means the guard applies to all resource names.
func TestAppliesToRequest_NamesFilter_EmptyMatchesAll(t *testing.T) {
	guard := &ewv1alpha1.ChangeValidator{
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "namespaces",
				// Names intentionally omitted – should match everything.
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
		},
	}
	req := makeDeleteRequest("", "namespaces", "any-namespace", nil)
	if !appliesToRequest(guard, req) {
		t.Error("expected guard to apply to all namespaces when Names list is empty")
	}
}

// labelsFromMap is a helper that converts a plain map to a labels.Set.
func labelsFromMap(m map[string]string) labels.Labels {
	return labels.Set(m)
}

// --- nameExistsAtPath tests ---

func TestNameExistsAtPath_SimpleMatch(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"configMap": map[string]interface{}{
				"name": "my-cm",
			},
		},
	}
	if !nameExistsAtPath(obj, []string{"spec", "configMap", "name"}, "my-cm") {
		t.Error("expected name to be found at path")
	}
}

func TestNameExistsAtPath_NoMatch(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"configMap": map[string]interface{}{
				"name": "other-cm",
			},
		},
	}
	if nameExistsAtPath(obj, []string{"spec", "configMap", "name"}, "my-cm") {
		t.Error("expected name NOT to be found when value differs")
	}
}

func TestNameExistsAtPath_MissingField(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{},
	}
	if nameExistsAtPath(obj, []string{"spec", "configMap", "name"}, "my-cm") {
		t.Error("expected false for missing field path")
	}
}

func TestNameExistsAtPath_TraversesArray(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"volumes": []interface{}{
				map[string]interface{}{
					"name":     "data-vol",
					"emptyDir": map[string]interface{}{},
				},
				map[string]interface{}{
					"name": "config-vol",
					"configMap": map[string]interface{}{
						"name": "my-cm",
					},
				},
			},
		},
	}
	if !nameExistsAtPath(obj, []string{"spec", "volumes", "configMap", "name"}, "my-cm") {
		t.Error("expected name to be found via array traversal")
	}
}

func TestNameExistsAtPath_NestedArrays(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"name": "app",
					"envFrom": []interface{}{
						map[string]interface{}{
							"configMapRef": map[string]interface{}{
								"name": "my-cm",
							},
						},
					},
				},
			},
		},
	}
	if !nameExistsAtPath(obj, []string{"spec", "containers", "envFrom", "configMapRef", "name"}, "my-cm") {
		t.Error("expected name to be found via nested array traversal")
	}
}

func TestNameExistsAtPath_EmptyArray(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"volumes": []interface{}{},
		},
	}
	if nameExistsAtPath(obj, []string{"spec", "volumes", "configMap", "name"}, "my-cm") {
		t.Error("expected false for empty array")
	}
}

// --- NameReferenceCheck rule evaluateRule tests ---

func TestEvaluateRule_NilNameReferenceCheck(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:               "bad-rule",
		Type:               ewv1alpha1.RuleTypeNameReferenceCheck,
		Message:            "msg",
		NameReferenceCheck: nil,
	}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-cm", nil)

	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for nil NameReferenceCheck config")
	}
}

// newFullHandlerScheme returns a runtime.Scheme with earlywatch, core, apps,
// and batch types registered, which is required for NameReferenceCheck tests.
func newFullHandlerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := newHandlerScheme(t)
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme (appsv1): %v", err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme (batchv1): %v", err)
	}
	return s
}

// newConfigMapDeletionGuard builds a ChangeValidator that prevents deletion of
// a ConfigMap when it is still referenced by Deployments, DaemonSets, or CronJobs.
func newConfigMapDeletionGuard() *ewv1alpha1.ChangeValidator {
	return &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "protect-configmap", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "configmaps",
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "configmap-not-referenced-by-workloads",
					Type:    ewv1alpha1.RuleTypeNameReferenceCheck,
					Message: "ConfigMap is still in use",
					NameReferenceCheck: &ewv1alpha1.NameReferenceCheck{
						Resources: []ewv1alpha1.NameReferenceResource{
							{
								APIGroup: "apps",
								Resource: "deployments",
								Version:  "v1",
								NameFields: []string{
									"spec.template.spec.volumes.configMap.name",
									"spec.template.spec.containers.envFrom.configMapRef.name",
									"spec.template.spec.containers.env.valueFrom.configMapKeyRef.name",
								},
							},
							{
								APIGroup: "apps",
								Resource: "daemonsets",
								Version:  "v1",
								NameFields: []string{
									"spec.template.spec.volumes.configMap.name",
									"spec.template.spec.containers.envFrom.configMapRef.name",
								},
							},
							{
								APIGroup: "batch",
								Resource: "cronjobs",
								Version:  "v1",
								NameFields: []string{
									"spec.jobTemplate.spec.template.spec.volumes.configMap.name",
									"spec.jobTemplate.spec.template.spec.containers.envFrom.configMapRef.name",
								},
							},
						},
					},
				},
			},
		},
	}
}

// TestHandle_NameReferenceCheck_DeniedWhenDeploymentReferencesViaVolume verifies
// that deleting a ConfigMap is denied when a Deployment mounts it as a volume.
func TestHandle_NameReferenceCheck_DeniedWhenDeploymentReferencesViaVolume(t *testing.T) {
	scheme := newFullHandlerScheme(t)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "config-vol",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
								},
							},
						},
					},
				},
			},
		},
	}
	guard := newConfigMapDeletionGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, deploy)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-configmap", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected ConfigMap DELETE to be denied because a Deployment mounts it as a volume")
	}
}

// TestHandle_NameReferenceCheck_DeniedWhenDeploymentReferencesViaEnvFrom verifies
// that deleting a ConfigMap is denied when a Deployment injects it via envFrom.
func TestHandle_NameReferenceCheck_DeniedWhenDeploymentReferencesViaEnvFrom(t *testing.T) {
	scheme := newFullHandlerScheme(t)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "app:latest",
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	guard := newConfigMapDeletionGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, deploy)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-configmap", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected ConfigMap DELETE to be denied because a Deployment references it via envFrom")
	}
}

// TestHandle_NameReferenceCheck_DeniedWhenDaemonSetReferences verifies that
// deleting a ConfigMap is denied when a DaemonSet references it.
func TestHandle_NameReferenceCheck_DeniedWhenDaemonSetReferences(t *testing.T) {
	scheme := newFullHandlerScheme(t)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ds", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "config-vol",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
								},
							},
						},
					},
				},
			},
		},
	}
	guard := newConfigMapDeletionGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, ds)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-configmap", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected ConfigMap DELETE to be denied because a DaemonSet mounts it as a volume")
	}
}

// TestHandle_NameReferenceCheck_DeniedWhenCronJobReferences verifies that
// deleting a ConfigMap is denied when a CronJob references it.
func TestHandle_NameReferenceCheck_DeniedWhenCronJobReferences(t *testing.T) {
	scheme := newFullHandlerScheme(t)
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cj", Namespace: "default"},
		Spec: batchv1.CronJobSpec{
			Schedule: "*/5 * * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "job",
									Image: "job:latest",
									EnvFrom: []corev1.EnvFromSource{
										{
											ConfigMapRef: &corev1.ConfigMapEnvSource{
												LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	guard := newConfigMapDeletionGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, cj)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-configmap", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected ConfigMap DELETE to be denied because a CronJob references it via envFrom")
	}
}

// TestHandle_NameReferenceCheck_AllowedWhenNotReferenced verifies that
// deleting a ConfigMap is allowed when no workloads reference it.
func TestHandle_NameReferenceCheck_AllowedWhenNotReferenced(t *testing.T) {
	scheme := newFullHandlerScheme(t)
	// Deployment references a different ConfigMap.
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "other-vol",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "other-configmap"},
								},
							},
						},
					},
				},
			},
		},
	}
	guard := newConfigMapDeletionGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, deploy)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-configmap", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected ConfigMap DELETE to be allowed when no workload references it: %v", resp.Result)
	}
}

// TestHandle_NameReferenceCheck_AllowedWhenNoWorkloads verifies that deleting
// a ConfigMap is allowed when there are no workloads at all.
func TestHandle_NameReferenceCheck_AllowedWhenNoWorkloads(t *testing.T) {
	scheme := newFullHandlerScheme(t)
	guard := newConfigMapDeletionGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme) // no workloads

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Delete, "", "configmaps", "default", "my-configmap", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected ConfigMap DELETE to be allowed when no workloads exist: %v", resp.Result)
	}
}

// --- AnnotationCheck tests ---

// namespaceObjWithAnnotations returns a minimal Namespace object with the
// provided annotations, suitable for serialization into an admission request.
func namespaceObjWithAnnotations(name string, annotations map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name":        name,
			"annotations": annotations,
		},
	}
}

// strPtr returns a pointer to the given string value.
func strPtr(s string) *string { return &s }

// TestEvaluateAnnotationCheck_AnnotationAbsent verifies that the check is
// violated when the required annotation is missing from the object.
func TestEvaluateAnnotationCheck_AnnotationAbsent(t *testing.T) {
	check := ewv1alpha1.AnnotationCheck{
		AnnotationKey:   "earlywatch.io/confirm-delete",
		AnnotationValue: strPtr("true"),
	}
	obj := namespaceObj("kube-system") // no annotations
	req := makeDeleteRequest("", "namespaces", "kube-system", obj)

	violated, msg, err := evaluateAnnotationCheck(check, "confirm annotation required", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected check to be violated when annotation is absent")
	}
	if msg != "confirm annotation required" {
		t.Errorf("unexpected message: %q", msg)
	}
}

// TestEvaluateAnnotationCheck_AnnotationPresentCorrectValue verifies that the
// check is NOT violated when the annotation key and value both match.
func TestEvaluateAnnotationCheck_AnnotationPresentCorrectValue(t *testing.T) {
	check := ewv1alpha1.AnnotationCheck{
		AnnotationKey:   "earlywatch.io/confirm-delete",
		AnnotationValue: strPtr("true"),
	}
	obj := namespaceObjWithAnnotations("kube-system", map[string]string{
		"earlywatch.io/confirm-delete": "true",
	})
	req := makeDeleteRequest("", "namespaces", "kube-system", obj)

	violated, _, err := evaluateAnnotationCheck(check, "confirm annotation required", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected check NOT to be violated when annotation key and value match")
	}
}

// TestEvaluateAnnotationCheck_AnnotationPresentWrongValue verifies that the
// check is violated when the annotation key is present but the value differs.
func TestEvaluateAnnotationCheck_AnnotationPresentWrongValue(t *testing.T) {
	check := ewv1alpha1.AnnotationCheck{
		AnnotationKey:   "earlywatch.io/confirm-delete",
		AnnotationValue: strPtr("true"),
	}
	obj := namespaceObjWithAnnotations("kube-system", map[string]string{
		"earlywatch.io/confirm-delete": "yes", // wrong value
	})
	req := makeDeleteRequest("", "namespaces", "kube-system", obj)

	violated, _, err := evaluateAnnotationCheck(check, "confirm annotation required", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected check to be violated when annotation value does not match")
	}
}

// TestEvaluateAnnotationCheck_NoValueRequired verifies that when AnnotationValue
// is nil, any annotation value (including empty string) satisfies the check.
func TestEvaluateAnnotationCheck_NoValueRequired(t *testing.T) {
	check := ewv1alpha1.AnnotationCheck{
		AnnotationKey: "earlywatch.io/confirm-delete",
		// AnnotationValue intentionally omitted.
	}
	obj := namespaceObjWithAnnotations("kube-system", map[string]string{
		"earlywatch.io/confirm-delete": "anything",
	})
	req := makeDeleteRequest("", "namespaces", "kube-system", obj)

	violated, _, err := evaluateAnnotationCheck(check, "confirm annotation required", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected check NOT to be violated when annotation is present and no specific value is required")
	}
}

// TestEvaluateAnnotationCheck_NoObjectData verifies that the check is violated
// when neither Object nor OldObject carries any raw data.
func TestEvaluateAnnotationCheck_NoObjectData(t *testing.T) {
	check := ewv1alpha1.AnnotationCheck{
		AnnotationKey:   "earlywatch.io/confirm-delete",
		AnnotationValue: strPtr("true"),
	}
	req := makeDeleteRequest("", "namespaces", "kube-system", nil) // no object data

	violated, _, err := evaluateAnnotationCheck(check, "confirm annotation required", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected check to be violated when no object data is available")
	}
}

// TestEvaluateRule_NilAnnotationCheck verifies that evaluateRule returns an
// error when the rule type is AnnotationCheck but no config is provided.
func TestEvaluateRule_NilAnnotationCheck(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:            "bad-rule",
		Type:            ewv1alpha1.RuleTypeAnnotationCheck,
		Message:         "msg",
		AnnotationCheck: nil,
	}
	req := makeDeleteRequest("", "namespaces", "kube-system", nil)

	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for nil AnnotationCheck config")
	}
}

// newKubeSystemAnnotationGuard builds a ChangeValidator that requires the
// earlywatch.io/confirm-delete=true annotation before kube-system can be deleted.
func newKubeSystemAnnotationGuard() *ewv1alpha1.ChangeValidator {
	return &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "protect-kube-system"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "namespaces",
				Names:    []string{"kube-system"},
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "require-confirm-delete-annotation",
					Type:    ewv1alpha1.RuleTypeAnnotationCheck,
					Message: "add earlywatch.io/confirm-delete=true to kube-system before deleting",
					AnnotationCheck: &ewv1alpha1.AnnotationCheck{
						AnnotationKey:   "earlywatch.io/confirm-delete",
						AnnotationValue: strPtr("true"),
					},
				},
			},
		},
	}
}

// TestHandle_AnnotationCheck_KubeSystem_DeniedWithoutAnnotation verifies that
// deleting kube-system is denied when the confirm-delete annotation is absent.
func TestHandle_AnnotationCheck_KubeSystem_DeniedWithoutAnnotation(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := newKubeSystemAnnotationGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeDeleteRequest("", "namespaces", "kube-system", namespaceObj("kube-system"))
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected kube-system DELETE to be denied when confirm-delete annotation is absent")
	}
}

// TestHandle_AnnotationCheck_KubeSystem_AllowedWithAnnotation verifies that
// deleting kube-system is allowed when the confirm-delete annotation is present.
func TestHandle_AnnotationCheck_KubeSystem_AllowedWithAnnotation(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := newKubeSystemAnnotationGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	obj := namespaceObjWithAnnotations("kube-system", map[string]string{
		"earlywatch.io/confirm-delete": "true",
	})
	req := makeDeleteRequest("", "namespaces", "kube-system", obj)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected kube-system DELETE to be allowed when confirm-delete annotation is present: %v", resp.Result)
	}
}

// TestHandle_AnnotationCheck_OtherNamespace_AllowedWithoutAnnotation verifies
// that the kube-system guard does NOT block deletion of other namespaces.
func TestHandle_AnnotationCheck_OtherNamespace_AllowedWithoutAnnotation(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := newKubeSystemAnnotationGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeDeleteRequest("", "namespaces", "default", namespaceObj("default"))
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected DELETE of a non-kube-system namespace to be allowed: %v", resp.Result)
	}
}

// --- renderMessage tests ---

func TestRenderMessage_NoPlaceholders(t *testing.T) {
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	msg := renderMessage("resource cannot be deleted", req)
	if msg != "resource cannot be deleted" {
		t.Errorf("expected message unchanged, got %q", msg)
	}
}

func TestRenderMessage_AllPlaceholders(t *testing.T) {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			Resource: metav1.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
			Namespace: "production",
			Name:      "my-deploy",
		},
	}
	tmpl := "{{operation}} of {{resource}} \"{{name}}\" in namespace \"{{namespace}}\" (group: {{apiGroup}}) denied"
	got := renderMessage(tmpl, req)
	want := `DELETE of deployments "my-deploy" in namespace "production" (group: apps) denied`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRenderMessage_NamePlaceholder(t *testing.T) {
	req := makeRequest(admissionv1.Delete, "", "secrets", "default", "my-secret", nil)
	got := renderMessage(`Secret "{{name}}" cannot be deleted`, req)
	want := `Secret "my-secret" cannot be deleted`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRenderMessage_NamespacePlaceholder(t *testing.T) {
	req := makeRequest(admissionv1.Delete, "", "services", "staging", "svc1", nil)
	got := renderMessage("namespace {{namespace}}", req)
	want := "namespace staging"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestEvaluateExpression_TemplateMessage verifies that the denial message is
// rendered with the admission request context when an ExpressionCheck fires.
func TestEvaluateExpression_TemplateMessage(t *testing.T) {
	check := ewv1alpha1.ExpressionCheck{Expression: "operation == 'DELETE'"}
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)

	violated, msg, err := evaluateExpression(check, `Cannot {{operation}} "{{name}}"`, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Fatal("expected expression to be violated")
	}
	want := `Cannot DELETE "my-svc"`
	if msg != want {
		t.Errorf("expected message %q, got %q", want, msg)
	}
}

// --- CheckLock rule tests ---

// lockedServiceObj returns a minimal Service object with the lock annotation set.
func lockedServiceObj(name, namespace string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"annotations": map[string]interface{}{
				ewv1alpha1.LockAnnotation: "true",
			},
		},
	}
}

// makeDeleteRequestNS builds a namespaced DELETE admission.Request placing the
// object in OldObject (as Kubernetes does for deletes).
func makeDeleteRequestNS(group, resource, namespace, name string, obj interface{}) admission.Request {
	var rawObj []byte
	if obj != nil {
		var err error
		rawObj, err = json.Marshal(obj)
		if err != nil {
			panic(err)
		}
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  "v1",
				Resource: resource,
			},
			Namespace: namespace,
			Name:      name,
			OldObject: runtime.RawExtension{Raw: rawObj},
		},
	}
}

// TestEvaluateCheckLock_AllowedWhenAnnotationEmpty verifies that an empty lock
// annotation value is not treated as a lock.
func TestEvaluateCheckLock_AllowedWhenAnnotationEmpty(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "my-svc",
			"namespace": "default",
			"annotations": map[string]interface{}{
				ewv1alpha1.LockAnnotation: "",
			},
		},
	}
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)

	violated, _, err := evaluateCheckLock(nil, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected CheckLock NOT to be violated when lock annotation value is empty")
	}
}

// TestEvaluateCheckLock_DeniedWhenAnnotationPresent verifies that a DELETE
// request is denied when the object carries the lock annotation.
func TestEvaluateCheckLock_DeniedWhenAnnotationPresent(t *testing.T) {
	obj := lockedServiceObj("my-svc", "default")
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)

	violated, msg, err := evaluateCheckLock(nil, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected CheckLock to be violated when lock annotation is present")
	}
	if msg != "resource is locked" {
		t.Errorf("unexpected message: %q", msg)
	}
}

// TestEvaluateCheckLock_AllowedWhenAnnotationAbsent verifies that a DELETE
// request is allowed when the object does not carry the lock annotation.
func TestEvaluateCheckLock_AllowedWhenAnnotationAbsent(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "my-svc",
			"namespace": "default",
		},
	}
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)

	violated, _, err := evaluateCheckLock(nil, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected CheckLock NOT to be violated when lock annotation is absent")
	}
}

// TestEvaluateCheckLock_AllowedForUpdateWhenLockOnMutateNotSet verifies that an
// UPDATE operation is not blocked when LockOnMutate is not configured.
func TestEvaluateCheckLock_AllowedForUpdateWhenLockOnMutateNotSet(t *testing.T) {
	obj := lockedServiceObj("my-svc", "default")
	raw, _ := json.Marshal(obj)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "my-svc",
			OldObject: runtime.RawExtension{Raw: raw},
		},
	}

	violated, _, err := evaluateCheckLock(nil, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected CheckLock NOT to be violated for UPDATE when LockOnMutate is not set")
	}
}

// TestEvaluateCheckLock_DeniedForUpdateWhenLockOnMutateTrue verifies that an
// UPDATE request is denied when LockOnMutate is true and the object is locked.
func TestEvaluateCheckLock_DeniedForUpdateWhenLockOnMutateTrue(t *testing.T) {
	lockOnMutate := true
	cfg := &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate}

	obj := lockedServiceObj("my-svc", "default")
	raw, _ := json.Marshal(obj)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "my-svc",
			OldObject: runtime.RawExtension{Raw: raw},
		},
	}

	violated, msg, err := evaluateCheckLock(cfg, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected CheckLock to be violated for UPDATE when LockOnMutate is true and lock annotation is set")
	}
	if msg != "resource is locked" {
		t.Errorf("unexpected message: %q", msg)
	}
}

// TestEvaluateCheckLock_AllowedForUpdateWhenLockOnMutateTrueButNotLocked verifies
// that an UPDATE request is allowed when LockOnMutate is true but the object
// does not carry the lock annotation.
func TestEvaluateCheckLock_AllowedForUpdateWhenLockOnMutateTrueButNotLocked(t *testing.T) {
	lockOnMutate := true
	cfg := &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate}

	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "my-svc", "namespace": "default"},
	}
	raw, _ := json.Marshal(obj)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "my-svc",
			OldObject: runtime.RawExtension{Raw: raw},
		},
	}

	violated, _, err := evaluateCheckLock(cfg, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected CheckLock NOT to be violated for UPDATE when LockOnMutate is true but lock annotation is absent")
	}
}

// TestEvaluateCheckLock_AllowedWhenNoObjectData verifies that a DELETE with no
// object data does not error and is treated as not locked.
func TestEvaluateCheckLock_AllowedWhenNoObjectData(t *testing.T) {
	req := makeDeleteRequestNS("", "services", "default", "my-svc", nil)

	violated, _, err := evaluateCheckLock(nil, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected CheckLock NOT to be violated when no object data is present")
	}
}

// TestEvaluateCheckLock_AllowedForUpdateThatOnlyRemovesLock verifies that an
// UPDATE whose only change is removing the earlywatch.io/lock annotation is
// allowed even when LockOnMutate is true.  This is the "unlock" path that
// operators rely on to release a locked resource.
func TestEvaluateCheckLock_AllowedForUpdateThatOnlyRemovesLock(t *testing.T) {
	lockOnMutate := true
	cfg := &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate}

	// Old object is locked.
	oldObj := lockedServiceObj("my-svc", "default")
	// New object is identical except the lock annotation has been removed.
	newObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "my-svc",
			"namespace": "default",
		},
	}
	oldRaw, err := json.Marshal(oldObj)
	if err != nil {
		t.Fatalf("marshaling old object: %v", err)
	}
	newRaw, err := json.Marshal(newObj)
	if err != nil {
		t.Fatalf("marshaling new object: %v", err)
	}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "my-svc",
			OldObject: runtime.RawExtension{Raw: oldRaw},
			Object:    runtime.RawExtension{Raw: newRaw},
		},
	}

	violated, _, err := evaluateCheckLock(cfg, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected CheckLock NOT to be violated when the only change is removing the lock annotation")
	}
}

// TestEvaluateCheckLock_DeniedForUpdateThatChangesMoreThanLock verifies that
// an UPDATE which removes the lock annotation AND changes other fields is
// still denied.
func TestEvaluateCheckLock_DeniedForUpdateThatChangesMoreThanLock(t *testing.T) {
	lockOnMutate := true
	cfg := &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate}

	// Old object is locked.
	oldObj := lockedServiceObj("my-svc", "default")
	// New object removes the lock but also changes another field (spec).
	newObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "my-svc",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"clusterIP": "10.0.0.2",
		},
	}
	oldRaw, err := json.Marshal(oldObj)
	if err != nil {
		t.Fatalf("marshaling old object: %v", err)
	}
	newRaw, err := json.Marshal(newObj)
	if err != nil {
		t.Fatalf("marshaling new object: %v", err)
	}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "my-svc",
			OldObject: runtime.RawExtension{Raw: oldRaw},
			Object:    runtime.RawExtension{Raw: newRaw},
		},
	}

	violated, _, err := evaluateCheckLock(cfg, "resource is locked", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected CheckLock to be violated when the UPDATE changes fields beyond the lock annotation")
	}
}

// TestEvaluateRule_CheckLock_Violated verifies that evaluateRule correctly
// routes CheckLock and returns a violation when the lock annotation is set.
func TestEvaluateRule_CheckLock_Violated(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:    "check-lock",
		Type:    ewv1alpha1.RuleTypeCheckLock,
		Message: "object is locked",
	}
	obj := lockedServiceObj("my-svc", "default")
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)

	violated, msg, err := h.evaluateRule(context.Background(), rule, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected rule to be violated")
	}
	if msg != "object is locked" {
		t.Errorf("unexpected message: %q", msg)
	}
}

// TestEvaluateRule_CheckLock_NotViolated verifies that evaluateRule returns no
// violation when the lock annotation is absent.
func TestEvaluateRule_CheckLock_NotViolated(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:    "check-lock",
		Type:    ewv1alpha1.RuleTypeCheckLock,
		Message: "object is locked",
	}
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "my-svc", "namespace": "default"},
	}
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)

	violated, _, err := h.evaluateRule(context.Background(), rule, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected rule NOT to be violated when lock annotation is absent")
	}
}

// TestHandle_CheckLock_DeniedWhenLocked verifies the full admission pipeline
// rejects a DELETE when a CheckLock guard is registered and the object is locked.
func TestHandle_CheckLock_DeniedWhenLocked(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "lock-guard", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "check-lock",
					Type:    ewv1alpha1.RuleTypeCheckLock,
					Message: "service is locked and cannot be deleted",
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	obj := lockedServiceObj("my-svc", "default")
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected DELETE to be denied because the service carries the lock annotation")
	}
}

// TestHandle_CheckLock_AllowedWhenNotLocked verifies the full admission pipeline
// allows DELETE when no lock annotation is present.
func TestHandle_CheckLock_AllowedWhenNotLocked(t *testing.T) {
	scheme := newHandlerScheme(t)
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "lock-guard", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "check-lock",
					Type:    ewv1alpha1.RuleTypeCheckLock,
					Message: "service is locked and cannot be deleted",
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "my-svc", "namespace": "default"},
	}
	req := makeDeleteRequestNS("", "services", "default", "my-svc", obj)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected DELETE to be allowed when no lock annotation is set: %v", resp.Result)
	}
}

// makeUpdateRequest builds an admission.Request for an UPDATE operation,
// placing the pre-update object in OldObject.
func makeUpdateRequest(group, resource, namespace, name string, oldObj interface{}) admission.Request {
	var rawOld []byte
	if oldObj != nil {
		var err error
		rawOld, err = json.Marshal(oldObj)
		if err != nil {
			panic(err)
		}
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  "v1",
				Resource: resource,
			},
			Namespace: namespace,
			Name:      name,
			OldObject: runtime.RawExtension{Raw: rawOld},
		},
	}
}

// makeUpdateRequestFull builds an admission.Request for an UPDATE operation
// with both OldObject (pre-update) and Object (post-update) populated.
func makeUpdateRequestFull(group, resource, namespace, name string, oldObj, newObj interface{}) admission.Request {
	var rawOld, rawNew []byte
	if oldObj != nil {
		var err error
		rawOld, err = json.Marshal(oldObj)
		if err != nil {
			panic(err)
		}
	}
	if newObj != nil {
		var err error
		rawNew, err = json.Marshal(newObj)
		if err != nil {
			panic(err)
		}
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Resource: metav1.GroupVersionResource{
				Group:    group,
				Version:  "v1",
				Resource: resource,
			},
			Namespace: namespace,
			Name:      name,
			OldObject: runtime.RawExtension{Raw: rawOld},
			Object:    runtime.RawExtension{Raw: rawNew},
		},
	}
}

// TestHandle_CheckLock_LockOnMutate_DeniedWhenLocked verifies the full
// admission pipeline rejects an UPDATE when LockOnMutate is true and the
// current resource carries the lock annotation.
func TestHandle_CheckLock_LockOnMutate_DeniedWhenLocked(t *testing.T) {
	scheme := newHandlerScheme(t)
	lockOnMutate := true
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "lock-guard", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:      "check-lock",
					Type:      ewv1alpha1.RuleTypeCheckLock,
					Message:   "service is locked and cannot be mutated",
					CheckLock: &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	// Old (locked) and new objects both with the lock (not an unlock attempt).
	oldObj := lockedServiceObj("my-svc", "default")
	newObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "my-svc",
			"namespace": "default",
			"annotations": map[string]interface{}{
				ewv1alpha1.LockAnnotation: "true",
			},
		},
		"spec": map[string]interface{}{
			"clusterIP": "10.0.0.2",
		},
	}
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldObj, newObj)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected UPDATE to be denied because the service carries the lock annotation and LockOnMutate is true")
	}
}

// TestHandle_CheckLock_LockOnMutate_AllowedWhenNotLocked verifies that an
// UPDATE is allowed when LockOnMutate is true but the object is not locked.
func TestHandle_CheckLock_LockOnMutate_AllowedWhenNotLocked(t *testing.T) {
	scheme := newHandlerScheme(t)
	lockOnMutate := true
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "lock-guard", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:      "check-lock",
					Type:      ewv1alpha1.RuleTypeCheckLock,
					Message:   "service is locked and cannot be mutated",
					CheckLock: &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "my-svc", "namespace": "default"},
	}
	req := makeUpdateRequest("", "services", "default", "my-svc", obj)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected UPDATE to be allowed when no lock annotation is set: %v", resp.Result)
	}
}

// TestHandle_CheckLock_LockOnMutate_AllowedWhenUnlocking verifies that an
// UPDATE that removes the lock annotation (and changes nothing else) is
// allowed even when LockOnMutate is true.
func TestHandle_CheckLock_LockOnMutate_AllowedWhenUnlocking(t *testing.T) {
	scheme := newHandlerScheme(t)
	lockOnMutate := true
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "lock-guard", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:      "check-lock",
					Type:      ewv1alpha1.RuleTypeCheckLock,
					Message:   "service is locked and cannot be mutated",
					CheckLock: &ewv1alpha1.CheckLockRule{LockOnMutate: &lockOnMutate},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	// Old object carries the lock; new object is identical except the lock is removed.
	oldObj := lockedServiceObj("my-svc", "default")
	newObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "my-svc",
			"namespace": "default",
		},
	}
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldObj, newObj)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected UPDATE to be allowed when the only change is removing the lock annotation: %v", resp.Result)
	}
}

// ---- ManualTouchCheck tests ----

// TestHandle_ManualTouchCheck_DeniedWhenRecentEventExists verifies that an
// automated pipeline change is denied when a recent ManualTouchEvent exists
// for the same resource.
func TestHandle_ManualTouchCheck_DeniedWhenRecentEventExists(t *testing.T) {
	scheme := newHandlerScheme(t)

	// Recent manual touch event for the service.
	mte := &ewv1alpha1.ManualTouchEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mte-001",
			Namespace: "early-watch-system",
			Labels: map[string]string{
				"earlywatch.io/resource":           "services",
				"earlywatch.io/resource-namespace": "default",
				"earlywatch.io/resource-name":      "my-svc",
				"earlywatch.io/api-group":          "",
				"earlywatch.io/operation":          "DELETE",
			},
		},
		Spec: ewv1alpha1.ManualTouchEventSpec{
			Timestamp:         metav1.NewTime(time.Now().Add(-5 * time.Minute)), // 5 min ago
			User:              "kubernetes-admin",
			UserAgent:         "kubectl/v1.29.0",
			Operation:         "DELETE",
			Resource:          "services",
			ResourceName:      "my-svc",
			ResourceNamespace: "default",
			AuditID:           "audit-001",
			MonitorName:       "my-monitor",
			MonitorNamespace:  "default",
		},
	}

	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard-manual-touch", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "no-recent-manual-touch",
					Type:    ewv1alpha1.RuleTypeManualTouchCheck,
					Message: "a recent manual touch was detected; pipeline change denied",
					ManualTouchCheck: &ewv1alpha1.ManualTouchCheck{
						WindowDuration: "1h",
						EventNamespace: "early-watch-system",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(guard, mte).
		Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Update, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected UPDATE to be denied because a recent ManualTouchEvent exists")
	}
}

// TestHandle_ManualTouchCheck_AllowedWhenNoRecentEvent verifies that an
// automated change is allowed when no recent ManualTouchEvent exists.
func TestHandle_ManualTouchCheck_AllowedWhenNoRecentEvent(t *testing.T) {
	scheme := newHandlerScheme(t)

	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard-manual-touch", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "no-recent-manual-touch",
					Type:    ewv1alpha1.RuleTypeManualTouchCheck,
					Message: "a recent manual touch was detected; pipeline change denied",
					ManualTouchCheck: &ewv1alpha1.ManualTouchCheck{
						WindowDuration: "1h",
						EventNamespace: "early-watch-system",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(guard).
		Build() // no ManualTouchEvents
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Update, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected UPDATE to be allowed when no ManualTouchEvent exists: %v", resp.Result)
	}
}

// TestHandle_ManualTouchCheck_AllowedWhenEventIsOutsideWindow verifies that a
// ManualTouchEvent that is older than the configured window does not block the
// automated change.
func TestHandle_ManualTouchCheck_AllowedWhenEventIsOutsideWindow(t *testing.T) {
	scheme := newHandlerScheme(t)

	// Event was 2 hours ago, window is 1 hour.
	mte := &ewv1alpha1.ManualTouchEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mte-old",
			Namespace: "early-watch-system",
			Labels: map[string]string{
				"earlywatch.io/resource":           "services",
				"earlywatch.io/resource-namespace": "default",
				"earlywatch.io/resource-name":      "my-svc",
				"earlywatch.io/api-group":          "",
				"earlywatch.io/operation":          "DELETE",
			},
		},
		Spec: ewv1alpha1.ManualTouchEventSpec{
			Timestamp:         metav1.NewTime(time.Now().Add(-2 * time.Hour)), // 2 hours ago
			User:              "kubernetes-admin",
			UserAgent:         "kubectl/v1.29.0",
			Operation:         "DELETE",
			Resource:          "services",
			ResourceName:      "my-svc",
			ResourceNamespace: "default",
			AuditID:           "audit-002",
			MonitorName:       "my-monitor",
			MonitorNamespace:  "default",
		},
	}

	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "guard-manual-touch", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "no-recent-manual-touch",
					Type:    ewv1alpha1.RuleTypeManualTouchCheck,
					Message: "a recent manual touch was detected; pipeline change denied",
					ManualTouchCheck: &ewv1alpha1.ManualTouchCheck{
						WindowDuration: "1h",
						EventNamespace: "early-watch-system",
					},
				},
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(guard, mte).
		Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}
	req := makeRequest(admissionv1.Update, "", "services", "default", "my-svc", nil)
	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected UPDATE to be allowed when only old ManualTouchEvent exists: %v", resp.Result)
	}
}

// TestEvaluateRule_NilManualTouchCheck verifies that evaluateRule returns an
// error when ManualTouchCheck is the type but the config pointer is nil.
func TestEvaluateRule_NilManualTouchCheck(t *testing.T) {
	h := &AdmissionHandler{}
	rule := ewv1alpha1.GuardRule{
		Name:             "bad-rule",
		Type:             ewv1alpha1.RuleTypeManualTouchCheck,
		Message:          "msg",
		ManualTouchCheck: nil,
	}
	req := makeRequest(admissionv1.Update, "", "services", "default", "my-svc", nil)
	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for nil ManualTouchCheck config")
	}
}

// TestEvaluateManualTouchCheck_InvalidWindowDuration verifies that an invalid
// duration string returns an error.
func TestEvaluateManualTouchCheck_InvalidWindowDuration(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()
	h := &AdmissionHandler{Client: fakeClient}

	check := ewv1alpha1.ManualTouchCheck{WindowDuration: "not-a-duration"}
	req := makeRequest(admissionv1.Update, "", "services", "default", "my-svc", nil)
	_, _, err := h.evaluateManualTouchCheck(context.Background(), check, "msg", req)
	if err == nil {
		t.Error("expected error for invalid window duration")
	}
}

// --- ServicePodSelectorCheck tests ---

// serviceObj builds a minimal Service JSON object for use in admission requests.
// selector may be nil (no selector). clusterIP may be empty (uses default) or
// "None" (headless).
func serviceObj(selector map[string]string, clusterIP string) map[string]interface{} {
	spec := map[string]interface{}{}
	if selector != nil {
		sel := make(map[string]interface{}, len(selector))
		for k, v := range selector {
			sel[k] = v
		}
		spec["selector"] = sel
	}
	if clusterIP != "" {
		spec["clusterIP"] = clusterIP
	}
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "my-svc", "namespace": "default"},
		"spec":       spec,
	}
}

// newServicePodSelectorGuard builds a ChangeValidator that uses the
// ServicePodSelectorCheck rule to protect Service UPDATE operations.
func newServicePodSelectorGuard() *ewv1alpha1.ChangeValidator {
	return &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "protect-svc-selector", Namespace: "default"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:                    "service-must-keep-pods",
					Type:                    ewv1alpha1.RuleTypeServicePodSelectorCheck,
					Message:                 "service selector change would leave no matching pods",
					ServicePodSelectorCheck: &ewv1alpha1.ServicePodSelectorCheck{},
				},
			},
		},
	}
}

// TestEvaluateRule_NilServicePodSelectorCheck verifies that a nil config
// returns an error.
func TestEvaluateRule_NilServicePodSelectorCheck(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	rule := ewv1alpha1.GuardRule{
		Name:                    "bad-rule",
		Type:                    ewv1alpha1.RuleTypeServicePodSelectorCheck,
		Message:                 "msg",
		ServicePodSelectorCheck: nil,
	}
	req := makeUpdateRequestFull("", "services", "default", "my-svc", serviceObj(nil, ""), serviceObj(nil, ""))
	_, _, err := h.evaluateRule(context.Background(), rule, req)
	if err == nil {
		t.Error("expected error for nil ServicePodSelectorCheck config")
	}
}

// TestServicePodSelectorCheck_NonUpdateAllowed verifies that non-UPDATE
// operations are always allowed.
func TestServicePodSelectorCheck_NonUpdateAllowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	// DELETE should be a no-op for this check.
	req := makeRequest(admissionv1.Delete, "", "services", "default", "my-svc", nil)
	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected non-UPDATE operation to be allowed")
	}
}

// TestServicePodSelectorCheck_OldNoSelector_Allowed verifies that a service
// with no selector (cannot select pods) is allowed to be modified.
func TestServicePodSelectorCheck_OldNoSelector_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	oldSvc := serviceObj(nil, "")
	newSvc := serviceObj(map[string]string{"app": "other"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected service with no old selector to be allowed")
	}
}

// TestServicePodSelectorCheck_HeadlessNoSelector_Allowed verifies that a
// headless service (clusterIP=None) without a selector is exempt.
func TestServicePodSelectorCheck_HeadlessNoSelector_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	oldSvc := serviceObj(nil, "None")
	newSvc := serviceObj(map[string]string{"app": "other"}, "10.0.0.1")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected headless service without selector to be exempt")
	}
}

// TestServicePodSelectorCheck_HeadlessWithSelector_Allowed verifies that a
// headless service (clusterIP=None) with a selector and matching pods is still
// exempt from this check.
func TestServicePodSelectorCheck_HeadlessWithSelector_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	// Headless service with a selector that matches a pod; change would drop pods.
	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "None")
	newSvc := serviceObj(map[string]string{"app": "no-such-app"}, "None")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected headless service (clusterIP=None) to be exempt even when selector change would drop pods")
	}
}

// TestServicePodSelectorCheck_EmptySelector_OldMatchesAll_NewNoPods_Denied
// verifies that a service with spec.selector: {} (matches all pods) is denied
// when the new selector would match no pods.
func TestServicePodSelectorCheck_EmptySelector_OldMatchesAll_NewNoPods_Denied(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	// spec.selector: {} matches all pods (including my-pod above).
	oldSvc := serviceObj(map[string]string{}, "")
	// New selector has no matching pods.
	newSvc := serviceObj(map[string]string{"app": "no-such-app"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected change to be denied: old empty-selector matched all pods but new selector matches none")
	}
}

// TestServicePodSelectorCheck_EmptySelector_NewMatchesAll_Allowed verifies
// that changing to spec.selector: {} (matches all pods) is allowed when pods exist.
func TestServicePodSelectorCheck_EmptySelector_NewMatchesAll_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	// Old service selected a specific app.
	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	// New service uses spec.selector: {} which matches all pods.
	newSvc := serviceObj(map[string]string{}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected change to be allowed: new empty-selector {} matches all pods")
	}
}

// TestServicePodSelectorCheck_OldNoPods_Allowed verifies that when the old
// service had a selector but no matching pods, the change is allowed.
func TestServicePodSelectorCheck_OldNoPods_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme) // no pods
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(map[string]string{"app": "other-app"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected service with no old matching pods to be allowed")
	}
}

// TestServicePodSelectorCheck_OldHadPods_NewHasPods_Allowed verifies that
// when the new service also selects pods, the change is allowed.
func TestServicePodSelectorCheck_OldHadPods_NewHasPods_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	// Old and new service both select the same pod.
	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violated {
		t.Error("expected change to be allowed when new service still has matching pods")
	}
}

// TestServicePodSelectorCheck_OldHadPods_NewNoSelector_Denied verifies that
// removing the selector is denied when the old service had matching pods.
func TestServicePodSelectorCheck_OldHadPods_NewNoSelector_Denied(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(nil, "") // selector removed
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, msg, err := h.evaluateServicePodSelectorCheck(context.Background(), "selector change denied", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected change to be denied when new service has no selector but old had pods")
	}
	if msg != "selector change denied" {
		t.Errorf("unexpected message: %q", msg)
	}
}

// TestServicePodSelectorCheck_OldHadPods_NewNoPods_Denied verifies that
// changing the selector so no pods match is denied when the old service had pods.
func TestServicePodSelectorCheck_OldHadPods_NewNoPods_Denied(t *testing.T) {
	scheme := newHandlerScheme(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)
	h := &AdmissionHandler{DynamicClient: fakeDynamic}

	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(map[string]string{"app": "no-such-app"}, "") // different selector, no pods
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	violated, _, err := h.evaluateServicePodSelectorCheck(context.Background(), "msg", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !violated {
		t.Error("expected change to be denied when new selector matches no pods but old had pods")
	}
}

// TestHandle_ServicePodSelectorCheck_Denied is an integration test that verifies
// the full Handle path denies a Service UPDATE that would drop all pod references.
func TestHandle_ServicePodSelectorCheck_Denied(t *testing.T) {
	scheme := newHandlerScheme(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	guard := newServicePodSelectorGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(map[string]string{"app": "no-such-app"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	resp := h.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected Handle to deny service UPDATE that drops all pod references")
	}
}

// TestHandle_ServicePodSelectorCheck_Allowed verifies that a Service UPDATE
// which keeps matching pods is allowed.
func TestHandle_ServicePodSelectorCheck_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "my-app"},
		},
	}
	guard := newServicePodSelectorGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme, pod)

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected Handle to allow service UPDATE that retains pod references: %v", resp.Result)
	}
}

// TestHandle_ServicePodSelectorCheck_NoPreviousPods_Allowed verifies that a
// Service UPDATE is allowed when the old service had no matching pods.
func TestHandle_ServicePodSelectorCheck_NoPreviousPods_Allowed(t *testing.T) {
	scheme := newHandlerScheme(t)

	guard := newServicePodSelectorGuard()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(guard).Build()
	fakeDynamic := dynamicfake.NewSimpleDynamicClient(scheme) // no pods

	h := &AdmissionHandler{Client: fakeClient, DynamicClient: fakeDynamic}

	oldSvc := serviceObj(map[string]string{"app": "my-app"}, "")
	newSvc := serviceObj(map[string]string{"app": "other-app"}, "")
	req := makeUpdateRequestFull("", "services", "default", "my-svc", oldSvc, newSvc)

	resp := h.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected Handle to allow service UPDATE when old service had no matching pods: %v", resp.Result)
	}
}
