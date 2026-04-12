// Package auditmonitor_e2e contains end-to-end tests for the EarlyWatch audit
// monitor subsystem.  The tests run against a real Kubernetes control plane
// provided by controller-runtime's envtest package, which starts an actual
// kube-apiserver and etcd process locally.
//
// The test suite covers:
//   - Core audit monitor pipeline: POST audit events → ManualTouchEvent CRs created
//   - Filtering: non-matching user agents, verbs, and resources are not recorded
//   - Service-account exclusions
//   - Idempotency: duplicate audit-event deliveries do not create duplicate CRs
//   - Multiple monitors matching a single event
//   - ManualTouchCheck admission rule integration:
//     ChangeValidator.ManualTouchCheck blocks automated changes when a recent
//     manual touch exists for the same resource
//
// Prerequisites – install the Kubernetes API server and etcd binaries:
//
//	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
//	export KUBEBUILDER_ASSETS=$(setup-envtest use --print path)
//
// Run:
//
//	go test -tags=e2e ./test/e2e/auditmonitor/... -v
//
//go:build e2e

package auditmonitor_e2e_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	"github.com/brendandburns/early-watch/pkg/auditmonitor"
	ewwebhook "github.com/brendandburns/early-watch/pkg/webhook"
)

const (
	// testNamespace is the namespace used for all test resources.
	testNamespace = "ew-auditmonitor-e2e"

	// eventNamespace is the namespace where ManualTouchEvent CRs are stored.
	eventNamespace = "early-watch-system"

	// pollInterval is the interval between polls when waiting for CRs.
	pollInterval = 50 * time.Millisecond

	// pollTimeout is the maximum time to wait for CRs to appear.
	pollTimeout = 10 * time.Second
)

var (
	k8sClient client.Client
	dynClient dynamic.Interface

	// auditHandler is the in-process HTTP handler used by audit monitor tests.
	// Tests POST directly to it via httptest rather than through the network.
	auditHandler *auditmonitor.AuditEventHandler

	mgrCtx    context.Context
	mgrCancel context.CancelFunc
)

// TestMain sets up a local Kubernetes control plane via envtest, registers the
// EarlyWatch admission webhook handler (for ManualTouchCheck tests) and the
// AuditEventHandler in-process, then runs all tests.
func TestMain(m *testing.M) {
	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	mgrCtx, mgrCancel = context.WithCancel(context.Background())

	scheme := k8sruntime.NewScheme()
	mustAddToScheme(clientgoscheme.AddToScheme, scheme)
	mustAddToScheme(ewv1alpha1.AddToScheme, scheme)

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("testdata")},
		},
	}

	cfg, err := env.Start()
	if err != nil {
		panic("envtest.Start: " + err.Error())
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		panic("client.New: " + err.Error())
	}

	dynClient, err = dynamic.NewForConfig(cfg)
	if err != nil {
		panic("dynamic.NewForConfig: " + err.Error())
	}

	// Build the in-process AuditEventHandler that connects to the real cluster.
	auditHandler = &auditmonitor.AuditEventHandler{
		Detector: &auditmonitor.TouchDetector{Client: k8sClient},
		Recorder: &auditmonitor.TouchRecorder{
			Client:         k8sClient,
			EventNamespace: eventNamespace,
		},
	}

	// Start the admission webhook server so ManualTouchCheck rules can be
	// evaluated during tests that exercise the ChangeValidator path.
	wh := webhook.NewServer(webhook.Options{
		Host:    env.WebhookInstallOptions.LocalServingHost,
		Port:    env.WebhookInstallOptions.LocalServingPort,
		CertDir: env.WebhookInstallOptions.LocalServingCertDir,
	})
	admHandler := &ewwebhook.AdmissionHandler{
		Client:        k8sClient,
		DynamicClient: dynClient,
		Decoder:       admission.NewDecoder(scheme),
	}
	wh.Register("/validate", &webhook.Admission{Handler: admHandler})

	go func() {
		if err := wh.Start(mgrCtx); err != nil && mgrCtx.Err() == nil {
			panic("webhook server: " + err.Error())
		}
	}()

	// Wait for the webhook TLS port to be ready.
	if err := waitForWebhook(
		env.WebhookInstallOptions.LocalServingHost,
		env.WebhookInstallOptions.LocalServingPort,
	); err != nil {
		panic(err.Error())
	}

	createNamespace(testNamespace)
	createNamespace(eventNamespace)

	code := m.Run()

	mgrCancel()
	_ = env.Stop()
	os.Exit(code)
}

