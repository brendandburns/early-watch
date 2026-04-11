package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestChangeGuard_DeepCopy_NilSafe(t *testing.T) {
	var cg *ChangeGuard
	if got := cg.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ChangeGuard should return nil")
	}

	var cl *ChangeGuardList
	if got := cl.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ChangeGuardList should return nil")
	}

	var spec *ChangeGuardSpec
	if got := spec.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ChangeGuardSpec should return nil")
	}

	var status *ChangeGuardStatus
	if got := status.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ChangeGuardStatus should return nil")
	}

	var rule *GuardRule
	if got := rule.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil GuardRule should return nil")
	}

	var erc *ExistingResourcesCheck
	if got := erc.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ExistingResourcesCheck should return nil")
	}

	var ec *ExpressionCheck
	if got := ec.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ExpressionCheck should return nil")
	}

	var subj *SubjectResource
	if got := subj.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil SubjectResource should return nil")
	}
}

func TestChangeGuard_DeepCopy(t *testing.T) {
	original := &ChangeGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "test-guard", Namespace: "default"},
		Spec: ChangeGuardSpec{
			Subject: SubjectResource{
				APIGroup: "apps",
				Resource: "deployments",
			},
			Operations: []OperationType{OperationDelete, OperationUpdate},
			Rules: []GuardRule{
				{
					Name:    "deny-delete",
					Type:    RuleTypeExpressionCheck,
					Message: "not allowed",
					ExpressionCheck: &ExpressionCheck{
						Expression: "operation == 'DELETE'",
					},
				},
			},
		},
	}

	copied := original.DeepCopy()

	// Modifying the copy must not affect the original.
	copied.Spec.Subject.Resource = "pods"
	copied.Spec.Operations = append(copied.Spec.Operations, OperationCreate)
	copied.Spec.Rules[0].ExpressionCheck.Expression = "modified"

	if original.Spec.Subject.Resource != "deployments" {
		t.Error("DeepCopy shared Subject.Resource with original")
	}
	if len(original.Spec.Operations) != 2 {
		t.Errorf("DeepCopy shared Operations slice: got len %d, want 2", len(original.Spec.Operations))
	}
	if original.Spec.Rules[0].ExpressionCheck.Expression != "operation == 'DELETE'" {
		t.Error("DeepCopy shared ExpressionCheck.Expression with original")
	}
}

func TestChangeGuard_DeepCopyObject(t *testing.T) {
	original := &ChangeGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "test-guard"},
		Spec: ChangeGuardSpec{
			Subject:    SubjectResource{Resource: "services"},
			Operations: []OperationType{OperationDelete},
			Rules:      []GuardRule{{Name: "r1", Type: RuleTypeExpressionCheck, Message: "m"}},
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}
	copied, ok := obj.(*ChangeGuard)
	if !ok {
		t.Fatalf("DeepCopyObject returned %T, want *ChangeGuard", obj)
	}
	copied.Spec.Subject.Resource = "pods"
	if original.Spec.Subject.Resource != "services" {
		t.Error("DeepCopyObject shared data with original")
	}
}

func TestChangeGuardList_DeepCopy(t *testing.T) {
	original := &ChangeGuardList{
		Items: []ChangeGuard{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "g1"},
				Spec: ChangeGuardSpec{
					Subject:    SubjectResource{Resource: "services"},
					Operations: []OperationType{OperationDelete},
					Rules:      []GuardRule{{Name: "r1", Type: RuleTypeExpressionCheck, Message: "m"}},
				},
			},
		},
	}

	copied := original.DeepCopy()
	copied.Items[0].Spec.Subject.Resource = "pods"

	if original.Items[0].Spec.Subject.Resource != "services" {
		t.Error("ChangeGuardList.DeepCopy shared Items data with original")
	}
}

func TestChangeGuardList_DeepCopyObject(t *testing.T) {
	original := &ChangeGuardList{
		Items: []ChangeGuard{
			{ObjectMeta: metav1.ObjectMeta{Name: "g1"}},
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}
	if _, ok := obj.(*ChangeGuardList); !ok {
		t.Fatalf("DeepCopyObject returned %T, want *ChangeGuardList", obj)
	}
}

func TestSubjectResource_DeepCopy_WithNamespaceSelector(t *testing.T) {
	original := &SubjectResource{
		APIGroup: "",
		Resource: "services",
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}

	copied := original.DeepCopy()
	copied.NamespaceSelector.MatchLabels["env"] = "staging"

	if original.NamespaceSelector.MatchLabels["env"] != "prod" {
		t.Error("DeepCopy shared NamespaceSelector.MatchLabels with original")
	}
}

func TestExistingResourcesCheck_DeepCopy_WithSameNamespace(t *testing.T) {
	sameNS := true
	original := &ExistingResourcesCheck{
		APIGroup:      "",
		Resource:      "pods",
		SameNamespace: &sameNS,
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "test"},
		},
	}

	copied := original.DeepCopy()
	copied.LabelSelector.MatchLabels["app"] = "modified"
	*copied.SameNamespace = false

	if original.LabelSelector.MatchLabels["app"] != "test" {
		t.Error("DeepCopy shared LabelSelector.MatchLabels with original")
	}
	if !*original.SameNamespace {
		t.Error("DeepCopy shared SameNamespace pointer with original")
	}
}

func TestChangeGuardStatus_DeepCopy(t *testing.T) {
	original := &ChangeGuardStatus{
		ObservedGeneration: 3,
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "OK"},
		},
	}

	copied := original.DeepCopy()
	copied.Conditions[0].Reason = "Modified"
	copied.ObservedGeneration = 99

	if original.Conditions[0].Reason != "OK" {
		t.Error("DeepCopy shared Conditions with original")
	}
	if original.ObservedGeneration != 3 {
		t.Error("DeepCopy shared ObservedGeneration with original")
	}
}
