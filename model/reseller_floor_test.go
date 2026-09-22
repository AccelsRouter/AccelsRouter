package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The retail floor for a series token must be judged against the concrete
// offerable models the token covers (exact or prefix), taking the HIGHEST
// wholesale among them — so "claude" at 0.95 is allowed when the only sellable
// claude model has wholesale 0.80, but blocked when a covered claude model has
// no wholesale discount (1.00). Different models in one series may carry
// different wholesale ratios; the strictest one wins and is named.
func TestRetailFloorForJudgesTheModelsTheTokenCovers(t *testing.T) {
	wholesale := map[string]float64{"claude-opus-4-8": 0.80, "claude-fable-5": 0.85, "deepseek": 0.70}

	// Only opus is sellable: the series floor is opus's wholesale, not 1.00.
	floor, by, n := RetailFloorFor("claude", []string{"claude-opus-4-8", "deepseek-v4-pro"}, wholesale)
	assert.InDelta(t, 0.80, floor, 1e-9)
	assert.Equal(t, "claude-opus-4-8", by)
	assert.Equal(t, 1, n)

	// Two claude models with different wholesale: the higher one sets the floor.
	floor, by, n = RetailFloorFor("Claude", []string{"claude-opus-4-8", "claude-fable-5"}, wholesale)
	assert.InDelta(t, 0.85, floor, 1e-9)
	assert.Equal(t, "claude-fable-5", by, "the strictest covered model is named")
	assert.Equal(t, 2, n)

	// A covered model with no wholesale discount pins the floor at 1.00.
	floor, by, n = RetailFloorFor("claude", []string{"claude-opus-4-8", "claude-sonnet-5"}, wholesale)
	assert.InDelta(t, 1.0, floor, 1e-9)
	assert.Equal(t, "claude-sonnet-5", by)
	assert.Equal(t, 2, n)

	// Prefix wholesale applies to every covered model of that series.
	floor, by, n = RetailFloorFor("deepseek-", []string{"deepseek-v4-pro", "deepseek-v3.2"}, wholesale)
	assert.InDelta(t, 0.70, floor, 1e-9)
	assert.Equal(t, "deepseek-v4-pro", by)
	assert.Equal(t, 2, n)

	// An exact-model token is judged against that model only.
	floor, by, n = RetailFloorFor("claude-opus-4-8", []string{"claude-opus-4-8", "claude-fable-5"}, wholesale)
	assert.InDelta(t, 0.80, floor, 1e-9)
	assert.Equal(t, "claude-opus-4-8", by)
	assert.Equal(t, 1, n)

	// Nothing sellable matches: fall back to the token itself (conservative).
	floor, by, n = RetailFloorFor("gpt", []string{"claude-opus-4-8"}, wholesale)
	assert.InDelta(t, 1.0, floor, 1e-9)
	assert.Equal(t, "", by)
	assert.Equal(t, 0, n)
	floor, _, n = RetailFloorFor("deepseek", []string{"claude-opus-4-8"}, wholesale)
	assert.InDelta(t, 0.70, floor, 1e-9, "an unmatched token with its own wholesale entry uses that entry")
	assert.Equal(t, 0, n)

	floor, _, n = RetailFloorFor("   ", []string{"claude-opus-4-8"}, wholesale)
	assert.InDelta(t, 1.0, floor, 1e-9)
	assert.Equal(t, 0, n)
}