// mustAddToScheme panics if fn returns an error.
func mustAddToScheme(fn func(*k8sruntime.Scheme) error, s *k8sruntime.Scheme) {
	if err := fn(s); err != nil {
		panic(err)
	}
}

// createNamespace creates the given namespace in the envtest cluster, ignoring
// AlreadyExists errors.
func createNamespace(name string) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := k8sClient.Create(context.Background(), ns); err != nil && !kerrors.IsAlreadyExists(err) {
		panic("create namespace " + name + ": " + err.Error())
	}
}

// waitForWebhook polls until a TLS connection to host:port succeeds or the
// 30-second deadline elapses.
func waitForWebhook(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	deadline := time.Now().Add(30 * time.Second)
	dialer := &net.Dialer{Timeout: time.Second}
	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // self-signed cert in test environment
	for time.Now().Before(deadline) {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("webhook server at %s did not become ready within 30s", addr)
}

// cleanupResources removes all test resources from both the test namespace and
// the event namespace.
func cleanupResources(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	// Remove ChangeValidators first (so they don't block subsequent deletions).
	guardList := &ewv1alpha1.ChangeValidatorList{}
	if err := k8sClient.List(ctx, guardList); err == nil {
		for i := range guardList.Items {
			_ = k8sClient.Delete(ctx, &guardList.Items[i])
		}
	}

	// Wait until no guards remain.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		list := &ewv1alpha1.ChangeValidatorList{}
		if err := k8sClient.List(ctx, list, client.InNamespace(testNamespace)); err == nil && len(list.Items) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Remove ManualTouchMonitors.
	monList := &ewv1alpha1.ManualTouchMonitorList{}
	if err := k8sClient.List(ctx, monList); err == nil {
		for i := range monList.Items {
			_ = k8sClient.Delete(ctx, &monList.Items[i])
		}
	}

	// Remove ManualTouchEvents from the event namespace.
	mteList := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(ctx, mteList, client.InNamespace(eventNamespace)); err == nil {
		for i := range mteList.Items {
			_ = k8sClient.Delete(ctx, &mteList.Items[i])
		}
	}

	// Remove Services.
	svcList := &corev1.ServiceList{}
	if err := k8sClient.List(ctx, svcList, client.InNamespace(testNamespace)); err == nil {
		for i := range svcList.Items {
			_ = k8sClient.Delete(ctx, &svcList.Items[i])
		}
	}

	// Remove ConfigMaps.
	cmList := &corev1.ConfigMapList{}
	if err := k8sClient.List(ctx, cmList, client.InNamespace(testNamespace)); err == nil {
		for i := range cmList.Items {
			_ = k8sClient.Delete(ctx, &cmList.Items[i])
		}
	}
}

// postAuditEvent sends an AuditEventList containing event to the in-process
// AuditEventHandler and returns the HTTP response code.
func postAuditEvent(t *testing.T, event auditmonitor.AuditEvent) int {
	t.Helper()
	eventList := auditmonitor.AuditEventList{Items: []auditmonitor.AuditEvent{event}}
	body, err := json.Marshal(eventList)
	if err != nil {
		t.Fatalf("marshal audit event: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
	w := httptest.NewRecorder()
	auditHandler.ServeHTTP(w, req)
	return w.Code
}

// waitForManualTouchEvents polls until the cluster contains exactly want events
// in the eventNamespace, or the deadline is reached.  Returns the event list.
func waitForManualTouchEvents(t *testing.T, want int) []ewv1alpha1.ManualTouchEvent {
	t.Helper()
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		list := &ewv1alpha1.ManualTouchEventList{}
		if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err == nil {
			if len(list.Items) == want {
				return list.Items
			}
		}
		time.Sleep(pollInterval)
	}
	// Final read for the error message.
	list := &ewv1alpha1.ManualTouchEventList{}
	_ = k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace))
	t.Errorf("timed out waiting for %d ManualTouchEvent(s) — found %d", want, len(list.Items))
	return nil
}

