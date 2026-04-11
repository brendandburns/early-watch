package webhook

import (
	"context"
	"encoding/json"
	"testing"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
	guard := &ewv1alpha1.ChangeGuard{
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeGuardSpec{
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
	guard := &ewv1alpha1.ChangeGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "guard1", Namespace: "default"},
		Spec: ewv1alpha1.ChangeGuardSpec{
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

// labelsFromMap is a helper that converts a plain map to a labels.Set.
func labelsFromMap(m map[string]string) interface {
	Has(label string) bool
	Get(label string) string
} {
	return labelSet(m)
}

type labelSet map[string]string

func (s labelSet) Has(label string) bool {
	_, ok := s[label]
	return ok
}

func (s labelSet) Get(label string) string {
	return s[label]
}
