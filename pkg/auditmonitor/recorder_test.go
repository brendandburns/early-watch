package auditmonitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// ---- isAlreadyExists ----

func TestIsAlreadyExists_True(t *testing.T) {
	err := fmt.Errorf("resource already exists")
	if !isAlreadyExists(err) {
		t.Error("expected true for 'already exists' error")
	}
}

func TestIsAlreadyExists_False(t *testing.T) {
	err := fmt.Errorf("some other error")
	if isAlreadyExists(err) {
		t.Error("expected false for unrelated error")
	}
}

func TestIsAlreadyExists_Nil(t *testing.T) {
	if isAlreadyExists(nil) {
		t.Error("expected false for nil error")
	}
}

// ---- namespaceMatchesSelector ----

func TestNamespaceMatchesSelector_NilSelector(t *testing.T) {
	if !namespaceMatchesSelector("default", nil) {
		t.Error("nil selector should match any namespace")
	}
}

func TestNamespaceMatchesSelector_EmptySelector(t *testing.T) {
	sel := &metav1.LabelSelector{}
	if !namespaceMatchesSelector("default", sel) {
		t.Error("empty selector should match any namespace")
	}
}

func TestNamespaceMatchesSelector_MatchLabelsContainsKey(t *testing.T) {
	sel := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"default": "allowed",
		},
	}
	if !namespaceMatchesSelector("default", sel) {
		t.Error("selector with namespace as key should match")
	}
}

func TestNamespaceMatchesSelector_MatchLabelsMissingKey(t *testing.T) {
	sel := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"production": "allowed",
		},
	}
	if namespaceMatchesSelector("default", sel) {
		t.Error("selector without namespace key should not match")
	}
}

// ---- TouchRecorder.Record idempotency ----

func TestTouchRecorder_Record_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = ewv1alpha1.AddToScheme(scheme)

	monitor := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon1", Namespace: "default"},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:   []ewv1alpha1.MonitorSubject{{APIGroup: "", Resource: "services"}},
			Operations: []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete},
		},
	}
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	recorder := &TouchRecorder{Client: fakeClient, EventNamespace: "early-watch-system"}
	event := &AuditEvent{
		AuditID:                  "idempotent-audit-001",
		Verb:                     "delete",
		UserAgent:                "kubectl/v1.29.0",
		Stage:                    "ResponseComplete",
		User:                     AuditUser{Username: "admin"},
		ObjectRef:                AuditObjectRef{Resource: "services", Namespace: "default", Name: "my-svc"},
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Now()),
	}
	touch := TouchRecord{
		Event:            event,
		Operation:        ewv1alpha1.MonitorOperationDelete,
		MonitorName:      "mon1",
		MonitorNamespace: "default",
	}

	// First record should succeed.
	if err := recorder.Record(context.Background(), touch); err != nil {
		t.Fatalf("first Record failed: %v", err)
	}

	// Second record with the same audit ID should be silently ignored (idempotent).
	if err := recorder.Record(context.Background(), touch); err != nil {
		t.Fatalf("second Record (idempotent) should not return error, got: %v", err)
	}

	// Verify only one ManualTouchEvent was created.
	mteList := &ewv1alpha1.ManualTouchEventList{}
	if err := fakeClient.List(context.Background(), mteList); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(mteList.Items) != 1 {
		t.Errorf("expected exactly 1 ManualTouchEvent after idempotent double-record, got %d", len(mteList.Items))
	}
}

// ---- TouchRecorder.Record labels ----