// makeKubectlDeleteEvent creates a ResponseComplete DELETE audit event that
// looks like it came from kubectl.
func makeKubectlDeleteEvent(auditID, resource, ns, name string) auditmonitor.AuditEvent {
	return auditmonitor.AuditEvent{
		AuditID:   auditID,
		Verb:      "delete",
		UserAgent: "kubectl/v1.29.0 (linux/amd64) kubernetes/abc1234",
		Stage:     "ResponseComplete",
		SourceIPs: []string{"10.0.0.1"},
		User:      auditmonitor.AuditUser{Username: "kubernetes-admin"},
		ObjectRef: auditmonitor.AuditObjectRef{
			Resource:  resource,
			Namespace: ns,
			Name:      name,
			APIGroup:  "",
		},
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Now()),
	}
}

// createMonitor creates a ManualTouchMonitor CR in testNamespace.
func createMonitor(t *testing.T, name string, resources []string, ops []ewv1alpha1.MonitorOperationType) {
	t.Helper()
	subjects := make([]ewv1alpha1.MonitorSubject, 0, len(resources))
	for _, r := range resources {
		subjects = append(subjects, ewv1alpha1.MonitorSubject{APIGroup: "", Resource: r})
	}
	mon := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:   subjects,
			Operations: ops,
		},
	}
	if err := k8sClient.Create(context.Background(), mon); err != nil {
		t.Fatalf("create ManualTouchMonitor %q: %v", name, err)
	}
}

// ---- Core audit monitor pipeline tests ----

// TestAuditMonitor_CreatesManualTouchEvent verifies the full pipeline:
// a kubectl DELETE audit event matching a ManualTouchMonitor produces a
// ManualTouchEvent CR in the cluster with correct field values.
func TestAuditMonitor_CreatesManualTouchEvent(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	createMonitor(t, "mon-services", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})

	event := makeKubectlDeleteEvent("e2e-audit-001", "services", testNamespace, "my-svc")
	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK from audit handler, got %d", code)
	}

	events := waitForManualTouchEvents(t, 1)
	if len(events) != 1 {
		return // error already reported by waitForManualTouchEvents
	}
	mte := events[0]

	if mte.Spec.User != "kubernetes-admin" {
		t.Errorf("expected User 'kubernetes-admin', got %q", mte.Spec.User)
	}
	if mte.Spec.Operation != "DELETE" {
		t.Errorf("expected Operation 'DELETE', got %q", mte.Spec.Operation)
	}
	if mte.Spec.Resource != "services" {
		t.Errorf("expected Resource 'services', got %q", mte.Spec.Resource)
	}
	if mte.Spec.ResourceName != "my-svc" {
		t.Errorf("expected ResourceName 'my-svc', got %q", mte.Spec.ResourceName)
	}
	if mte.Spec.ResourceNamespace != testNamespace {
		t.Errorf("expected ResourceNamespace %q, got %q", testNamespace, mte.Spec.ResourceNamespace)
	}
	if mte.Spec.AuditID != "e2e-audit-001" {
		t.Errorf("expected AuditID 'e2e-audit-001', got %q", mte.Spec.AuditID)
	}
	if mte.Spec.SourceIP != "10.0.0.1" {
		t.Errorf("expected SourceIP '10.0.0.1', got %q", mte.Spec.SourceIP)
	}
	if mte.Namespace != eventNamespace {
		t.Errorf("expected namespace %q, got %q", eventNamespace, mte.Namespace)
	}
}

// TestAuditMonitor_Idempotent verifies that posting the same audit event
// (same AuditID) twice does not create duplicate ManualTouchEvent CRs.
func TestAuditMonitor_Idempotent(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	createMonitor(t, "mon-idempotent", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})

	event := makeKubectlDeleteEvent("e2e-idempotent-001", "services", testNamespace, "my-svc")

	// Post the same event twice.
	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("first POST: expected 200, got %d", code)
	}
	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("second POST: expected 200, got %d", code)
	}

	// Wait briefly, then verify only one event was recorded.
	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected exactly 1 ManualTouchEvent (idempotent), got %d", len(list.Items))
	}
}

