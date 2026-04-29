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
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	k8sclientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	ewinstall "github.com/brendandburns/early-watch/pkg/install"
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
	dynClient dynamic.Interface

	// restCfg is the REST config for the envtest cluster.  It is exposed so
	// that TestInstallUninstall can build a temporary kubeconfig and exercise
	// the install/uninstall code paths against the same cluster.
	restCfg *rest.Config

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
	restCfg = cfg

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

// cleanupNamespace removes all ChangeValidators, ClusterChangeValidators,
// ConfigMaps, Services, and Pods from testNamespace.  It deletes validators
// first and waits for them to disappear from the API server so that subsequent
// deletes of guarded resources are not blocked by the webhook.
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

	// Also delete any namespace-scoped guards from all namespaces that may have been created.
	allGuards := &ewv1alpha1.ChangeValidatorList{}
	if err := k8sClient.List(ctx, allGuards); err == nil {
		for i := range allGuards.Items {
			_ = k8sClient.Delete(ctx, &allGuards.Items[i])
		}
	}

	// Delete all ClusterChangeValidators.
	ccvList := &ewv1alpha1.ClusterChangeValidatorList{}
	if err := k8sClient.List(ctx, ccvList); err == nil {
		for i := range ccvList.Items {
			_ = k8sClient.Delete(ctx, &ccvList.Items[i])
		}
	}

	// Wait until no ChangeValidators or ClusterChangeValidators remain so the
	// webhook no longer blocks deletions of resources that were protected.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		nsList := &ewv1alpha1.ChangeValidatorList{}
		clList := &ewv1alpha1.ClusterChangeValidatorList{}
		nsGone := k8sClient.List(ctx, nsList, client.InNamespace(testNamespace)) == nil && len(nsList.Items) == 0
		clGone := k8sClient.List(ctx, clList) == nil && len(clList.Items) == 0
		if nsGone && clGone {
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

// makeClusterChangeGuard creates a ClusterChangeValidator (cluster-scoped).
func makeClusterChangeGuard(t *testing.T, guard *ewv1alpha1.ClusterChangeValidator) {
	t.Helper()
	if err := k8sClient.Create(context.Background(), guard); err != nil {
		t.Fatalf("create ClusterChangeValidator %q: %v", guard.Name, err)
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

// makeConfigMapInNamespace creates a ConfigMap in the specified namespace.
func makeConfigMapInNamespace(t *testing.T, name, namespace string) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string]string{"key": "value"},
	}
	if err := k8sClient.Create(context.Background(), cm); err != nil {
		t.Fatalf("create ConfigMap %q in namespace %q: %v", name, namespace, err)
	}
	return cm
}

// createLabeledNamespace creates a namespace with the given labels and
// registers a cleanup function to delete it (and any ConfigMaps inside it)
// when the test ends.
func createLabeledNamespace(t *testing.T, name string, labels map[string]string) {
	t.Helper()
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
	if err := k8sClient.Create(ctx, ns); err != nil && !kerrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %q: %v", name, err)
	}
	t.Cleanup(func() {
		// Remove any ConfigMaps from the namespace before deleting it.
		cmList := &corev1.ConfigMapList{}
		if err := k8sClient.List(ctx, cmList, client.InNamespace(name)); err == nil {
			for i := range cmList.Items {
				_ = k8sClient.Delete(ctx, &cmList.Items[i])
			}
		}
		_ = k8sClient.Delete(ctx, ns)
	})
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

	makeConfigMap(t, "wrong-key-cm")

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

// --- install / uninstall tests ---

// writeTestKubeconfig serialises the envtest REST config into a temporary
// kubeconfig file and returns the path.  The file is cleaned up automatically
// when the test ends.
func writeTestKubeconfig(t *testing.T) string {
	t.Helper()

	kubecfg := k8sclientcmdapi.NewConfig()
	kubecfg.Clusters["e2e"] = &k8sclientcmdapi.Cluster{
		Server:                   restCfg.Host,
		CertificateAuthorityData: restCfg.CAData,
	}
	kubecfg.AuthInfos["e2e"] = &k8sclientcmdapi.AuthInfo{
		ClientCertificateData: restCfg.CertData,
		ClientKeyData:         restCfg.KeyData,
	}
	kubecfg.Contexts["e2e"] = &k8sclientcmdapi.Context{
		Cluster:  "e2e",
		AuthInfo: "e2e",
	}
	kubecfg.CurrentContext = "e2e"

	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*kubecfg, path); err != nil {
		t.Fatalf("writing test kubeconfig: %v", err)
	}
	return path
}

