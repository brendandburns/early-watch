package auditmonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// buildScheme creates a runtime scheme with the earlywatch types registered.
func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = ewv1alpha1.AddToScheme(s)
	return s
}

// makeMonitor returns a ManualTouchMonitor watching "services" DELETE ops.
func makeMonitor(name, ns string) *ewv1alpha1.ManualTouchMonitor {
	return &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects: []ewv1alpha1.MonitorSubject{
				{APIGroup: "", Resource: "services"},
			},
			Operations: []ewv1alpha1.MonitorOperationType{
				ewv1alpha1.MonitorOperationDelete,
			},
		},
	}
}

// makeAuditEvent builds a minimal AuditEvent for testing.
func makeAuditEvent(verb, resource, ns, name, userAgent string) *AuditEvent {
	return &AuditEvent{
		AuditID:   "test-audit-id-001",
		Verb:      verb,
		UserAgent: userAgent,
		Stage:     "ResponseComplete",
		User:      AuditUser{Username: "kubernetes-admin"},
		ObjectRef: AuditObjectRef{
			Resource:  resource,
			Namespace: ns,
			Name:      name,
			APIGroup:  "",
		},
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Now()),
	}
}

// ---- TouchDetector tests ----

func TestTouchDetector_DetectsKubectlDelete(t *testing.T) {
	scheme := buildScheme()
	monitor := makeMonitor("mon1", "default")
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	detector := &TouchDetector{Client: fakeClient}
	event := makeAuditEvent("delete", "services", "default", "my-svc", "kubectl/v1.29.0 (linux/amd64)")

	records, err := detector.Detect(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 touch record, got %d", len(records))
	}
	if records[0].MonitorName != "mon1" {
		t.Errorf("expected MonitorName mon1, got %s", records[0].MonitorName)
	}
}

func TestTouchDetector_IgnoresControllerUserAgent(t *testing.T) {
	scheme := buildScheme()
	monitor := makeMonitor("mon1", "default")
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	detector := &TouchDetector{Client: fakeClient}
	// User-agent from a controller, not kubectl.
	event := makeAuditEvent("delete", "services", "default", "my-svc", "kube-controller-manager/v1.29.0")

	records, err := detector.Detect(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for controller user-agent, got %d", len(records))
	}
}

func TestTouchDetector_IgnoresReadVerb(t *testing.T) {
	scheme := buildScheme()
	monitor := makeMonitor("mon1", "default")
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	detector := &TouchDetector{Client: fakeClient}
	event := makeAuditEvent("get", "services", "default", "my-svc", "kubectl/v1.29.0")

	records, err := detector.Detect(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for GET verb, got %d", len(records))
	}
}

func TestTouchDetector_ExcludesServiceAccount(t *testing.T) {
	scheme := buildScheme()
	monitor := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon2", Namespace: "default"},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects: []ewv1alpha1.MonitorSubject{
				{APIGroup: "", Resource: "services"},
			},
			Operations: []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete},
			ExcludeServiceAccounts: []string{
				"system:serviceaccount:kube-system:my-bot",
			},
		},
	}
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	detector := &TouchDetector{Client: fakeClient}
	event := makeAuditEvent("delete", "services", "default", "my-svc", "kubectl/v1.29.0")
	event.User.Username = "system:serviceaccount:kube-system:my-bot"

	records, err := detector.Detect(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for excluded service account, got %d", len(records))
	}
}

func TestTouchDetector_CustomUserAgentPattern(t *testing.T) {
	scheme := buildScheme()
	monitor := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon3", Namespace: "default"},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:          []ewv1alpha1.MonitorSubject{{APIGroup: "", Resource: "pods"}},
			Operations:        []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete},
			UserAgentPatterns: []string{`^my-custom-tool/`},
		},
	}
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	detector := &TouchDetector{Client: fakeClient}

	// Should match custom pattern.
	event := makeAuditEvent("delete", "pods", "default", "my-pod", "my-custom-tool/v2.0.0")
	records, err := detector.Detect(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record for custom user-agent, got %d", len(records))
	}

	// kubectl should NOT match the custom pattern.
	event2 := makeAuditEvent("delete", "pods", "default", "my-pod", "kubectl/v1.29.0")
	records2, err := detector.Detect(context.Background(), event2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records2) != 0 {
		t.Errorf("expected 0 records for kubectl when custom pattern is set, got %d", len(records2))
	}
}

func TestTouchDetector_PatchMapsToUpdate(t *testing.T) {
	scheme := buildScheme()
	monitor := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon4", Namespace: "default"},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:   []ewv1alpha1.MonitorSubject{{APIGroup: "", Resource: "services"}},
			Operations: []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationUpdate},
		},
	}
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	detector := &TouchDetector{Client: fakeClient}
	event := makeAuditEvent("patch", "services", "default", "my-svc", "kubectl/v1.29.0")

	records, err := detector.Detect(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record for PATCH→UPDATE, got %d", len(records))
	}
}

// ---- cachedPatterns / userAgentMatches tests ----

func TestCachedPatterns_DefaultWhenEmpty(t *testing.T) {
	monitor := makeMonitor("mon", "default")
	monitor.Spec.UserAgentPatterns = nil
	patterns, ok := cachedPatterns(monitor)
	if !ok {
		t.Fatal("expected ok=true for empty patterns (default)")
	}
	if !userAgentMatches("kubectl/v1.29.0 (linux/amd64)", patterns) {
		t.Error("default pattern should match kubectl user-agent")
	}
	if userAgentMatches("kube-controller-manager/v1.29.0", patterns) {
		t.Error("default pattern should not match controller user-agent")
	}
}