// TestAuditMonitor_IgnoresControllerUserAgent verifies that events with a
// non-kubectl User-Agent (e.g. kube-controller-manager) are not recorded.
func TestAuditMonitor_IgnoresControllerUserAgent(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	createMonitor(t, "mon-ctrl-agent", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})

	event := makeKubectlDeleteEvent("e2e-ctrl-001", "services", testNamespace, "my-svc")
	event.UserAgent = "kube-controller-manager/v1.29.0 (linux/amd64)"

	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	// No ManualTouchEvent should appear.
	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents for controller user-agent, got %d", len(list.Items))
	}
}

// TestAuditMonitor_IgnoresUnwatchedResource verifies that a kubectl DELETE on
// a resource not covered by any ManualTouchMonitor is not recorded.
func TestAuditMonitor_IgnoresUnwatchedResource(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	// Monitor only watches "services"; event targets "configmaps".
	createMonitor(t, "mon-unwatched", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})

	event := makeKubectlDeleteEvent("e2e-unwatched-001", "configmaps", testNamespace, "my-cm")

	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents for unwatched resource, got %d", len(list.Items))
	}
}

// TestAuditMonitor_IgnoresNonResponseCompleteStage verifies that audit events
// not in the ResponseComplete stage are not processed.
func TestAuditMonitor_IgnoresNonResponseCompleteStage(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	createMonitor(t, "mon-stage", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})

	event := makeKubectlDeleteEvent("e2e-stage-001", "services", testNamespace, "my-svc")
	event.Stage = "RequestReceived" // not ResponseComplete

	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents for non-ResponseComplete stage, got %d", len(list.Items))
	}
}

// TestAuditMonitor_ExcludesServiceAccount verifies that events from an
// excluded service account are not recorded even if other conditions match.
func TestAuditMonitor_ExcludesServiceAccount(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	mon := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon-excl-sa", Namespace: testNamespace},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:   []ewv1alpha1.MonitorSubject{{APIGroup: "", Resource: "services"}},
			Operations: []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete},
			ExcludeServiceAccounts: []string{
				"system:serviceaccount:ci:pipeline-bot",
			},
		},
	}
	if err := k8sClient.Create(context.Background(), mon); err != nil {
		t.Fatalf("create ManualTouchMonitor: %v", err)
	}

	event := makeKubectlDeleteEvent("e2e-excl-sa-001", "services", testNamespace, "my-svc")
	event.User.Username = "system:serviceaccount:ci:pipeline-bot"

	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents for excluded service account, got %d", len(list.Items))
	}
}

// TestAuditMonitor_MultipleMonitors verifies that a single audit event
// produces one ManualTouchEvent per matching ManualTouchMonitor.
func TestAuditMonitor_MultipleMonitors(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	createMonitor(t, "mon-multi-1", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})
	createMonitor(t, "mon-multi-2", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete})

	event := makeKubectlDeleteEvent("e2e-multi-001", "services", testNamespace, "my-svc")

	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	events := waitForManualTouchEvents(t, 2)
	if len(events) != 2 {
		return
	}

	// Verify each event refers to a different monitor.
	monitorNames := map[string]bool{}
	for _, mte := range events {
		monitorNames[mte.Spec.MonitorName] = true
	}
	if !monitorNames["mon-multi-1"] || !monitorNames["mon-multi-2"] {
		t.Errorf("expected events for both monitors, got monitor names: %v", monitorNames)
	}
}

// TestAuditMonitor_PatchMapsToUpdate verifies that a PATCH verb audit event
// is treated as an UPDATE operation and matches a monitor configured for UPDATE.
func TestAuditMonitor_PatchMapsToUpdate(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	createMonitor(t, "mon-patch", []string{"services"}, []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationUpdate})

	event := makeKubectlDeleteEvent("e2e-patch-001", "services", testNamespace, "my-svc")
	event.Verb = "patch"

	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	events := waitForManualTouchEvents(t, 1)
	if len(events) == 1 {
		if events[0].Spec.Operation != "UPDATE" {
			t.Errorf("expected Operation 'UPDATE' for PATCH verb, got %q", events[0].Spec.Operation)
		}
	}
}

