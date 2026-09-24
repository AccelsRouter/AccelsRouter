package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Organization keys carry the same limits as personal keys, but their model
// limits must stay inside the org's callable catalog and untouched fields
// survive a partial update.
func TestApplyOrgKeyRequest(t *testing.T) {
	callable := []string{"claude-opus-4-8", "deepseek-v4-pro"}
	ptrS := func(s string) *string { return &s }
	ptrB := func(b bool) *bool { return &b }
	ptrI := func(i int) *int { return &i }
	ptrI64 := func(i int64) *int64 { return &i }

	t.Run("create defaults stay unlimited and unrestricted", func(t *testing.T) {
		tok := &model.Token{Name: "API Key", ExpiredTime: -1, UnlimitedQuota: true, Status: common.TokenStatusEnabled}
		require.NoError(t, applyOrgKeyRequest(tok, orgKeyRequest{}, callable))
		assert.True(t, tok.UnlimitedQuota)
		assert.Equal(t, int64(-1), tok.ExpiredTime)
		assert.False(t, tok.ModelLimitsEnabled)
		assert.Equal(t, "API Key", tok.Name)
	})

	t.Run("limits are applied", func(t *testing.T) {
		tok := &model.Token{ExpiredTime: -1, UnlimitedQuota: true, Status: common.TokenStatusEnabled}
		req := orgKeyRequest{
			Name: ptrS("  prod  "), UnlimitedQuota: ptrB(false), RemainQuota: ptrI(500000),
			ExpiredTime: ptrI64(1_900_000_000), ModelLimitsEnabled: ptrB(true),
			ModelLimits: []string{"deepseek-v4-pro", " claude-opus-4-8 ", "deepseek-v4-pro"},
		}
		require.NoError(t, applyOrgKeyRequest(tok, req, callable))
		assert.Equal(t, "prod", tok.Name)
		assert.False(t, tok.UnlimitedQuota)
		assert.Equal(t, 500000, tok.RemainQuota)
		assert.Equal(t, int64(1_900_000_000), tok.ExpiredTime)
		assert.True(t, tok.ModelLimitsEnabled)
		assert.Equal(t, "deepseek-v4-pro,claude-opus-4-8", tok.ModelLimits, "trimmed and de-duplicated")
	})

	t.Run("model outside the org catalog is rejected", func(t *testing.T) {
		tok := &model.Token{UnlimitedQuota: true, Status: common.TokenStatusEnabled}
		err := applyOrgKeyRequest(tok, orgKeyRequest{ModelLimitsEnabled: ptrB(true), ModelLimits: []string{"gpt-4o"}}, callable)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gpt-4o")
	})

	t.Run("enabled limits need at least one model", func(t *testing.T) {
		tok := &model.Token{UnlimitedQuota: true, Status: common.TokenStatusEnabled}
		require.Error(t, applyOrgKeyRequest(tok, orgKeyRequest{ModelLimitsEnabled: ptrB(true), ModelLimits: []string{}}, callable))
	})

	t.Run("quota bounds", func(t *testing.T) {
		tok := &model.Token{UnlimitedQuota: true, Status: common.TokenStatusEnabled}
		require.Error(t, applyOrgKeyRequest(tok, orgKeyRequest{UnlimitedQuota: ptrB(false), RemainQuota: ptrI(-1)}, callable))
		tok = &model.Token{UnlimitedQuota: true, Status: common.TokenStatusEnabled}
		require.NoError(t, applyOrgKeyRequest(tok, orgKeyRequest{UnlimitedQuota: ptrB(false), RemainQuota: ptrI(0)}, callable), "zero = exhausted but valid")
	})

	t.Run("partial update keeps other fields; only enabled/disabled status", func(t *testing.T) {
		tok := &model.Token{Name: "keep", UnlimitedQuota: false, RemainQuota: 42, ExpiredTime: 123, ModelLimitsEnabled: true, ModelLimits: "deepseek-v4-pro", Status: common.TokenStatusEnabled}
		require.NoError(t, applyOrgKeyRequest(tok, orgKeyRequest{Status: ptrI(common.TokenStatusDisabled)}, callable))
		assert.Equal(t, common.TokenStatusDisabled, tok.Status)
		assert.Equal(t, "keep", tok.Name)
		assert.Equal(t, 42, tok.RemainQuota)
		assert.Equal(t, int64(123), tok.ExpiredTime)
		assert.Equal(t, "deepseek-v4-pro", tok.ModelLimits)
		require.Error(t, applyOrgKeyRequest(tok, orgKeyRequest{Status: ptrI(common.TokenStatusExpired)}, callable))
	})
}
