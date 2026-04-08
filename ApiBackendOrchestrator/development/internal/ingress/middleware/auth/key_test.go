package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPublicKey_RSA(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	path := filepath.Join(t.TempDir(), "rsa_pub.pem")
	if err := os.WriteFile(path, pemData, 0644); err != nil {
		t.Fatalf("write PEM: %v", err)
	}

	pub, err := LoadPublicKey(path)
	if err != nil {
		t.Fatalf("LoadPublicKey: %v", err)
	}

	if _, ok := pub.(*rsa.PublicKey); !ok {
		t.Errorf("expected *rsa.PublicKey, got %T", pub)
	}
}

func TestLoadPublicKey_ECDSA(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	path := filepath.Join(t.TempDir(), "ec_pub.pem")
	if err := os.WriteFile(path, pemData, 0644); err != nil {
		t.Fatalf("write PEM: %v", err)
	}

	pub, err := LoadPublicKey(path)
	if err != nil {
		t.Fatalf("LoadPublicKey: %v", err)
	}

	if _, ok := pub.(*ecdsa.PublicKey); !ok {
		t.Errorf("expected *ecdsa.PublicKey, got %T", pub)
	}
}

func TestLoadPublicKey_PKCS1RSA(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	pubBytes := x509.MarshalPKCS1PublicKey(&privKey.PublicKey)

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubBytes,
	})

	path := filepath.Join(t.TempDir(), "rsa_pkcs1_pub.pem")
	if err := os.WriteFile(path, pemData, 0644); err != nil {
		t.Fatalf("write PEM: %v", err)
	}

	pub, err := LoadPublicKey(path)
	if err != nil {
		t.Fatalf("LoadPublicKey: %v", err)
	}

	if _, ok := pub.(*rsa.PublicKey); !ok {
		t.Errorf("expected *rsa.PublicKey, got %T", pub)
	}
}

func TestLoadPublicKey_FileNotFound(t *testing.T) {
	_, err := LoadPublicKey("/nonexistent/path/key.pem")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadPublicKey_InvalidPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(path, []byte("not a pem file"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadPublicKey(path)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestLoadPublicKey_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadPublicKey(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestSigningMethodForKey_RSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	method, err := signingMethodForKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("signingMethodForKey: %v", err)
	}
	if method != "RS256" {
		t.Errorf("method = %q, want RS256", method)
	}
}

func TestSigningMethodForKey_ECDSA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	method, err := signingMethodForKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("signingMethodForKey: %v", err)
	}
	if method != "ES256" {
		t.Errorf("method = %q, want ES256", method)
	}
}

func TestSigningMethodForKey_UnsupportedType(t *testing.T) {
	// Pass a string — not a valid key type.
	_, err := signingMethodForKey("not-a-key")
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

func TestSigningMethodForKey_WrongECDSACurve(t *testing.T) {
	// ES256 requires P-256; P-384 should be rejected.
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	_, err = signingMethodForKey(&key.PublicKey)
	if err == nil {
		t.Fatal("expected error for P-384 curve (ES256 requires P-256)")
	}
}
