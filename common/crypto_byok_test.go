package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BYOK secrets must survive an encrypt/decrypt round-trip, be recognizably
// encrypted at rest, leave non-BYOK plaintext untouched, and reject tampering.
func TestByokSecretRoundTrip(t *testing.T) {
	CryptoSecret = "test-crypto-secret-fixed"

	plain := "sk-proj-abcdef0123456789"
	enc, err := EncryptByokSecret(plain)
	require.NoError(t, err)
	assert.True(t, IsByokSecretEncrypted(enc), "stored value must carry the encrypted marker")
	assert.NotContains(t, enc, plain, "plaintext must not appear in the stored value")

	got, err := DecryptByokSecret(enc)
	require.NoError(t, err)
	assert.Equal(t, plain, got)

	// Non-encrypted values (plaintext / normal platform keys) pass through.
	assert.False(t, IsByokSecretEncrypted(plain))
	passthrough, err := DecryptByokSecret(plain)
	require.NoError(t, err)
	assert.Equal(t, plain, passthrough)

	// Idempotent: encrypting an already-encrypted value is a no-op.
	again, err := EncryptByokSecret(enc)
	require.NoError(t, err)
	assert.Equal(t, enc, again)

	// Empty stays empty.
	e, err := EncryptByokSecret("")
	require.NoError(t, err)
	assert.Equal(t, "", e)

	// Tampered ciphertext fails closed (does not leak the ciphertext upstream).
	_, err = DecryptByokSecret(enc + "AA")
	assert.Error(t, err)

	// A different master secret cannot decrypt.
	CryptoSecret = "a-different-secret"
	_, err = DecryptByokSecret(enc)
	assert.Error(t, err, "wrong master secret must not decrypt")
}
