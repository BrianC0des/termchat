package system

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Identity represents the device's persistent cryptographic Ed25519 identity
type Identity struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// GetOrCreateIdentity loads or creates a persistent Ed25519 keypair for the device
func GetOrCreateIdentity() (*Identity, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".config", "termchat")
	_ = os.MkdirAll(dir, 0700)
	keyPath := filepath.Join(dir, "identity.key")

	if data, err := os.ReadFile(keyPath); err == nil && len(data) == ed25519.PrivateKeySize {
		priv := ed25519.PrivateKey(data)
		pub := priv.Public().(ed25519.PublicKey)
		return &Identity{
			PrivateKey: priv,
			PublicKey:  pub,
		}, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	_ = os.WriteFile(keyPath, priv, 0600)
	return &Identity{
		PrivateKey: priv,
		PublicKey:  pub,
	}, nil
}

// Fingerprint returns an 8-character hex ID of the public key
func (id *Identity) Fingerprint() string {
	if len(id.PublicKey) == 0 {
		return "unknown"
	}
	hash := sha256.Sum256(id.PublicKey)
	return hex.EncodeToString(hash[:4])
}

// FullPublicKeyHex returns full public key in hex
func (id *Identity) FullPublicKeyHex() string {
	return hex.EncodeToString(id.PublicKey)
}

// Sign returns hex signature of message
func (id *Identity) Sign(msg []byte) string {
	sig := ed25519.Sign(id.PrivateKey, msg)
	return hex.EncodeToString(sig)
}

// VerifySignature checks an ed25519 signature
func VerifySignature(pubKeyHex, sigHex string, msg []byte) bool {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return false
	}
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pubBytes, msg, sigBytes)
}
