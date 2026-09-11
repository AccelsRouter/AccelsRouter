package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The retail discount matches the longest series token that is a substring of
// the model name, defaulting to 1.0 (no discount) when nothing matches.
func TestRetailDiscountFor(t *testing.T) {
	d := map[string]float64{"deepseek": 0.6, "kimi": 0.8, "claude": 0.9}
	assert.Equal(t, 0.6, RetailDiscountFor("deepseek-chat", d))
	assert.Equal(t, 0.6, RetailDiscountFor("DeepSeek-Reasoner", d), "case-insensitive")
	assert.Equal(t, 0.9, RetailDiscountFor("claude-opus-4", d))
	assert.Equal(t, 0.8, RetailDiscountFor("kimi-k2", d))
	assert.Equal(t, 1.0, RetailDiscountFor("gpt-4o", d), "no matching series = no discount")
	assert.Equal(t, 1.0, RetailDiscountFor("anything", map[string]float64{}), "empty table = no discount")
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
