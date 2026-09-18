package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The discount matches an exact full-model-name key first, then the longest
// matching PREFIX, defaulting to 1.0 (no discount) when nothing matches.
func TestRetailDiscountFor(t *testing.T) {
	d := map[string]float64{"deepseek": 0.6, "kimi": 0.8, "claude": 0.9}
	assert.Equal(t, 0.6, RetailDiscountFor("deepseek-chat", d), "prefix")
	assert.Equal(t, 0.6, RetailDiscountFor("DeepSeek-Reasoner", d), "case-insensitive")
	assert.Equal(t, 0.9, RetailDiscountFor("claude-opus-4", d), "prefix")
	assert.Equal(t, 0.8, RetailDiscountFor("kimi-k2", d))
	assert.Equal(t, 1.0, RetailDiscountFor("gpt-4o", d), "no matching series = no discount")
	assert.Equal(t, 1.0, RetailDiscountFor("anything", map[string]float64{}), "empty table = no discount")

	// Exact full-model-name key beats a shorter prefix key.
	exact := map[string]float64{"claude": 0.9, "claude-opus-4-8": 0.5}
	assert.Equal(t, 0.5, RetailDiscountFor("claude-opus-4-8", exact), "exact name wins")
	assert.Equal(t, 0.9, RetailDiscountFor("claude-opus-4-1", exact), "falls back to prefix")

	// A token that is only a mid-string substring (not a prefix) no longer
	// matches — matching is prefix/exact, not "contains".
	assert.Equal(t, 1.0, RetailDiscountFor("claude-opus-4", map[string]float64{"opus": 0.7}), "mid-substring is not a prefix")

	// WholesaleRatioFor shares the same precedence.
	assert.Equal(t, 0.5, WholesaleRatioFor("claude-opus-4-8", exact))
	assert.Equal(t, 1.0, WholesaleRatioFor("gpt-4o", exact))
}

// Round-trip: invalid ratios/tokens are dropped; empty map serializes to "".
func TestRetailDiscountsSerialization(t *testing.T) {
	stored, err := MarshalRetailDiscounts(map[string]float64{
		"deepseek": 0.6, "  Claude ": 0.9, "bad": 1.5, "zero": 0, "": 0.5,
	})
	require.NoError(t, err)
	parsed := ParseRetailDiscounts(stored)
	assert.Equal(t, 0.6, parsed["deepseek"])
	assert.Equal(t, 0.9, parsed["claude"], "trimmed + lowercased")
	assert.NotContains(t, parsed, "bad", ">1 dropped")
	assert.NotContains(t, parsed, "zero", "<=0 dropped")
	assert.Len(t, parsed, 2)

	empty, err := MarshalRetailDiscounts(map[string]float64{"bad": 2})
	require.NoError(t, err)
	assert.Equal(t, "", empty)
	assert.Empty(t, ParseRetailDiscounts(""))
}

// The overlay computes per-model retail = standard × matched ratio and the
// total, without touching the standard (billed) quota.
func TestApplyRetailDiscounts(t *testing.T) {
	report := &OrgUsageReport{
		ByModel: []OrgUsageBucket{
			{Key: "deepseek-chat", Quota: 1000},
			{Key: "gpt-4o", Quota: 500},
		},
	}
	report.ApplyRetailDiscounts(map[string]float64{"deepseek": 0.6})
	assert.Equal(t, int64(600), report.ByModel[0].RetailQuota, "deepseek 0.6")
	assert.Equal(t, int64(500), report.ByModel[1].RetailQuota, "unmatched = standard")
	assert.Equal(t, int64(1100), report.TotalRetailQuota)
	assert.Equal(t, int64(1000), report.ByModel[0].Quota, "standard billed quota unchanged")
}
