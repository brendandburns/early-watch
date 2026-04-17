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
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// webhookSecretName is the name of the Secret that holds the webhook TLS
// certificate and private key.
const webhookSecretName = "early-watch-webhook-server-cert"

// webhookServiceName is the name of the Service that fronts the webhook pod.
const webhookServiceName = "early-watch-webhook-service"

// webhookConfigName is the name of the ValidatingWebhookConfiguration resource.
const webhookConfigName = "early-watch-validating-webhook"

// certValidity is the duration for which the self-signed CA and serving
// certificates are valid.
const certValidity = 10 * 365 * 24 * time.Hour // 10 years

// provisionWebhookCert generates a self-signed CA and a TLS serving
// certificate for the admission webhook, stores the cert and key in a Secret,
// and patches the ValidatingWebhookConfiguration with the CA certificate so
// that the API server can verify the webhook server's certificate.
//
// The caller must hold RBAC permissions to create/update Secrets and patch
// ValidatingWebhookConfigurations.
func provisionWebhookCert(ctx context.Context, cfg *rest.Config, namespace string) error {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}
	return provisionWebhookCertWithClient(ctx, clientset, namespace)
}

// provisionWebhookCertWithClient is the testable inner implementation of
// provisionWebhookCert that accepts an explicit kubernetes.Interface.
func provisionWebhookCertWithClient(ctx context.Context, clientset kubernetes.Interface, namespace string) error {
	dnsNames := webhookDNSNames(webhookServiceName, namespace)

	caCertPEM, certPEM, keyPEM, err := generateSelfSignedCert(dnsNames)
	if err != nil {
		return fmt.Errorf("generating self-signed webhook TLS certificate: %w", err)
	}

	// Store the TLS cert and key in the webhook Secret.
	if err := upsertWebhookSecret(ctx, clientset, namespace, certPEM, keyPEM); err != nil {
		return err
	}

	// Patch the ValidatingWebhookConfiguration with the self-signed CA cert.
	if err := patchWebhookCABundle(ctx, clientset, caCertPEM); err != nil {
		return err
	}

	return nil
}

// generateSelfSignedCert creates a self-signed CA and uses it to issue a TLS
// serving certificate covering the given DNS names.  It returns the PEM-encoded
// CA certificate, serving certificate, and private key respectively.
func generateSelfSignedCert(dnsNames []string) (caCertPEM, certPEM, keyPEM []byte, err error) {
	// Generate CA key and self-signed CA certificate.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating CA key: %w", err)
	}

	caSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating CA serial: %w", err)
	}

	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber: caSerial,
		Subject: pkix.Name{
			CommonName:   "earlywatch-webhook-ca",
			Organization: []string{"earlywatch.io"},
		},
		// NotBefore is set one minute in the past to tolerate minor clock
		// skew between the machine running the installer and the cluster
		// nodes that validate the certificate chain.
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating CA certificate: %w", err)
	}
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	// Generate serving key and certificate signed by the CA.
	servingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating serving key: %w", err)
	}

	servingSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating serving cert serial: %w", err)
	}

	servingTemplate := &x509.Certificate{
		SerialNumber: servingSerial,
		Subject: pkix.Name{
			CommonName:   dnsNames[0],
			Organization: []string{"earlywatch.io"},
		},
		DNSNames: dnsNames,
		// NotBefore is set one minute in the past to tolerate minor clock
		// skew between the machine running the installer and the cluster
		// nodes that validate the certificate chain.
		NotBefore: now.Add(-time.Minute),
		NotAfter:  now.Add(certValidity),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, servingTemplate, caCert, &servingKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating serving certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalPKCS8PrivateKey(servingKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshaling serving key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return caCertPEM, certPEM, keyPEM, nil
}

// webhookDNSNames returns the DNS SANs that the webhook's TLS certificate
// must cover so the API server can reach it via the Service.
func webhookDNSNames(service, namespace string) []string {
	return []string{
		fmt.Sprintf("%s.%s.svc", service, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", service, namespace),
	}
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