// TestAuditMonitor_CustomUserAgentPattern verifies that a ManualTouchMonitor
// with a custom userAgentPattern fires only for matching user agents and
// ignores kubectl events.
func TestAuditMonitor_CustomUserAgentPattern(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	mon := &ewv1alpha1.ManualTouchMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "mon-custom-ua", Namespace: testNamespace},
		Spec: ewv1alpha1.ManualTouchMonitorSpec{
			Subjects:          []ewv1alpha1.MonitorSubject{{APIGroup: "", Resource: "services"}},
			Operations:        []ewv1alpha1.MonitorOperationType{ewv1alpha1.MonitorOperationDelete},
			UserAgentPatterns: []string{`^my-custom-tool/`},
		},
	}
	if err := k8sClient.Create(context.Background(), mon); err != nil {
		t.Fatalf("create ManualTouchMonitor: %v", err)
	}

	// kubectl event should NOT be recorded (custom pattern only matches my-custom-tool/).
	kubectlEvent := makeKubectlDeleteEvent("e2e-custom-ua-kubectl", "services", testNamespace, "my-svc")
	if code := postAuditEvent(t, kubectlEvent); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}
	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents for kubectl with custom pattern, got %d", len(list.Items))
	}

	// Custom tool event SHOULD be recorded.
	customEvent := makeKubectlDeleteEvent("e2e-custom-ua-tool", "services", testNamespace, "my-svc")
	customEvent.UserAgent = "my-custom-tool/v2.0.0"
	if code := postAuditEvent(t, customEvent); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	waitForManualTouchEvents(t, 1)
}

// ---- ManualTouchCheck admission rule integration tests ----

// makeService creates a Service in testNamespace.
func makeService(t *testing.T, name string) *corev1.Service {
	t.Helper()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80}},
		},
	}
	if err := k8sClient.Create(context.Background(), svc); err != nil {
		t.Fatalf("create Service %q: %v", name, err)
	}
	return svc
}

// makeManualTouchEvent creates a ManualTouchEvent CR directly in the
// eventNamespace, simulating what the audit monitor would record.
func makeManualTouchEvent(t *testing.T, name, resource, ns, resName, apiGroup string, age time.Duration) {
	t.Helper()
	mte := &ewv1alpha1.ManualTouchEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: eventNamespace,
			Labels: map[string]string{
				auditmonitor.LabelResource:          resource,
				auditmonitor.LabelResourceNamespace: ns,
				auditmonitor.LabelResourceName:      resName,
				auditmonitor.LabelAPIGroup:          apiGroup,
				auditmonitor.LabelOperation:         "DELETE",
			},
		},
		Spec: ewv1alpha1.ManualTouchEventSpec{
			Timestamp:         metav1.NewTime(time.Now().Add(-age)),
			User:              "kubernetes-admin",
			UserAgent:         "kubectl/v1.29.0",
			Operation:         "DELETE",
			Resource:          resource,
			ResourceName:      resName,
			ResourceNamespace: ns,
			AuditID:           name + "-audit",
			MonitorName:       "mon",
			MonitorNamespace:  testNamespace,
		},
	}
	if err := k8sClient.Create(context.Background(), mte); err != nil {
		t.Fatalf("create ManualTouchEvent %q: %v", name, err)
	}
}

// makeManualTouchCheckGuard creates a ChangeValidator with a ManualTouchCheck
// rule on Services in testNamespace.
func makeManualTouchCheckGuard(t *testing.T, name, windowDuration string) {
	t.Helper()
	guard := &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{
				{
					Name:    "no-recent-manual-touch",
					Type:    ewv1alpha1.RuleTypeManualTouchCheck,
					Message: "a recent manual touch was recorded; automated change denied",
					ManualTouchCheck: &ewv1alpha1.ManualTouchCheck{
						WindowDuration: windowDuration,
						EventNamespace: eventNamespace,
					},
				},
			},
		},
	}
	if err := k8sClient.Create(context.Background(), guard); err != nil {
		t.Fatalf("create ChangeValidator %q: %v", name, err)
	}
}

