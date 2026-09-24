package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

// BYOK (bring-your-own-key) upstream credentials are encrypted at rest with
// AES-256-GCM so a database dump/backup does not reveal them. The key is derived
// from CryptoSecret (which itself falls back to the required SESSION_SECRET), so
// encryption works out of the box without new configuration; set CRYPTO_SECRET
// to control it explicitly. Rotating that secret makes existing ciphertexts
// undecryptable — the standard key-management caveat.
//
// Format: byokEncPrefix + base64Raw(nonce || ciphertext). Values without the
// prefix are treated as plaintext (backward compatible with pre-encryption rows
// and with normal, non-BYOK channel keys, which are never encrypted).
const byokEncPrefix = "encb64:v1:"

// byokAEADKey derives a 32-byte key from CryptoSecret with domain separation, so
// the BYOK encryption key is distinct from the HMAC signing use of CryptoSecret.
func byokAEADKey() [32]byte {
	return sha256.Sum256([]byte(CryptoSecret + "|byok-key-encryption|v1"))
}

func byokGCM() (cipher.AEAD, error) {
	key := byokAEADKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// IsByokSecretEncrypted reports whether a stored value is an encrypted BYOK
// secret (has the marker prefix).
func IsByokSecretEncrypted(s string) bool {
	return strings.HasPrefix(s, byokEncPrefix)
}

// EncryptByokSecret encrypts a plaintext upstream credential for storage. An
// empty string and an already-encrypted value are returned unchanged (idempotent
// — safe to call on migration without double-encrypting).
func EncryptByokSecret(plaintext string) (string, error) {
	if plaintext == "" || IsByokSecretEncrypted(plaintext) {
		return plaintext, nil
	}
	// Without a restart-stable master secret, encrypting would make the value
	// unrecoverable after the next boot. Store as-is instead (init.go warns);
	// decryption transparently passes plaintext through.
	if !CryptoSecretPersistent {
		return plaintext, nil
	}
	gcm, err := byokGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return byokEncPrefix + base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

// DecryptByokSecret returns the plaintext credential. A value without the marker
// prefix is returned as-is (plaintext / non-BYOK), so this is safe to call on any
// channel key. A prefixed value that fails to decrypt returns an error rather
// than leaking the ciphertext upstream.
func DecryptByokSecret(stored string) (string, error) {
	if !IsByokSecretEncrypted(stored) {
		return stored, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, byokEncPrefix))
	if err != nil {
		return "", err
	}
	gcm, err := byokGCM()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("byok ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// DecryptByokSecretOrSelf returns the decrypted plaintext, or the original value
// if it is not encrypted. On decrypt failure it returns the original unchanged
// so best-effort readers (e.g. multi-key splitting) never panic; callers that
// must not send a bad key upstream should use DecryptByokSecret and check err.
func DecryptByokSecretOrSelf(stored string) string {
	if !IsByokSecretEncrypted(stored) {
		return stored
	}
	if plaintext, err := DecryptByokSecret(stored); err == nil {
		return plaintext
	}
	return stored
}
