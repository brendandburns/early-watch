package install

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// webhookCerts holds the TLS material generated for the EarlyWatch webhook server.
type webhookCerts struct {
	// caCert is the PEM-encoded CA certificate.  It is set as the caBundle on
	// the ValidatingWebhookConfiguration so the API server can verify the
	// webhook server's TLS certificate.
	caCert []byte
	// tlsCert is the PEM-encoded server TLS certificate signed by the CA.
	tlsCert []byte
	// tlsKey is the PEM-encoded ECDSA private key for the server TLS certificate.
	tlsKey []byte
}

// webhookDNSNames returns the DNS SAN entries required for the webhook Service.
func webhookDNSNames() []string {
	return []string{
		webhookServiceName + "." + systemNamespace + ".svc",
		webhookServiceName + "." + systemNamespace + ".svc.cluster.local",
	}
}

// generateWebhookCerts generates a self-signed CA and a server TLS certificate
// signed by that CA.  The certificates are valid for 10 years from the time of
// generation and include the appropriate DNS SANs for the webhook Service.
func generateWebhookCerts() (*webhookCerts, error) {
	// Generate CA private key (ECDSA P-256).
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA key: %w", err)
	}

	caSerial, err := randomSerial()
	if err != nil {
		return nil, fmt.Errorf("generating CA serial: %w", err)
	}

	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "earlywatch-webhook-ca"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	// Generate server private key (ECDSA P-256).
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating server key: %w", err)
	}

	serverSerial, err := randomSerial()
	if err != nil {
		return nil, fmt.Errorf("generating server serial: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject:      pkix.Name{CommonName: webhookServiceName + "." + systemNamespace + ".svc"},
		DNSNames:     webhookDNSNames(),
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating server certificate: %w", err)
	}

	// Encode server key to PEM (PKCS#8 for broad compatibility).
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("marshalling server key: %w", err)
	}

	return &webhookCerts{
		caCert:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		tlsCert: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		tlsKey:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER}),
	}, nil
}

// randomSerial generates a 128-bit random certificate serial number.
func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}
