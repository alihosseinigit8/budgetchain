package types

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrInvalidPublicKey = errors.New("invalid ECDSA public key")
	ErrInvalidSignature = errors.New("invalid signature")
)

// NewPrivateKey is used only to initialize sample identities or clients. The
// primary node never creates or retains a key on an organization's behalf.
func NewPrivateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// EncodePublicKey produces a storable identity-registry representation of a
// public key; the private key is not included in this conversion.
func EncodePublicKey(key *ecdsa.PublicKey) (string, error) {
	if key == nil {
		return "", ErrInvalidPublicKey
	}
	b, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// DecodePublicKey runs before identity registration and signature verification
// so only ECDSA public keys on the expected P-256 curve are accepted.
func DecodePublicKey(encoded string) (*ecdsa.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: base64", ErrInvalidPublicKey)
	}
	key, err := x509.ParsePKIXPublicKey(b)
	if err != nil {
		return nil, fmt.Errorf("%w: parse", ErrInvalidPublicKey)
	}
	ecdsaKey, ok := key.(*ecdsa.PublicKey)
	if !ok || ecdsaKey.Curve != elliptic.P256() {
		return nil, ErrInvalidPublicKey
	}
	return ecdsaKey, nil
}

// Sign models the client-side organizational action: it hashes the payload with
// SHA-256 and signs it with the private key, which is never sent to the Node.
func Sign(privateKey *ecdsa.PrivateKey, payload []byte) (string, error) {
	if privateKey == nil {
		return "", errors.New("private key is required")
	}
	digest := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifySignature models signature verification by authorized nodes. The public
// key is read from the registry rather than supplied by the requester message.
func VerifySignature(encodedPublicKey string, payload []byte, encodedSignature string) error {
	publicKey, err := DecodePublicKey(encodedPublicKey)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil {
		return ErrInvalidSignature
	}
	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(publicKey, digest[:], sig) {
		return ErrInvalidSignature
	}
	return nil
}

// DigestHex is used for cryptographic commitments and to bind approvals to a
// request. By itself, it does not make guessable data safe for public release.
func DigestHex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// NewOpaqueToken creates a random value with sufficient entropy. It is used as a
// pseudonymous reference and private nonce so public commitments cannot be linked
// to private data by trial and error.
func NewOpaqueToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(raw), nil
}
