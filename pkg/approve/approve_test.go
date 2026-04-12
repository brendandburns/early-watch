package approve

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

// generateTestKey generates a 2048-bit RSA key pair for testing and returns
// both the private key and its PEM-encoded bytes.
func generateTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(privKey)
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: der,
	})
	return privKey, pemBytes
}

// generateTestKeyPKCS8 generates a 2048-bit RSA key pair encoded in PKCS#8 PEM format.
func generateTestKeyPKCS8(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		t.Fatalf("marshalling PKCS#8 key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
	return privKey, pemBytes
}

// --- ParseRSAPrivateKey tests ---

func TestParseRSAPrivateKey_PKCS1(t *testing.T) {
	_, pemBytes := generateTestKey(t)
	key, err := ParseRSAPrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParseRSAPrivateKey_PKCS8(t *testing.T) {
	_, pemBytes := generateTestKeyPKCS8(t)
	key, err := ParseRSAPrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParseRSAPrivateKey_NoPEMBlock(t *testing.T) {
	_, err := ParseRSAPrivateKey([]byte("not a pem block"))
	if err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func TestParseRSAPrivateKey_UnsupportedType(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: []byte("dummy"),
	})
	_, err := ParseRSAPrivateKey(pemBytes)
	if err == nil {
		t.Fatal("expected error for unsupported PEM type")
	}
}

func TestParseRSAPrivateKey_CorruptedPKCS1(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: []byte("not valid DER"),
	})
	_, err := ParseRSAPrivateKey(pemBytes)
	if err == nil {
		t.Fatal("expected error for corrupted PKCS#1 key")
	}
}

func TestParseRSAPrivateKey_CorruptedPKCS8(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: []byte("not valid DER"),
	})
	_, err := ParseRSAPrivateKey(pemBytes)
	if err == nil {
		t.Fatal("expected error for corrupted PKCS#8 key")
	}
}

func TestParseRSAPrivateKey_EmptyPEM(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: []byte{},
	})
	_, err := ParseRSAPrivateKey(pemBytes)
	if err == nil {
		t.Fatal("expected error for empty PKCS#8 key bytes")
	}
}

// --- LoadRSAPrivateKey tests ---

func TestLoadRSAPrivateKey_ValidFile(t *testing.T) {
	_, pemBytes := generateTestKey(t)

	tmpFile := t.TempDir() + "/key.pem"
	if err := writeFile(tmpFile, pemBytes); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	key, err := LoadRSAPrivateKey(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestLoadRSAPrivateKey_FileNotFound(t *testing.T) {
	_, err := LoadRSAPrivateKey("/nonexistent/path/key.pem")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadRSAPrivateKey_InvalidPEM(t *testing.T) {
	tmpFile := t.TempDir() + "/key.pem"
	if err := writeFile(tmpFile, []byte("not pem")); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	_, err := LoadRSAPrivateKey(tmpFile)
	if err == nil {
		t.Fatal("expected error for invalid PEM content")
	}
}

// --- SignResourcePath tests ---

func TestSignResourcePath_ProducesVerifiableSignature(t *testing.T) {
	privKey, _ := generateTestKey(t)
	path := "apps/v1/namespaces/default/deployments/my-app"

	sig, err := SignResourcePath(privKey, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("expected non-empty signature")
	}

	// Verify the signature is valid.
	digest := sha256.Sum256([]byte(path))
	if err := rsa.VerifyPSS(&privKey.PublicKey, crypto.SHA256, digest[:], sig, nil); err != nil {
		t.Errorf("signature verification failed: %v", err)
	}
}

func TestSignResourcePath_DifferentPathsDifferentSigs(t *testing.T) {
	privKey, _ := generateTestKey(t)

	sig1, err := SignResourcePath(privKey, "v1/namespaces/default/configmaps/cm1")
	if err != nil {
		t.Fatalf("signing path1: %v", err)
	}
	_, err = SignResourcePath(privKey, "v1/namespaces/default/configmaps/cm2")
	if err != nil {
		t.Fatalf("signing path2: %v", err)
	}

	// RSA-PSS is probabilistic, so the signatures will always differ; but more
	// importantly sig1 must verify only against its own path.
	digest1 := sha256.Sum256([]byte("v1/namespaces/default/configmaps/cm1"))
	digest2 := sha256.Sum256([]byte("v1/namespaces/default/configmaps/cm2"))

	if err := rsa.VerifyPSS(&privKey.PublicKey, crypto.SHA256, digest1[:], sig1, nil); err != nil {
		t.Errorf("sig1 does not verify against path1: %v", err)
	}
	// sig1 must NOT verify against path2's digest.
	if err := rsa.VerifyPSS(&privKey.PublicKey, crypto.SHA256, digest2[:], sig1, nil); err == nil {
		t.Error("sig1 unexpectedly verified against path2")
	}
}

func TestSignResourcePath_PKCS8Key(t *testing.T) {
	privKey, _ := generateTestKeyPKCS8(t)
	path := "v1/namespaces/kube-system/configmaps/my-cm"

	sig, err := SignResourcePath(privKey, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	digest := sha256.Sum256([]byte(path))
	if err := rsa.VerifyPSS(&privKey.PublicKey, crypto.SHA256, digest[:], sig, nil); err != nil {
		t.Errorf("signature verification failed: %v", err)
	}
}

// writeFile is a small helper to write bytes to a file path.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
