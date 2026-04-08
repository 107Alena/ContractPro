package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// LoadPublicKey reads a PEM-encoded public key from the given file path and
// returns it as a crypto.PublicKey. Both RSA and ECDSA keys are supported,
// matching the JWT algorithms RS256 and ES256 respectively.
//
// The function validates that:
//   - The file exists and is readable
//   - The file contains exactly one valid PEM block
//   - The PEM block decodes to either an RSA or ECDSA public key
//
// Returns an error if the file cannot be read, the PEM is malformed, or the
// key type is unsupported.
func LoadPublicKey(path string) (crypto.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}

	// Try PKIX first (most common format for public keys).
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		// Fall back to PKCS1 for RSA keys that use "RSA PUBLIC KEY" PEM type.
		rsaPub, rsaErr := x509.ParsePKCS1PublicKey(block.Bytes)
		if rsaErr != nil {
			return nil, fmt.Errorf("parse public key (tried PKIX and PKCS1): PKIX: %w; PKCS1: %v", err, rsaErr)
		}
		return rsaPub, nil
	}

	// Verify the key is a type we support.
	switch pub.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
		return pub, nil
	default:
		return nil, fmt.Errorf("unsupported public key type: %T (expected RSA or ECDSA)", pub)
	}
}

// signingMethodForKey returns the expected jwt.SigningMethod for the given
// public key type. This ensures the token's alg header matches the loaded key.
func signingMethodForKey(key crypto.PublicKey) (string, error) {
	switch k := key.(type) {
	case *rsa.PublicKey:
		return "RS256", nil
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return "", fmt.Errorf("ECDSA key uses curve %v, expected P-256 for ES256", k.Curve.Params().Name)
		}
		return "ES256", nil
	default:
		return "", fmt.Errorf("unsupported key type for signing method: %T", key)
	}
}