// makeServicePodSelectorGuard creates a ChangeValidator that applies the
// ServicePodSelectorCheck rule to service UPDATEs in testNamespace.
func makeServicePodSelectorGuard(t *testing.T, name string) {
	t.Helper()
	makeChangeGuard(t, &ewv1alpha1.ChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "services"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{{
				Name:                    "selector-must-keep-pods",
				Type:                    ewv1alpha1.RuleTypeServicePodSelectorCheck,
				Message:                 "service selector change would leave no matching pods",
				ServicePodSelectorCheck: &ewv1alpha1.ServicePodSelectorCheck{},
			}},
		},
	})
}

// TestServicePodSelectorCheck_DeniesWhenOldSelectorHadPods verifies that a
// Service UPDATE is denied when the old selector had matching Pods but the new
// selector would match none.
func TestServicePodSelectorCheck_DeniesWhenOldSelectorHadPods(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makePod(t, "app-pod", map[string]string{"app": "my-app"})
	svc := makeService(t, "my-svc", map[string]string{"app": "my-app"})
	makeServicePodSelectorGuard(t, "svc-selector-guard")

	// Update the selector to one that matches no pods.
	fresh := &corev1.Service{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: svc.Name}, fresh); err != nil {
		t.Fatalf("re-fetch Service: %v", err)
	}
	fresh.Spec.Selector = map[string]string{"app": "no-such-app"}
	err := k8sClient.Update(context.Background(), fresh)
	if err == nil {
		t.Fatal("expected Service UPDATE to be denied: old selector had pods but new selector matches none")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestServicePodSelectorCheck_AllowsWhenNewSelectorStillHasPods verifies that a
// Service UPDATE is allowed when both the old and new selectors have matching Pods.
func TestServicePodSelectorCheck_AllowsWhenNewSelectorStillHasPods(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makePod(t, "app-pod-old", map[string]string{"app": "my-app"})
	makePod(t, "app-pod-new", map[string]string{"app": "new-app"})
	svc := makeService(t, "my-svc-keep", map[string]string{"app": "my-app"})
	makeServicePodSelectorGuard(t, "svc-selector-guard-allow")

	fresh := &corev1.Service{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: svc.Name}, fresh); err != nil {
		t.Fatalf("re-fetch Service: %v", err)
	}
	fresh.Spec.Selector = map[string]string{"app": "new-app"}
	if err := k8sClient.Update(context.Background(), fresh); err != nil {
		t.Fatalf("expected Service UPDATE to be allowed when new selector also has matching pods: %v", err)
	}
}

// TestServicePodSelectorCheck_AllowsWhenOldSelectorHadNoPods verifies that a
// Service UPDATE is allowed when the old selector had no matching Pods (there is
// nothing to protect).
func TestServicePodSelectorCheck_AllowsWhenOldSelectorHadNoPods(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	// Pod is present but does not match the service selector.
	makePod(t, "other-pod", map[string]string{"app": "other-app"})
	svc := makeService(t, "my-svc-no-match", map[string]string{"app": "my-app"})
	makeServicePodSelectorGuard(t, "svc-selector-guard-no-pods")

	fresh := &corev1.Service{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: svc.Name}, fresh); err != nil {
		t.Fatalf("re-fetch Service: %v", err)
	}
	fresh.Spec.Selector = map[string]string{"app": "completely-different"}
	if err := k8sClient.Update(context.Background(), fresh); err != nil {
		t.Fatalf("expected Service UPDATE to be allowed when old selector had no matching pods: %v", err)
	}
}

// TestServicePodSelectorCheck_AllowsNonUpdateOperation verifies that the guard
// does not block Service creates.
func TestServicePodSelectorCheck_AllowsNonUpdateOperation(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makeServicePodSelectorGuard(t, "svc-selector-guard-create")

	// CREATE must always be allowed by this guard (it only covers UPDATE).
	if makeService(t, "new-svc", map[string]string{"app": "my-app"}) == nil {
		t.Fatal("expected Service CREATE to succeed")
	}
}