// TestManualTouchCheck_DeniesWhenRecentEvent verifies that an automated
// UPDATE on a Service is denied when a ManualTouchEvent was recently recorded
// for that same Service.
func TestManualTouchCheck_DeniesWhenRecentEvent(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	svc := makeService(t, "guarded-svc")

	// Record a manual touch that happened 5 minutes ago (within the 1h window).
	makeManualTouchEvent(t, "mte-recent", "services", testNamespace, "guarded-svc", "", 5*time.Minute)

	makeManualTouchCheckGuard(t, "mtc-guard", "1h")

	// Attempt an automated UPDATE — should be denied.
	svc.Labels = map[string]string{"automated": "true"}
	err := k8sClient.Update(context.Background(), svc)
	if err == nil {
		t.Fatal("expected UPDATE to be denied by ManualTouchCheck rule")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestManualTouchCheck_AllowsWhenNoEvent verifies that an automated UPDATE is
// allowed when no ManualTouchEvent exists for the resource.
func TestManualTouchCheck_AllowsWhenNoEvent(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	svc := makeService(t, "unguarded-svc")

	// No ManualTouchEvent — guard should not fire.
	makeManualTouchCheckGuard(t, "mtc-guard-no-event", "1h")

	svc.Labels = map[string]string{"automated": "true"}
	if err := k8sClient.Update(context.Background(), svc); err != nil {
		t.Fatalf("expected UPDATE to be allowed when no ManualTouchEvent exists: %v", err)
	}
}

// TestManualTouchCheck_AllowsWhenEventOutsideWindow verifies that an automated
// UPDATE is allowed when the most recent ManualTouchEvent is older than the
// configured window.
func TestManualTouchCheck_AllowsWhenEventOutsideWindow(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	svc := makeService(t, "old-touch-svc")

	// Touch event happened 2 hours ago; window is 1h — outside window.
	makeManualTouchEvent(t, "mte-old", "services", testNamespace, "old-touch-svc", "", 2*time.Hour)

	makeManualTouchCheckGuard(t, "mtc-guard-old", "1h")

	svc.Labels = map[string]string{"automated": "true"}
	if err := k8sClient.Update(context.Background(), svc); err != nil {
		t.Fatalf("expected UPDATE to be allowed when ManualTouchEvent is outside window: %v", err)
	}
}

// TestManualTouchCheck_AllowsDifferentResource verifies that a ManualTouchEvent
// for a different resource does not block an automated UPDATE on a different
// Service.
func TestManualTouchCheck_AllowsDifferentResource(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	svc := makeService(t, "my-svc-a")

	// Manual touch was on "my-svc-b", not "my-svc-a".
	makeManualTouchEvent(t, "mte-other-svc", "services", testNamespace, "my-svc-b", "", 5*time.Minute)

	makeManualTouchCheckGuard(t, "mtc-guard-other", "1h")

	svc.Labels = map[string]string{"automated": "true"}
	if err := k8sClient.Update(context.Background(), svc); err != nil {
		t.Fatalf("expected UPDATE on my-svc-a to be allowed when touch was on my-svc-b: %v", err)
	}
}

// TestAuditMonitor_NoMonitors verifies that a kubectl audit event produces no
// ManualTouchEvent CRs when no ManualTouchMonitor resources exist in the
// cluster.
func TestAuditMonitor_NoMonitors(t *testing.T) {
	cleanupResources(t)
	t.Cleanup(func() { cleanupResources(t) })

	// No monitors — nothing should be recorded.
	event := makeKubectlDeleteEvent("e2e-no-mon-001", "services", testNamespace, "my-svc")
	if code := postAuditEvent(t, event); code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", code)
	}

	time.Sleep(200 * time.Millisecond)
	list := &ewv1alpha1.ManualTouchEventList{}
	if err := k8sClient.List(context.Background(), list, client.InNamespace(eventNamespace)); err != nil {
		t.Fatalf("listing ManualTouchEvents: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 ManualTouchEvents with no monitors, got %d", len(list.Items))
	}
}
