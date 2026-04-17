package install

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// csrName is the name given to the CertificateSigningRequest resource
// created during installation.
const csrName = "early-watch-webhook-serving"

// webhookSecretName is the name of the Secret that holds the webhook TLS
// certificate and private key.
const webhookSecretName = "early-watch-webhook-server-cert"

// webhookServiceName is the name of the Service that fronts the webhook pod.
const webhookServiceName = "early-watch-webhook-service"

// webhookConfigName is the name of the ValidatingWebhookConfiguration resource.
const webhookConfigName = "early-watch-validating-webhook"

// certSigningTimeout is the maximum time to wait for the API server to issue
// the signed certificate after the CSR has been approved.
const certSigningTimeout = 2 * time.Minute

// provisionWebhookCert generates a TLS key pair for the admission webhook
// server using the Kubernetes built-in CertificateSigningRequest API, stores
// the resulting cert and key in a Secret, and patches the
// ValidatingWebhookConfiguration with the cluster CA bundle so that the API
// server can verify the webhook server's certificate.
//
// The caller must hold RBAC permissions to create and approve
// CertificateSigningRequests as well as to create/update Secrets and patch
// ValidatingWebhookConfigurations.
func provisionWebhookCert(ctx context.Context, cfg *rest.Config, namespace string) error {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}
	return provisionWebhookCertWithClient(ctx, clientset, cfg, namespace)
}

// provisionWebhookCertWithClient is the testable inner implementation of
// provisionWebhookCert that accepts an explicit kubernetes.Interface.
func provisionWebhookCertWithClient(ctx context.Context, clientset kubernetes.Interface, cfg *rest.Config, namespace string) error {
	// Generate a new ECDSA private key.
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating private key: %w", err)
	}

	// Build the x509 certificate signing request with the webhook's DNS names.
	dnsNames := webhookDNSNames(webhookServiceName, namespace)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   dnsNames[0],
			Organization: []string{"earlywatch.io"},
		},
		DNSNames: dnsNames,
	}, privateKey)
	if err != nil {
		return fmt.Errorf("creating certificate request: %w", err)
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Remove any stale CSR from a previous install attempt.
	if err := deleteCSRIfExists(ctx, clientset); err != nil {
		return err
	}

	// Submit the CSR to the Kubernetes API.
	kCSR := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrName,
			Annotations: map[string]string{
				CreatedByAnnotation: managedByValue,
			},
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Request: csrPEM,
			// kubernetes.io/legacy-unknown is signed by the cluster's default
			// CA and supports arbitrary usages, making it suitable for TLS
			// server certificates used by admission webhooks.
			SignerName: "kubernetes.io/legacy-unknown",
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageDigitalSignature,
				certificatesv1.UsageKeyEncipherment,
				certificatesv1.UsageServerAuth,
			},
		},
	}

	created, err := clientset.CertificatesV1().CertificateSigningRequests().Create(ctx, kCSR, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating CertificateSigningRequest %q: %w", csrName, err)
	}
	fmt.Printf("Created CertificateSigningRequest %q\n", csrName)

	// Approve the CSR so that the controller-manager will sign it.
	created.Status.Conditions = append(created.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Status:         corev1.ConditionTrue,
		Reason:         "WatchctlInstall",
		Message:        "Approved by watchctl install for admission webhook TLS",
		LastUpdateTime: metav1.Now(),
	})

	if _, err := clientset.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csrName, created, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("approving CertificateSigningRequest %q: %w", csrName, err)
	}
	fmt.Printf("Approved CertificateSigningRequest %q\n", csrName)

	// Wait for the controller-manager to populate the signed certificate.
	signedCertPEM, err := waitForCertificate(ctx, clientset)
	if err != nil {
		return err
	}

	// Marshal the private key to PKCS8 DER, then PEM-encode it.
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("marshaling private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Retrieve the cluster CA bundle so the ValidatingWebhookConfiguration
	// can be patched to trust the signed certificate.
	caBundle, err := clusterCABundle(ctx, clientset, cfg)
	if err != nil {
		return err
	}

	// Store the TLS cert and key in the webhook Secret.
	if err := upsertWebhookSecret(ctx, clientset, namespace, signedCertPEM, keyPEM); err != nil {
		return err
	}

	// Patch the ValidatingWebhookConfiguration with the cluster CA bundle.
	if err := patchWebhookCABundle(ctx, clientset, caBundle); err != nil {
		return err
	}

	return nil
}