// TestServicePodSelectorCheck_AllowsHeadlessService verifies that a headless
// service (clusterIP=None) is exempt from the check even when its selector
// previously had matching pods.
func TestServicePodSelectorCheck_AllowsHeadlessService(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makePod(t, "stateful-pod", map[string]string{"app": "stateful-app"})

	// Create a headless Service.
	headless := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "headless-svc", Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"app": "stateful-app"},
			Ports:     []corev1.ServicePort{{Port: 80}},
		},
	}
	if err := k8sClient.Create(context.Background(), headless); err != nil {
		t.Fatalf("create headless Service: %v", err)
	}

	makeServicePodSelectorGuard(t, "svc-selector-guard-headless")

	fresh := &corev1.Service{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: headless.Name}, fresh); err != nil {
		t.Fatalf("re-fetch headless Service: %v", err)
	}
	fresh.Spec.Selector = map[string]string{"app": "no-such-app"}
	if err := k8sClient.Update(context.Background(), fresh); err != nil {
		t.Fatalf("expected headless Service UPDATE to be allowed (headless services are exempt): %v", err)
	}
}

// --- ClusterChangeValidator e2e tests ---

// TestClusterChangeValidator_DeniesMatchingOperation verifies that a
// ClusterChangeValidator guard denies admission requests whose operation and
// resource match its configuration, regardless of namespace.
func TestClusterChangeValidator_DeniesMatchingOperation(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	makeClusterChangeGuard(t, &ewv1alpha1.ClusterChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-deny-cm-delete"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "no-delete",
				Type:    ewv1alpha1.RuleTypeExpressionCheck,
				Message: "cluster policy: configmap deletion is not allowed",
				ExpressionCheck: &ewv1alpha1.ExpressionCheck{
					Expression: "operation == 'DELETE'",
				},
			}},
		},
	})

	cm := makeConfigMap(t, "cluster-protected-cm")
	err := k8sClient.Delete(context.Background(), cm)
	if err == nil {
		t.Fatal("expected DELETE to be denied by the ClusterChangeValidator guard")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestClusterChangeValidator_AllowsNonTargetedOperation verifies that a
// ClusterChangeValidator guard does not block operations it was not configured for.
func TestClusterChangeValidator_AllowsNonTargetedOperation(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	// Guard only covers UPDATE; CREATE must be allowed.
	makeClusterChangeGuard(t, &ewv1alpha1.ClusterChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-deny-cm-update-only"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject:    ewv1alpha1.SubjectResource{APIGroup: "", Resource: "configmaps"},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationUpdate},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "no-update",
				Type:    ewv1alpha1.RuleTypeExpressionCheck,
				Message: "cluster policy: configmap updates are not allowed",
				ExpressionCheck: &ewv1alpha1.ExpressionCheck{
					Expression: "operation == 'UPDATE'",
				},
			}},
		},
	})

	// CREATE is not in the guard's Operations list; it must succeed.
	if makeConfigMap(t, "cluster-create-allowed-cm") == nil {
		t.Fatal("expected ConfigMap CREATE to succeed (cluster guard only covers UPDATE)")
	}
}

// TestClusterChangeValidator_NamespaceSelector_DeniesWhenMatching verifies that
// a ClusterChangeValidator with a NamespaceSelector denies requests in namespaces
// whose labels match the selector.
func TestClusterChangeValidator_NamespaceSelector_DeniesWhenMatching(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	const labeledNS = "ew-e2e-prod-ns"
	createLabeledNamespace(t, labeledNS, map[string]string{"env": "prod"})

	makeClusterChangeGuard(t, &ewv1alpha1.ClusterChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-deny-prod-cm-delete"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "configmaps",
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "no-delete-in-prod",
				Type:    ewv1alpha1.RuleTypeExpressionCheck,
				Message: "cluster policy: configmap deletion is not allowed in prod namespaces",
				ExpressionCheck: &ewv1alpha1.ExpressionCheck{
					Expression: "operation == 'DELETE'",
				},
			}},
		},
	})

	cm := makeConfigMapInNamespace(t, "prod-cm", labeledNS)
	err := k8sClient.Delete(context.Background(), cm)
	if err == nil {
		t.Fatal("expected DELETE to be denied: namespace matches NamespaceSelector")
	}
	if !kerrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden error, got: %v", err)
	}
}

