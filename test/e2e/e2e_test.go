// Package e2e contains end-to-end tests for the EarlyWatch admission webhook.
// The tests run against a real Kubernetes control plane provided by
// controller-runtime's envtest package, which starts an actual kube-apiserver
// and etcd process locally.
//
// Prerequisites – install the Kubernetes API server and etcd binaries:
//
//	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
//	export KUBEBUILDER_ASSETS=$(setup-envtest use --print path)
//
// Run:
//
//	go test -tags=e2e ./test/e2e/... -v
//
//go:build e2e

package e2e_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	ewwebhook "github.com/brendandburns/early-watch/pkg/webhook"
)

// testNamespace is the Kubernetes namespace used by all e2e tests.
const testNamespace = "ew-e2e-test"

var (
	// k8sClient is a direct (non-caching) client used for all test setup and
	// assertions.  A direct client is used so that list/get calls always
	// reflect the latest etcd state without waiting for a cache sync; this is
	// important for the cleanup helper, which must see deleted ChangeValidators
	// before attempting to delete guarded resources.
	k8sClient client.Client
	dynClient  dynamic.Interface

	// mgrCtx is cancelled in TestMain after all tests have run in order to
	// shut down the webhook server.
	mgrCtx    context.Context
	mgrCancel context.CancelFunc
)

// TestMain sets up a local Kubernetes control plane via envtest, registers the
// EarlyWatch admission webhook handler in-process, and then runs all tests.
func TestMain(m *testing.M) {
	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	mgrCtx, mgrCancel = context.WithCancel(context.Background())

	scheme := k8sruntime.NewScheme()
	mustAddToScheme(clientgoscheme.AddToScheme, scheme)
	mustAddToScheme(ewv1alpha1.AddToScheme, scheme)

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("testdata")},
		},
	}

	cfg, err := env.Start()
	if err != nil {
		panic("envtest.Start: " + err.Error())
	}

	// Build a direct (non-caching) client for test setup, teardown, and as the
	// AdmissionHandler's client.  Using a direct client ensures the handler
	// always reads from etcd, which avoids cache-sync races during cleanup.
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		panic("client.New: " + err.Error())
	}

	dynClient, err = dynamic.NewForConfig(cfg)
	if err != nil {
		panic("dynamic.NewForConfig: " + err.Error())
	}

	// Start the webhook server on the host/port/certs that envtest prepared.
	srv := webhook.NewServer(webhook.Options{
		Host:    env.WebhookInstallOptions.LocalServingHost,
		Port:    env.WebhookInstallOptions.LocalServingPort,
		CertDir: env.WebhookInstallOptions.LocalServingCertDir,
	})

	handler := &ewwebhook.AdmissionHandler{
		Client:        k8sClient,
		DynamicClient: dynClient,
		Decoder:       admission.NewDecoder(scheme),
	}
	srv.Register("/validate", &webhook.Admission{Handler: handler})

	go func() {
		if err := srv.Start(mgrCtx); err != nil && mgrCtx.Err() == nil {
			panic("webhook server: " + err.Error())
		}
	}()

	if err := waitForWebhook(
		env.WebhookInstallOptions.LocalServingHost,
		env.WebhookInstallOptions.LocalServingPort,
	); err != nil {
		panic(err.Error())
	}

	createTestNamespace()

	code := m.Run()

	mgrCancel()
	_ = env.Stop()
	os.Exit(code)
}

// mustAddToScheme panics if the scheme-builder function returns an error.
func mustAddToScheme(fn func(*k8sruntime.Scheme) error, s *k8sruntime.Scheme) {
	if err := fn(s); err != nil {
		panic(err)
	}
}

// waitForWebhook polls until a TLS connection to host:port succeeds, or the
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

// createTestNamespace creates the shared test namespace once at startup.
func createTestNamespace() {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	if err := k8sClient.Create(context.Background(), ns); err != nil && !kerrors.IsAlreadyExists(err) {
		panic("create test namespace: " + err.Error())
	}
}