func TestCachedPatterns_Custom(t *testing.T) {
	monitor := makeMonitor("mon-custom", "default")
	monitor.Spec.UserAgentPatterns = []string{`^helm/`}
	patterns, ok := cachedPatterns(monitor)
	if !ok {
		t.Fatal("expected ok=true for valid custom pattern")
	}
	if !userAgentMatches("helm/v3.12.0", patterns) {
		t.Error("custom pattern should match helm user-agent")
	}
	if userAgentMatches("kubectl/v1.29.0", patterns) {
		t.Error("custom pattern should not match kubectl when only helm pattern set")
	}
}

func TestCachedPatterns_InvalidPatternNonMatching(t *testing.T) {
	// An invalid regex should cause ok=false (non-matching), not fall back to default.
	monitor := makeMonitor("mon-invalid", "default")
	monitor.Spec.UserAgentPatterns = []string{`[invalid`}
	_, ok := cachedPatterns(monitor)
	if ok {
		t.Error("should return ok=false when all configured patterns are invalid")
	}
}

// ---- sanitizeName tests ----

func TestSanitizeName_Basic(t *testing.T) {
	result := sanitizeName("mte-hello-world")
	if result != "mte-hello-world" {
		t.Errorf("unexpected sanitized name: %q", result)
	}
}

func TestSanitizeName_UpperCase(t *testing.T) {
	result := sanitizeName("MTE-Hello")
	if result != "mte-hello" {
		t.Errorf("expected lower-cased name, got %q", result)
	}
}

func TestSanitizeName_SpecialChars(t *testing.T) {
	result := sanitizeName("mte-abc:def/ghi")
	if result == "" {
		t.Error("sanitized name should not be empty")
	}
	for _, r := range result {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			t.Errorf("sanitized name contains invalid character: %q", r)
		}
	}
}

func TestSanitizeName_Truncation(t *testing.T) {
	// Use a long string of valid characters so sanitizeName does not trim them
	// away and we actually exercise the 253-character truncation path.
	long := strings.Repeat("a", 300)
	result := sanitizeName(long)
	if len(result) > 253 {
		t.Errorf("name too long: %d chars", len(result))
	}
	if len(result) != 253 {
		t.Errorf("expected truncated length 253, got %d", len(result))
	}
}

// ---- AuditEventHandler HTTP handler tests ----

func TestAuditEventHandler_PostKubectlDelete(t *testing.T) {
	scheme := buildScheme()
	monitor := makeMonitor("mon1", "default")
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	h := &AuditEventHandler{
		Detector: &TouchDetector{Client: fakeClient},
		Recorder: &TouchRecorder{Client: fakeClient, EventNamespace: "early-watch-system"},
	}

	eventList := AuditEventList{
		Items: []AuditEvent{
			*makeAuditEvent("delete", "services", "default", "my-svc", "kubectl/v1.29.0"),
		},
	}
	body, _ := json.Marshal(eventList)

	req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w.Code)
	}

	// Verify ManualTouchEvent was created.
	mteList := &ewv1alpha1.ManualTouchEventList{}
	if err := fakeClient.List(context.Background(), mteList); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(mteList.Items) != 1 {
		t.Errorf("expected 1 ManualTouchEvent, got %d", len(mteList.Items))
	}
	if mteList.Items[0].Spec.User != "kubernetes-admin" {
		t.Errorf("unexpected user: %s", mteList.Items[0].Spec.User)
	}
}

func TestAuditEventHandler_IgnoresGetMethod(t *testing.T) {
	scheme := buildScheme()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()

	h := &AuditEventHandler{
		Detector: &TouchDetector{Client: fakeClient},
		Recorder: &TouchRecorder{Client: fakeClient, EventNamespace: "early-watch-system"},
	}

	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestAuditEventHandler_BadJSON(t *testing.T) {
	scheme := buildScheme()
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()

	h := &AuditEventHandler{
		Detector: &TouchDetector{Client: fakeClient},
		Recorder: &TouchRecorder{Client: fakeClient, EventNamespace: "early-watch-system"},
	}

	req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAuditEventHandler_SkipsNonResponseCompleteStage(t *testing.T) {
	scheme := buildScheme()
	monitor := makeMonitor("mon1", "default")
	fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(monitor).Build()

	h := &AuditEventHandler{
		Detector: &TouchDetector{Client: fakeClient},
		Recorder: &TouchRecorder{Client: fakeClient, EventNamespace: "early-watch-system"},
	}

	event := makeAuditEvent("delete", "services", "default", "my-svc", "kubectl/v1.29.0")
	event.Stage = "RequestReceived" // not ResponseComplete
	eventList := AuditEventList{Items: []AuditEvent{*event}}
	body, _ := json.Marshal(eventList)

	req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w.Code)
	}

	mteList := &ewv1alpha1.ManualTouchEventList{}
	_ = fakeClient.List(context.Background(), mteList)
	if len(mteList.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents for non-ResponseComplete stage, got %d", len(mteList.Items))
	}
}

// ---- firstSourceIP / eventTime helpers tests ----

func TestFirstSourceIP_NonEmpty(t *testing.T) {
	if got := firstSourceIP([]string{"1.2.3.4", "5.6.7.8"}); got != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", got)
	}
}

func TestFirstSourceIP_Empty(t *testing.T) {
	if got := firstSourceIP(nil); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestEventTime_Zero(t *testing.T) {
	ts := eventTime(metav1.MicroTime{})
	if ts.IsZero() {
		t.Error("eventTime with zero input should return current time, not zero")
	}
}

func TestEventTime_NonZero(t *testing.T) {
	fixed := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ts := eventTime(metav1.NewMicroTime(fixed))
	if !ts.Time.Equal(fixed) {
		t.Errorf("expected %v, got %v", fixed, ts.Time)
	}
}