// webhookDNSNames returns the DNS SANs that the webhook's TLS certificate
// must cover so the API server can reach it via the Service.
func webhookDNSNames(service, namespace string) []string {
	return []string{
		fmt.Sprintf("%s.%s.svc", service, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", service, namespace),
	}
}

// deleteCSRIfExists removes any pre-existing CertificateSigningRequest with
// csrName so that a fresh installation can reuse the same name.
func deleteCSRIfExists(ctx context.Context, clientset kubernetes.Interface) error {
	err := clientset.CertificatesV1().CertificateSigningRequests().Delete(ctx, csrName, metav1.DeleteOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return fmt.Errorf("deleting stale CertificateSigningRequest %q: %w", csrName, err)
	}
	return nil
}

// waitForCertificate polls the CertificateSigningRequest until its Certificate
// field is populated by the controller-manager, then returns the PEM-encoded
// signed certificate.
func waitForCertificate(ctx context.Context, clientset kubernetes.Interface) ([]byte, error) {
	var certPEM []byte
	pollCtx, cancel := context.WithTimeout(ctx, certSigningTimeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(pollCtx, 2*time.Second, certSigningTimeout, true, func(ctx context.Context) (bool, error) {
		csr, err := clientset.CertificatesV1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("getting CertificateSigningRequest %q: %w", csrName, err)
		}
		if len(csr.Status.Certificate) == 0 {
			return false, nil
		}
		certPEM = csr.Status.Certificate
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for certificate from CertificateSigningRequest %q: %w", csrName, err)
	}
	return certPEM, nil
}

// clusterCABundle returns the PEM-encoded cluster CA certificate. It first
// tries the REST config's TLS CA data, then falls back to the kube-root-ca.crt
// ConfigMap that the Kubernetes control plane projects into every namespace.
func clusterCABundle(ctx context.Context, clientset kubernetes.Interface, cfg *rest.Config) ([]byte, error) {
	if len(cfg.CAData) > 0 {
		return cfg.CAData, nil
	}
	// Fall back to the well-known projected ConfigMap.
	cm, err := clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "kube-root-ca.crt", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("fetching cluster CA from kube-root-ca.crt: %w", err)
	}
	ca, ok := cm.Data["ca.crt"]
	if !ok || ca == "" {
		return nil, fmt.Errorf("kube-root-ca.crt ConfigMap does not contain ca.crt key")
	}
	return []byte(ca), nil
}

// upsertWebhookSecret creates or updates the Secret that the webhook Deployment
// mounts for its TLS certificate and private key.
func upsertWebhookSecret(ctx context.Context, clientset kubernetes.Interface, namespace string, certPEM, keyPEM []byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookSecretName,
			Namespace: namespace,
			Annotations: map[string]string{
				CreatedByAnnotation: managedByValue,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}

	_, err := clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating webhook TLS secret: %w", err)
		}
		// Secret already exists — update it.
		existing, getErr := clientset.CoreV1().Secrets(namespace).Get(ctx, webhookSecretName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("fetching existing webhook TLS secret: %w", getErr)
		}
		existing.Data = secret.Data
		if _, updateErr := clientset.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{}); updateErr != nil {
			return fmt.Errorf("updating webhook TLS secret: %w", updateErr)
		}
		fmt.Printf("Updated Secret %q\n", namespace+"/"+webhookSecretName)
		return nil
	}
	fmt.Printf("Created Secret %q\n", namespace+"/"+webhookSecretName)
	return nil
}

// patchWebhookCABundle sets the caBundle field on every webhook entry in the
// ValidatingWebhookConfiguration so the API server trusts the newly-issued
// certificate.
func patchWebhookCABundle(ctx context.Context, clientset kubernetes.Interface, caBundle []byte) error {
	cfg, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting ValidatingWebhookConfiguration %q: %w", webhookConfigName, err)
	}

	for i := range cfg.Webhooks {
		cfg.Webhooks[i].ClientConfig.CABundle = caBundle
	}

	if _, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(ctx, cfg, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating ValidatingWebhookConfiguration %q caBundle: %w", webhookConfigName, err)
	}
	fmt.Printf("Updated ValidatingWebhookConfiguration %q caBundle\n", webhookConfigName)
	return nil
}