// cleanupNamespace removes all ChangeValidators, ConfigMaps, Services, and Pods
// from testNamespace.  It deletes ChangeValidators first and waits for them to
// disappear from the API server so that subsequent deletes of guarded resources
// are not blocked by the webhook.
func cleanupNamespace(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	// Delete all ChangeValidators.  The webhook allows this because no guard
	// protects earlywatch.io/changevalidators resources.
	guardList := &ewv1alpha1.ChangeValidatorList{}
	if err := k8sClient.List(ctx, guardList, client.InNamespace(testNamespace)); err == nil {
		for i := range guardList.Items {
			_ = k8sClient.Delete(ctx, &guardList.Items[i])
		}
	}

	// Also delete any cluster-wide guards that may have been created.
	allGuards := &ewv1alpha1.ChangeValidatorList{}
	if err := k8sClient.List(ctx, allGuards); err == nil {
		for i := range allGuards.Items {
			_ = k8sClient.Delete(ctx, &allGuards.Items[i])
		}
	}

	// Wait until no guards remain so the webhook no longer blocks deletions
	// of resources that were protected by those guards.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		list := &ewv1alpha1.ChangeValidatorList{}
		if err := k8sClient.List(ctx, list, client.InNamespace(testNamespace)); err == nil && len(list.Items) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Delete remaining test resources (errors are ignored – best-effort cleanup).
	cmList := &corev1.ConfigMapList{}
	if err := k8sClient.List(ctx, cmList, client.InNamespace(testNamespace)); err == nil {
		for i := range cmList.Items {
			_ = k8sClient.Delete(ctx, &cmList.Items[i])
		}
	}

	svcList := &corev1.ServiceList{}
	if err := k8sClient.List(ctx, svcList, client.InNamespace(testNamespace)); err == nil {
		for i := range svcList.Items {
			_ = k8sClient.Delete(ctx, &svcList.Items[i])
		}
	}

	podList := &corev1.PodList{}
	if err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace)); err == nil {
		for i := range podList.Items {
			_ = k8sClient.Delete(ctx, &podList.Items[i])
		}
	}
}

// --- resource helpers ---

// makeChangeGuard creates a ChangeValidator in testNamespace.
func makeChangeGuard(t *testing.T, guard *ewv1alpha1.ChangeValidator) {
	t.Helper()
	guard.Namespace = testNamespace
	if err := k8sClient.Create(context.Background(), guard); err != nil {
		t.Fatalf("create ChangeGuard %q: %v", guard.Name, err)
	}
}

// makeConfigMap creates a ConfigMap in testNamespace.
func makeConfigMap(t *testing.T, name string) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Data:       map[string]string{"key": "value"},
	}
	if err := k8sClient.Create(context.Background(), cm); err != nil {
		t.Fatalf("create ConfigMap %q: %v", name, err)
	}
	return cm
}

// makeService creates a Service in testNamespace with the given label selector.
func makeService(t *testing.T, name string, selector map[string]string) *corev1.Service {
	t.Helper()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    []corev1.ServicePort{{Port: 80}},
		},
	}
	if err := k8sClient.Create(context.Background(), svc); err != nil {
		t.Fatalf("create Service %q: %v", name, err)
	}
	return svc
}

// makePod creates a Pod in testNamespace with the given labels.
func makePod(t *testing.T, name string, labels map[string]string) *corev1.Pod {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "pause", Image: "pause:3.9"}},
		},
	}
	if err := k8sClient.Create(context.Background(), pod); err != nil {
		t.Fatalf("create Pod %q: %v", name, err)
	}
	return pod
}

func boolPtr(b bool) *bool { return &b }

// --- approval-check helper ---

// generateKeyPair generates a 2048-bit RSA key pair and returns the private
// key together with the PEM-encoded public key ready to embed in a
// ChangeValidator ApprovalCheck rule.
func generateKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return privKey, pubPEM
}

