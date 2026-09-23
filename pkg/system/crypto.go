package system

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// kdfSalt is a fixed, domain-separated salt used for passphrase-based key
// derivation. It is not a substitute for a per-user random salt, but it
// ensures TermChat's derived keys cannot be looked up in generic SHA-256
// rainbow tables and are cryptographically distinct from any other use of
// PBKDF2-HMAC-SHA256 over the same passphrase.
const kdfSalt = "termchat-aes256-salt-v2"

// kdfIterations is the number of PBKDF2 rounds applied when deriving an
// AES-256 key from a user-supplied passphrase. 100,000 iterations of
// HMAC-SHA256 is in line with OWASP's current minimum recommendation and
// meaningfully slows down GPU/ASIC brute-force attacks against weak
// passphrases compared to a single unsalted SHA-256 hash.
const kdfIterations = 100000

// pbkdf2 implements RFC 8018 PBKDF2 using HMAC-SHA256 as the pseudorandom
// function. It is implemented directly against the standard library so the
// module does not need to take on an external dependency for a single
// primitive.
func pbkdf2(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var derivedKey []byte
	buf := make([]byte, 4)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf)
		u := prf.Sum(nil)

		t := make([]byte, len(u))
		copy(t, u)

		for i := 1; i < iterations; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		derivedKey = append(derivedKey, t...)
	}
	return derivedKey[:keyLen]
}

// DeriveKey derives a 32-byte AES-256 key from a passphrase using
// PBKDF2-HMAC-SHA256 with a domain-separated salt and 100,000 iterations.
// This replaces the previous single unsalted SHA-256 hash, which offered no
// resistance to GPU-accelerated brute-force attacks against weak
// passphrases.
func DeriveKey(passphrase string) []byte {
	return pbkdf2([]byte(passphrase), []byte(kdfSalt), kdfIterations, 32)
}

// Encrypt encrypts plain text using AES-GCM
func Encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts AES-GCM encrypted text
func Decrypt(cipherBase64 string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (incorrect key or corrupted payload)")
	}

	return string(plaintext), nil
}

// GenerateKeyFingerprint generates a clean 8-character verification security code (e.g. "7F2A-9C4B")
func GenerateKeyFingerprint(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	hash := sha256.Sum256(key)
	return fmt.Sprintf("%02X%02X-%02X%02X", hash[0], hash[1], hash[2], hash[3])
}
