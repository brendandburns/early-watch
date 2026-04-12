package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---- ManualTouchCheck DeepCopy ----

func TestManualTouchCheck_DeepCopy_NilSafe(t *testing.T) {
	var mtc *ManualTouchCheck
	if got := mtc.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchCheck should return nil")
	}
}

func TestManualTouchCheck_DeepCopy(t *testing.T) {
	original := &ManualTouchCheck{
		WindowDuration: "2h",
		EventNamespace: "monitoring",
	}
	copied := original.DeepCopy()
	copied.WindowDuration = "30m"
	copied.EventNamespace = "other"

	if original.WindowDuration != "2h" {
		t.Error("DeepCopy shared WindowDuration with original")
	}
	if original.EventNamespace != "monitoring" {
		t.Error("DeepCopy shared EventNamespace with original")
	}
}

// ---- AlertingConfig DeepCopy ----

func TestAlertingConfig_DeepCopy_NilSafe(t *testing.T) {
	var ac *AlertingConfig
	if got := ac.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil AlertingConfig should return nil")
	}
}

func TestAlertingConfig_DeepCopy(t *testing.T) {
	original := &AlertingConfig{
		PrometheusLabels: map[string]string{
			"env":  "prod",
			"team": "platform",
		},
	}
	copied := original.DeepCopy()
	copied.PrometheusLabels["env"] = "staging"
	copied.PrometheusLabels["new"] = "label"

	if original.PrometheusLabels["env"] != "prod" {
		t.Error("DeepCopy shared PrometheusLabels map with original")
	}
	if _, ok := original.PrometheusLabels["new"]; ok {
		t.Error("DeepCopy shared PrometheusLabels map with original (extra key)")
	}
}

func TestAlertingConfig_DeepCopy_NilLabels(t *testing.T) {
	original := &AlertingConfig{PrometheusLabels: nil}
	copied := original.DeepCopy()
	if copied.PrometheusLabels != nil {
		t.Error("DeepCopy should preserve nil PrometheusLabels")
	}
}

// ---- MonitorSubject DeepCopy ----

func TestMonitorSubject_DeepCopy_NilSafe(t *testing.T) {
	var ms *MonitorSubject
	if got := ms.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil MonitorSubject should return nil")
	}
}

func TestMonitorSubject_DeepCopy(t *testing.T) {
	original := &MonitorSubject{
		APIGroup: "",
		Resource: "services",
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}
	copied := original.DeepCopy()
	copied.Resource = "pods"
	copied.NamespaceSelector.MatchLabels["env"] = "dev"

	if original.Resource != "services" {
		t.Error("DeepCopy shared Resource with original")
	}
	if original.NamespaceSelector.MatchLabels["env"] != "prod" {
		t.Error("DeepCopy shared NamespaceSelector.MatchLabels with original")
	}
}

func TestMonitorSubject_DeepCopy_NilNamespaceSelector(t *testing.T) {
	original := &MonitorSubject{APIGroup: "apps", Resource: "deployments", NamespaceSelector: nil}
	copied := original.DeepCopy()
	if copied.NamespaceSelector != nil {
		t.Error("DeepCopy should preserve nil NamespaceSelector")
	}
}

// ---- ManualTouchMonitorSpec DeepCopy ----

func TestManualTouchMonitorSpec_DeepCopy_NilSafe(t *testing.T) {
	var ms *ManualTouchMonitorSpec
	if got := ms.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchMonitorSpec should return nil")
	}
}