// approveResource computes the canonical resource path, signs it with the
// given private key (RSA-PSS SHA-256), and patches the approval annotation
// onto the resource — mirroring exactly what the approve CLI tool does.
func approveResource(t *testing.T, privKey *rsa.PrivateKey, annotationKey, group, version, resource, namespace, name string) {
	t.Helper()

	path := ewwebhook.ResourcePath(group, version, resource, namespace, name)

	digest := sha256.Sum256([]byte(path))
	sig, err := rsa.SignPSS(rand.Reader, privKey, crypto.SHA256, digest[:], nil)
	if err != nil {
		t.Fatalf("rsa.SignPSS: %v", err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Fetch the ConfigMap, add the annotation, then update.
	cm := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, cm); err != nil {
		t.Fatalf("Get ConfigMap %q for annotation: %v", name, err)
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	cm.Annotations[annotationKey] = sigB64
	if err := k8sClient.Update(context.Background(), cm); err != nil {
		t.Fatalf("Update ConfigMap %q with approval annotation: %v", name, err)
	}
}

// --- test cases ---

// TestNoGuards_AllowsRequests verifies that when no ChangeValidators exist all
// admission requests are allowed through.
func TestNoGuards_AllowsRequests(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	cm := makeConfigMap(t, "no-guard-cm")
	if err := k8sClient.Delete(context.Background(), cm); err != nil {
		t.Fatalf("expected ConfigMap DELETE to be allowed with no guards: %v", err)
	}
}

// TestExpressionCheck_DeniesMatchingOperation verifies that an ExpressionCheck
// guard denies the admission request whose operation matches its expression.
func TestExpressionCheck_DeniesMatchingOperation(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-cm-delete"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "no-delete",
				Type:    ewv1alpha1.RuleTypeExpressionCheck,
				Message: "configmap deletion is not allowed",
				ExpressionCheck: &ewv1alpha1.ExpressionCheck{
					Expression: "operation == 'DELETE'",
				},
			}},
		},
	})

	cm := makeConfigMap(t, "protected-cm")
	err := k8sClient.Delete(context.Background(), cm)
	if err == nil {
		t.Fatal("expected DELETE to be denied by the ExpressionCheck guard")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestExpressionCheck_AllowsNonTargetedOperation verifies that an
// ExpressionCheck guard does not block operations it was not configured for.
func TestExpressionCheck_AllowsNonTargetedOperation(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-cm-delete-only"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "no-delete",
				Type:    ewv1alpha1.RuleTypeExpressionCheck,
				Message: "deletion denied",
				ExpressionCheck: &ewv1alpha1.ExpressionCheck{
					Expression: "operation == 'DELETE'",
				},
			}},
		},
	})

	// CREATE is not in the guard's Operations list, so the guard should not apply.
	if makeConfigMap(t, "create-allowed-cm") == nil {
		t.Fatal("expected ConfigMap CREATE to succeed (guard only covers DELETE)")
	}
}

// TestExistingResources_DeniesWhenDependentsPresent verifies that an
// ExistingResources rule denies a DELETE when dependent resources exist in the
// same namespace.
func TestExistingResources_DeniesWhenDependentsPresent(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makePod(t, "running-pod", nil)

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "block-cm-if-pods"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "pods-present",
				Type:    ewv1alpha1.RuleTypeExistingResources,
				Message: "pods still running in namespace",
				ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
					APIGroup:      "",
					Resource:      "pods",
					SameNamespace: boolPtr(true),
				},
			}},
		},
	})

	cm := makeConfigMap(t, "blocked-cm")
	err := k8sClient.Delete(context.Background(), cm)
	if err == nil {
		t.Fatal("expected ConfigMap DELETE to be denied because pods exist")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestExistingResources_AllowsWhenNoDependents verifies that an
// ExistingResources rule allows a DELETE when no dependent resources are found.
func TestExistingResources_AllowsWhenNoDependents(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	// No pods are created; the guard should not fire.
	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "block-cm-if-pods"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "pods-present",
				Type:    ewv1alpha1.RuleTypeExistingResources,
				Message: "pods still running",
				ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
					APIGroup:      "",
					Resource:      "pods",
					SameNamespace: boolPtr(true),
				},
			}},
		},
	})

	cm := makeConfigMap(t, "unblocked-cm")
	if err := k8sClient.Delete(context.Background(), cm); err != nil {
		t.Fatalf("expected ConfigMap DELETE to be allowed with no pods: %v", err)
	}
}

// TestExistingResources_LabelSelector_DeniesWhenMatchingPodsPresent verifies
// that a guard using labelSelectorFromField blocks a Service DELETE when Pods
// matching the Service's spec.selector exist in the same namespace.
func TestExistingResources_LabelSelector_DeniesWhenMatchingPodsPresent(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makePod(t, "app-pod", map[string]string{"app": "my-app"})

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "protect-service"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "matching-pods-exist",
				Type:    ewv1alpha1.RuleTypeExistingResources,
				Message: "matching Pods are still running",
				ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
					APIGroup:               "",
					Resource:               "pods",
					LabelSelectorFromField: "spec.selector",
					SameNamespace:          boolPtr(true),
				},
			}},
		},
	})

	svc := makeService(t, "my-service", map[string]string{"app": "my-app"})
	err := k8sClient.Delete(context.Background(), svc)
	if err == nil {
		t.Fatal("expected Service DELETE to be denied because matching Pods exist")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestExistingResources_LabelSelector_AllowsWhenNoMatchingPods verifies that a
// guard using labelSelectorFromField allows a Service DELETE when no Pods
// matching the Service's spec.selector exist.
func TestExistingResources_LabelSelector_AllowsWhenNoMatchingPods(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	// Pod has a different label and will not match the Service selector.
	makePod(t, "other-pod", map[string]string{"app": "other-app"})

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "protect-service-no-match"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "matching-pods-exist",
				Type:    ewv1alpha1.RuleTypeExistingResources,
				Message: "matching Pods are still running",
				ExistingResources: &ewv1alpha1.ExistingResourcesCheck{
					APIGroup:               "",
					Resource:               "pods",
					LabelSelectorFromField: "spec.selector",
					SameNamespace:          boolPtr(true),
				},
			}},
		},
	})

	svc := makeService(t, "my-service-2", map[string]string{"app": "my-app"})
	if err := k8sClient.Delete(context.Background(), svc); err != nil {
		t.Fatalf("expected Service DELETE to be allowed when no matching Pods exist: %v", err)
	}
}