// TestClusterChangeValidator_NamespaceSelector_AllowsWhenNotMatching verifies
// that a ClusterChangeValidator with a NamespaceSelector does not block requests
// in namespaces whose labels do not match the selector.
func TestClusterChangeValidator_NamespaceSelector_AllowsWhenNotMatching(t *testing.T) {
	cleanupNamespace(t)
	t.Cleanup(func() { cleanupNamespace(t) })

	// testNamespace carries no labels; the guard targets "env=prod" only.
	makeClusterChangeGuard(t, &ewv1alpha1.ClusterChangeValidator{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-deny-prod-only"},
		Spec: ewv1alpha1.ChangeValidatorSpec{
			Subject: ewv1alpha1.SubjectResource{
				APIGroup: "",
				Resource: "configmaps",
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			Operations: []ewv1alpha1.OperationType{ewv1alpha1.OperationDelete},
			Rules: []ewv1alpha1.GuardRule{{
				Name:    "no-delete-in-prod",
				Type:    ewv1alpha1.RuleTypeExpressionCheck,
				Message: "cluster policy: configmap deletion is not allowed in prod namespaces",
				ExpressionCheck: &ewv1alpha1.ExpressionCheck{
					Expression: "operation == 'DELETE'",
				},
			}},
		},
	})

	// testNamespace does not have "env=prod"; the guard must not apply.
	cm := makeConfigMap(t, "non-prod-cm")
	if err := k8sClient.Delete(context.Background(), cm); err != nil {
		t.Fatalf("expected DELETE to be allowed: namespace does not match NamespaceSelector: %v", err)
	}
}

// TestZZZInstallUninstall is intentionally named to sort late in `go test`
// execution (go test orders by name alphabetically) so the
// ValidatingWebhookConfiguration created by install does not slow down other
// tests. In envtest the Service/Deployment won't be reachable/scheduled, so
// admission requests would have to wait for the 10-second timeout before
// being ignored by the failurePolicy.
//
// The test verifies the full install → assert → uninstall → assert cycle.
func TestZZZInstallUninstall(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)
	ctx := context.Background()

	// --- install ---
	if err := ewinstall.Run(ewinstall.Options{Kubeconfig: kubeconfig}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verify the ClusterRole was created with the managed-by annotation.
	role := &rbacv1.ClusterRole{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "early-watch-role"}, role); err != nil {
		t.Fatalf("expected ClusterRole early-watch-role to exist after install: %v", err)
	}
	if got := role.Annotations[ewinstall.CreatedByAnnotation]; got != "watchctl" {
		t.Errorf("expected annotation %s=watchctl on ClusterRole, got %q", ewinstall.CreatedByAnnotation, got)
	}

	// Verify the Namespace was created.
	ns := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "early-watch-system"}, ns); err != nil {
		t.Fatalf("expected Namespace early-watch-system to exist after install: %v", err)
	}

	// --- uninstall ---
	if err := ewinstall.Uninstall(ewinstall.UninstallOptions{Kubeconfig: kubeconfig}); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}

	// Poll until the ClusterRole is gone (delete is asynchronous / may have
	// a finalizer-driven delay).
	deadline := time.Now().Add(10 * time.Second)
	var roleGone bool
	for time.Now().Before(deadline) {
		role = &rbacv1.ClusterRole{}
		if kerrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "early-watch-role"}, role)) {
			roleGone = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !roleGone {
		t.Fatalf("expected ClusterRole early-watch-role to be absent after uninstall")
	}

	// Poll until the ClusterRoleBinding is gone.
	var crbGone bool
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		crb := &rbacv1.ClusterRoleBinding{}
		if kerrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "early-watch-rolebinding"}, crb)) {
			crbGone = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !crbGone {
		t.Fatalf("expected ClusterRoleBinding early-watch-rolebinding to be absent after uninstall")
	}
}