func TestManualTouchMonitorSpec_DeepCopy(t *testing.T) {
	original := &ManualTouchMonitorSpec{
		Subjects: []MonitorSubject{
			{APIGroup: "", Resource: "services"},
		},
		Operations:             []MonitorOperationType{MonitorOperationDelete},
		UserAgentPatterns:      []string{`^kubectl/`},
		ExcludeServiceAccounts: []string{"system:serviceaccount:ci:bot"},
		Alerting:               &AlertingConfig{},
	}

	copied := original.DeepCopy()

	// Mutate copy.
	copied.Subjects[0].Resource = "pods"
	copied.Operations = append(copied.Operations, MonitorOperationCreate)
	copied.UserAgentPatterns[0] = "changed"
	copied.ExcludeServiceAccounts[0] = "other"

	if original.Subjects[0].Resource != "services" {
		t.Error("DeepCopy shared Subjects with original")
	}
	if len(original.Operations) != 1 {
		t.Errorf("DeepCopy shared Operations slice: got %d, want 1", len(original.Operations))
	}
	if original.UserAgentPatterns[0] != `^kubectl/` {
		t.Error("DeepCopy shared UserAgentPatterns with original")
	}
	if original.ExcludeServiceAccounts[0] != "system:serviceaccount:ci:bot" {
		t.Error("DeepCopy shared ExcludeServiceAccounts with original")
	}
}

// ---- ManualTouchMonitorStatus DeepCopy ----

func TestManualTouchMonitorStatus_DeepCopy_NilSafe(t *testing.T) {
	var s *ManualTouchMonitorStatus
	if got := s.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchMonitorStatus should return nil")
	}
}

func TestManualTouchMonitorStatus_DeepCopy(t *testing.T) {
	original := &ManualTouchMonitorStatus{
		ObservedGeneration: 5,
		TouchesDetected:    42,
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "OK", Message: "all good",
				LastTransitionTime: metav1.NewTime(time.Now())},
		},
	}

	copied := original.DeepCopy()
	copied.ObservedGeneration = 99
	copied.TouchesDetected = 0
	copied.Conditions[0].Reason = "Modified"

	if original.ObservedGeneration != 5 {
		t.Error("DeepCopy shared ObservedGeneration with original")
	}
	if original.TouchesDetected != 42 {
		t.Error("DeepCopy shared TouchesDetected with original")
	}
	if original.Conditions[0].Reason != "OK" {
		t.Error("DeepCopy shared Conditions with original")
	}
}

// ---- ManualTouchMonitor DeepCopy ----

func TestManualTouchMonitor_DeepCopy_NilSafe(t *testing.T) {
	var m *ManualTouchMonitor
	if got := m.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchMonitor should return nil")
	}
}

func TestManualTouchMonitor_DeepCopyObject(t *testing.T) {
	original := &ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon1", Namespace: "default"},
		Spec: ManualTouchMonitorSpec{
			Subjects:   []MonitorSubject{{APIGroup: "", Resource: "services"}},
			Operations: []MonitorOperationType{MonitorOperationDelete},
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}
	copied, ok := obj.(*ManualTouchMonitor)
	if !ok {
		t.Fatalf("DeepCopyObject returned %T, want *ManualTouchMonitor", obj)
	}
	copied.Spec.Subjects[0].Resource = "pods"
	if original.Spec.Subjects[0].Resource != "services" {
		t.Error("DeepCopyObject shared Subjects data with original")
	}
}

// ---- ManualTouchMonitorList DeepCopy ----

func TestManualTouchMonitorList_DeepCopy_NilSafe(t *testing.T) {
	var l *ManualTouchMonitorList
	if got := l.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchMonitorList should return nil")
	}
}

func TestManualTouchMonitorList_DeepCopyObject(t *testing.T) {
	original := &ManualTouchMonitorList{
		Items: []ManualTouchMonitor{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "mon1"},
				Spec: ManualTouchMonitorSpec{
					Subjects:   []MonitorSubject{{Resource: "services"}},
					Operations: []MonitorOperationType{MonitorOperationDelete},
				},
			},
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}
	if _, ok := obj.(*ManualTouchMonitorList); !ok {
		t.Fatalf("DeepCopyObject returned %T, want *ManualTouchMonitorList", obj)
	}
}

// ---- ManualTouchEventSpec DeepCopy ----

func TestManualTouchEventSpec_DeepCopy_NilSafe(t *testing.T) {
	var s *ManualTouchEventSpec
	if got := s.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchEventSpec should return nil")
	}
}