func TestTouchRecorder_Record_Labels(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = ewv1alpha1.AddToScheme(scheme)

	monitor := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon1", Namespace: "default"},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:   []ewv1alpha1.MonitorSubject{{APIGroup: "apps", Resource: "deployments"}},
			Operations: []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationUpdate},
		},
	}
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	recorder := &TouchRecorder{Client: fakeClient, EventNamespace: "early-watch-system"}
	event := &AuditEvent{
		AuditID:   "labels-audit-001",
		Verb:      "update",
		UserAgent: "kubectl/v1.29.0",
		Stage:     "ResponseComplete",
		User:      AuditUser{Username: "kubernetes-admin"},
		SourceIPs: []string{"192.168.1.1"},
		ObjectRef: AuditObjectRef{
			Resource:   "deployments",
			Namespace:  "production",
			Name:       "my-deploy",
			APIGroup:   "apps",
			APIVersion: "v1",
		},
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Now()),
	}
	touch := TouchRecord{
		Event:            event,
		Operation:        ewv1alpha1.MonitorOperationUpdate,
		MonitorName:      "mon1",
		MonitorNamespace: "default",
	}

	if err := recorder.Record(context.Background(), touch); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	mteList := &ewv1alpha1.ManualTouchEventList{}
	if err := fakeClient.List(context.Background(), mteList); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(mteList.Items) != 1 {
		t.Fatalf("expected 1 ManualTouchEvent, got %d", len(mteList.Items))
	}

	mte := mteList.Items[0]

	// Verify label indexes.
	assertLabel(t, mte.Labels, LabelResource, "deployments")
	assertLabel(t, mte.Labels, LabelResourceNamespace, "production")
	assertLabel(t, mte.Labels, LabelResourceName, "my-deploy")
	assertLabel(t, mte.Labels, LabelAPIGroup, "apps")
	assertLabel(t, mte.Labels, LabelOperation, "UPDATE")

	// Verify spec fields.
	if mte.Spec.User != "kubernetes-admin" {
		t.Errorf("expected User 'kubernetes-admin', got %q", mte.Spec.User)
	}
	if mte.Spec.Resource != "deployments" {
		t.Errorf("expected Resource 'deployments', got %q", mte.Spec.Resource)
	}
	if mte.Spec.ResourceNamespace != "production" {
		t.Errorf("expected ResourceNamespace 'production', got %q", mte.Spec.ResourceNamespace)
	}
	if mte.Spec.ResourceName != "my-deploy" {
		t.Errorf("expected ResourceName 'my-deploy', got %q", mte.Spec.ResourceName)
	}
	if mte.Spec.SourceIP != "192.168.1.1" {
		t.Errorf("expected SourceIP '192.168.1.1', got %q", mte.Spec.SourceIP)
	}
	if mte.Spec.AuditID != "labels-audit-001" {
		t.Errorf("expected AuditID 'labels-audit-001', got %q", mte.Spec.AuditID)
	}
	if mte.Spec.MonitorName != "mon1" {
		t.Errorf("expected MonitorName 'mon1', got %q", mte.Spec.MonitorName)
	}
}

// assertLabel is a helper that fails the test when the expected label is
// missing or has the wrong value.
func assertLabel(t *testing.T, labels map[string]string, key, want string) {
	t.Helper()
	got, ok := labels[key]
	if !ok {
		t.Errorf("label %q missing", key)
		return
	}
	if got != want {
		t.Errorf("label %q: want %q, got %q", key, want, got)
	}
}

// ---- TouchRecorder defaults ----

func TestTouchRecorder_DefaultNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = ewv1alpha1.AddToScheme(scheme)

	monitor := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon1", Namespace: "default"},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:   []ewv1alpha1.MonitorSubject{{APIGroup: "", Resource: "services"}},
			Operations: []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete},
		},
	}
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	// No EventNamespace set — should default to "early-watch-system".
	recorder := &TouchRecorder{Client: fakeClient}
	event := &AuditEvent{
		AuditID:                  "ns-default-001",
		Verb:                     "delete",
		UserAgent:                "kubectl/v1.29.0",
		Stage:                    "ResponseComplete",
		User:                     AuditUser{Username: "admin"},
		ObjectRef:                AuditObjectRef{Resource: "services", Namespace: "default", Name: "svc"},
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Now()),
	}
	touch := TouchRecord{Event: event, Operation: ewv1alpha1.MonitorOperationDelete, MonitorName: "mon1", MonitorNamespace: "default"}

	if err := recorder.Record(context.Background(), touch); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	mteList := &ewv1alpha1.ManualTouchEventList{}
	if err := fakeClient.List(context.Background(), mteList); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(mteList.Items) != 1 {
		t.Fatalf("expected 1 ManualTouchEvent, got %d", len(mteList.Items))
	}
	if mteList.Items[0].Namespace != DefaultEventNamespace {
		t.Errorf("expected namespace %q, got %q", DefaultEventNamespace, mteList.Items[0].Namespace)
	}
}
