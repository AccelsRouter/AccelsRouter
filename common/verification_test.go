package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A verification code is matched case-insensitively and ignoring surrounding
// whitespace: the generated code is lowercase hex, and a person retyping it
// from an email may use capitals or paste a trailing space.
func TestVerifyCodeWithKeyNormalizesInput(t *testing.T) {
	RegisterVerificationCodeWithKey("a@b.c", "a3f2c1", EmailVerificationPurpose)
	t.Cleanup(func() { DeleteKey("a@b.c", EmailVerificationPurpose) })

	assert.True(t, VerifyCodeWithKey("a@b.c", "a3f2c1", EmailVerificationPurpose))
	assert.True(t, VerifyCodeWithKey("a@b.c", "A3F2C1", EmailVerificationPurpose))
	assert.True(t, VerifyCodeWithKey("a@b.c", " a3f2c1\n", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("a@b.c", "a3f2c2", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("a@b.c", "a3f2c1", PasswordResetPurpose), "purpose is part of the key")
}