func TestManualTouchEventSpec_DeepCopy(t *testing.T) {
	ts := metav1.NewTime(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	original := &ManualTouchEventSpec{
		Timestamp:         ts,
		User:              "kubernetes-admin",
		UserAgent:         "kubectl/v1.29.0",
		Operation:         "DELETE",
		APIGroup:          "",
		Resource:          "services",
		ResourceName:      "my-svc",
		ResourceNamespace: "default",
		SourceIP:          "10.0.0.1",
		AuditID:           "audit-001",
		MonitorName:       "my-monitor",
		MonitorNamespace:  "default",
	}

	copied := original.DeepCopy()
	copied.User = "changed"
	copied.Resource = "pods"

	if original.User != "kubernetes-admin" {
		t.Error("DeepCopy shared User with original")
	}
	if original.Resource != "services" {
		t.Error("DeepCopy shared Resource with original")
	}
	if !original.Timestamp.Equal(&ts) {
		t.Error("DeepCopy altered Timestamp")
	}
}

// ---- ManualTouchEvent DeepCopy ----

func TestManualTouchEvent_DeepCopy_NilSafe(t *testing.T) {
	var e *ManualTouchEvent
	if got := e.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchEvent should return nil")
	}
}

func TestManualTouchEvent_DeepCopyObject(t *testing.T) {
	original := &ManualTouchEvent{
		ObjectMeta: metav1.ObjectMeta{Name: "mte-001", Namespace: "early-watch-system"},
		Spec: ManualTouchEventSpec{
			User:         "admin",
			Operation:    "DELETE",
			Resource:     "services",
			ResourceName: "my-svc",
			AuditID:      "audit-001",
			MonitorName:  "mon",
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}
	copied, ok := obj.(*ManualTouchEvent)
	if !ok {
		t.Fatalf("DeepCopyObject returned %T, want *ManualTouchEvent", obj)
	}
	copied.Spec.User = "changed"
	if original.Spec.User != "admin" {
		t.Error("DeepCopyObject shared Spec with original")
	}
}

// ---- ManualTouchEventList DeepCopy ----

func TestManualTouchEventList_DeepCopy_NilSafe(t *testing.T) {
	var l *ManualTouchEventList
	if got := l.DeepCopy(); got != nil {
		t.Error("DeepCopy of nil ManualTouchEventList should return nil")
	}
}

func TestManualTouchEventList_DeepCopyObject(t *testing.T) {
	original := &ManualTouchEventList{
		Items: []ManualTouchEvent{
			{ObjectMeta: metav1.ObjectMeta{Name: "mte-001"}},
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}
	if _, ok := obj.(*ManualTouchEventList); !ok {
		t.Fatalf("DeepCopyObject returned %T, want *ManualTouchEventList", obj)
	}
}

// ---- GuardRule DeepCopy with ManualTouchCheck ----

func TestGuardRule_DeepCopy_WithManualTouchCheck(t *testing.T) {
	original := &GuardRule{
		Name:    "mtc-rule",
		Type:    RuleTypeManualTouchCheck,
		Message: "recent manual touch detected",
		ManualTouchCheck: &ManualTouchCheck{
			WindowDuration: "1h",
			EventNamespace: "early-watch-system",
		},
	}

	copied := original.DeepCopy()
	copied.ManualTouchCheck.WindowDuration = "2h"
	copied.ManualTouchCheck.EventNamespace = "other"

	if original.ManualTouchCheck.WindowDuration != "1h" {
		t.Error("DeepCopy shared ManualTouchCheck.WindowDuration with original")
	}
	if original.ManualTouchCheck.EventNamespace != "early-watch-system" {
		t.Error("DeepCopy shared ManualTouchCheck.EventNamespace with original")
	}
}

func TestGuardRule_DeepCopy_NilManualTouchCheck(t *testing.T) {
	original := &GuardRule{
		Name:             "rule",
		Type:             RuleTypeManualTouchCheck,
		Message:          "msg",
		ManualTouchCheck: nil,
	}
	copied := original.DeepCopy()
	if copied.ManualTouchCheck != nil {
		t.Error("DeepCopy should preserve nil ManualTouchCheck")
	}
}
