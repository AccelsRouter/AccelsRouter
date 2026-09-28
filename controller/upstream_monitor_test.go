package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The price comparison must read both upstream payload shapes and classify
// the platform price against the upstream one deterministically.
func TestParseUpstreamPricingBothShapes(t *testing.T) {
	ratioConfig := []byte(`{"success":true,"data":{"model_ratio":{"gpt-4o":2.5},"completion_ratio":{"gpt-4o":4},"model_price":{"dall-e-3":0.04}}}`)
	p, err := parseUpstreamPricing(ratioConfig)
	require.NoError(t, err)
	assert.Equal(t, &upstreamPrice{ModelRatio: 2.5, CompletionRatio: 4}, p.priceOf("gpt-4o"))
	assert.Equal(t, &upstreamPrice{PerCall: true, ModelPrice: 0.04}, p.priceOf("dall-e-3"))
	assert.Nil(t, p.priceOf("unknown"))

	pricing := []byte(`{"success":true,"data":[{"model_name":"gpt-4o","quota_type":0,"model_ratio":2.5,"completion_ratio":4},{"model_name":"dall-e-3","quota_type":1,"model_price":0.04}]}`)
	p, err = parseUpstreamPricing(pricing)
	require.NoError(t, err)
	assert.Equal(t, &upstreamPrice{ModelRatio: 2.5, CompletionRatio: 4}, p.priceOf("gpt-4o"))
	assert.Equal(t, &upstreamPrice{PerCall: true, ModelPrice: 0.04}, p.priceOf("dall-e-3"))

	_, err = parseUpstreamPricing([]byte(`{"success":false,"message":"nope"}`))
	require.Error(t, err)
	_, err = parseUpstreamPricing([]byte(`<html>`))
	require.Error(t, err)
	_, err = parseUpstreamPricing([]byte(`{"success":true,"data":[]}`))
	require.Error(t, err, "a payload without models is not usable")
}

func TestComparePrices(t *testing.T) {
	ratio := func(in, comp float64) *upstreamPrice {
		return &upstreamPrice{ModelRatio: in, CompletionRatio: comp}
	}
	perCall := func(v float64) *upstreamPrice { return &upstreamPrice{PerCall: true, ModelPrice: v} }
	cases := []struct {
		name            string
		local, upstream *upstreamPrice
		want            string
	}{
		{"same ratios", ratio(2.5, 4), ratio(2.5, 4), "match"},
		{"float noise is a match", ratio(2.5, 4), ratio(2.5000000001, 4), "match"},
		{"platform charges more on both", ratio(3, 4), ratio(2.5, 4), "platform_higher"},
		{"platform charges less on output", ratio(2.5, 3), ratio(2.5, 4), "platform_lower"},
		{"input higher, output lower", ratio(3, 2), ratio(2.5, 4), "mixed"},
		{"per-call equal", perCall(0.04), perCall(0.04), "match"},
		{"per-call platform cheaper", perCall(0.03), perCall(0.04), "platform_lower"},
		{"per-call vs per-token", perCall(0.04), ratio(2.5, 4), "type_mismatch"},
		{"upstream does not list it", ratio(2.5, 4), nil, "missing_upstream"},
		{"platform has no price", nil, ratio(2.5, 4), "unpriced_local"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, comparePrices(tc.local, tc.upstream), tc.name)
	}
}
