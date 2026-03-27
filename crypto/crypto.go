// Package crypto provides Ed25519 key generation, transaction signing, and verification.
// Ed25519 is chosen for its speed, small key/signature size, and resistance to side-channel attacks.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// KeyPair holds a public/private Ed25519 key pair.
// The Address is the hex-encoded public key used to identify wallets.
type KeyPair struct {
	PrivateKey ed25519.PrivateKey `json:"-"`          // never serialized
	PublicKey  ed25519.PublicKey  `json:"public_key"`  // 32-byte public key
	Address    string             `json:"address"`     // hex-encoded public key
}

// GenerateKeyPair creates a new Ed25519 key pair.
// Returns an error if the system random source fails.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("key generation failed: %w", err)
	}
	return &KeyPair{
		PrivateKey: priv,
		PublicKey:  pub,
		Address:    hex.EncodeToString(pub),
	}, nil
}

// Sign produces an Ed25519 signature over the given message bytes.
// The signature is returned as a hex-encoded string.
func Sign(privateKey ed25519.PrivateKey, message []byte) string {
	sig := ed25519.Sign(privateKey, message)
	return hex.EncodeToString(sig)
}

// Verify checks an Ed25519 signature against a message and hex-encoded public key.
// Returns true only when the signature is valid.
func Verify(pubKeyHex string, message []byte, signatureHex string) bool {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return false
	}
	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pubBytes), message, sigBytes)
}