// TestApprovalCheck_DeleteDeniedWithoutAnnotation verifies that an
// ApprovalCheck guard blocks a DELETE when the resource has no approval
// annotation.
func TestApprovalCheck_DeleteDeniedWithoutAnnotation(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	_, pubPEM := generateKeyPair(t)

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "require-approval"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "check-approval",
				Type:    ewv1alpha1.RuleTypeApprovalCheck,
				Message: "resource must be approved before deletion",
				ApprovalCheck: &ewv1alpha1.ApprovalCheck{
					PublicKey: pubPEM,
				},
			}},
		},
	})

	cm := makeConfigMap(t, "unapproved-cm")
	err := k8sClient.Delete(context.Background(), cm)
	if err == nil {
		t.Fatal("expected DELETE to be denied because no approval annotation is present")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestApprovalCheck_DeleteAllowedWithValidApproval verifies the full approval
// flow: a DELETE is initially denied, then the approve tool logic signs the
// resource path and annotates the resource, and the subsequent DELETE succeeds.
func TestApprovalCheck_DeleteAllowedWithValidApproval(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	privKey, pubPEM := generateKeyPair(t)
	const annotationKey = "earlywatch.io/approved"

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "require-approval-full"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "check-approval",
				Type:    ewv1alpha1.RuleTypeApprovalCheck,
				Message: "resource must be approved before deletion",
				ApprovalCheck: &ewv1alpha1.ApprovalCheck{
					PublicKey:     pubPEM,
					AnnotationKey: annotationKey,
				},
			}},
		},
	})

	cm := makeConfigMap(t, "approved-cm")

	// Step 1: DELETE without approval must be denied.
	err := k8sClient.Delete(context.Background(), cm)
	if err == nil {
		t.Fatal("expected initial DELETE to be denied because approval annotation is absent")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}

	// Step 2: Apply the approval annotation using the same logic as the approve
	// CLI tool — sign the canonical resource path with the private key.
	approveResource(t, privKey, annotationKey, "", "v1", "configmaps", testNamespace, "approved-cm")

	// Step 3: DELETE with the approval annotation must now be allowed.
	// Re-fetch the ConfigMap so we have the up-to-date resourceVersion.
	fresh := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "approved-cm"}, fresh); err != nil {
		t.Fatalf("re-fetch ConfigMap after annotation: %v", err)
	}
	if err := k8sClient.Delete(context.Background(), fresh); err != nil {
		t.Fatalf("expected DELETE to succeed after approval annotation was set: %v", err)
	}
}

// TestApprovalCheck_DeleteDeniedWithWrongKey verifies that a DELETE is denied
// when the approval annotation was signed with a different private key than the
// one configured in the rule.
func TestApprovalCheck_DeleteDeniedWithWrongKey(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	privKey1, _ := generateKeyPair(t)
	_, pubPEM2 := generateKeyPair(t) // different key pair

	const annotationKey = "earlywatch.io/approved"

	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "require-approval-wrong-key"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "check-approval",
				Type:    ewv1alpha1.RuleTypeApprovalCheck,
				Message: "resource must be approved before deletion",
				ApprovalCheck: &ewv1alpha1.ApprovalCheck{
					PublicKey:     pubPEM2, // webhook verifies with key2
					AnnotationKey: annotationKey,
				},
			}},
		},
	})

	cm := makeConfigMap(t, "wrong-key-cm")

	// Sign with key1 but the guard expects key2.
	approveResource(t, privKey1, annotationKey, "", "v1", "configmaps", testNamespace, "wrong-key-cm")

	fresh := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "wrong-key-cm"}, fresh); err != nil {
		t.Fatalf("re-fetch ConfigMap: %v", err)
	}
	err := k8sClient.Delete(context.Background(), fresh)
	if err == nil {
		t.Fatal("expected DELETE to be denied because the annotation was signed with the wrong key")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}
