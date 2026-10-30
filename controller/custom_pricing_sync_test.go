package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertCustomAlibabaModelsToBillingExpr(t *testing.T) {
	tokenTier := func(rangeName, in, out string) customAlibabaPriceTier {
		return customAlibabaPriceTier{RangeName: rangeName, Prices: []customAlibabaPriceItem{
			{Type: "input_token", Price: in, PriceUnit: "per million tokens"},
			{Type: "output_token", Price: out, PriceUnit: "per million tokens"},
		}}
	}
	models := []customAlibabaModel{
		{Model: "flat", Prices: []customAlibabaPriceTier{tokenTier("Default", "2", "8")}},
		{Model: "tiered", Prices: []customAlibabaPriceTier{tokenTier("128k<Input<=256k", "1.2", "6"), tokenTier("Input<=32k", "0.4", "1.6"), tokenTier("32k<Input<=128k", "0.8", "3.2")}},
		{Model: "image", Prices: []customAlibabaPriceTier{{RangeName: "Default", Prices: []customAlibabaPriceItem{{Type: "image_number", Price: "0.03", PriceUnit: "per image"}}}}},
		{Model: "cache-item", Prices: []customAlibabaPriceTier{{RangeName: "Default", Prices: []customAlibabaPriceItem{
			{Type: "input_token", Price: "1", PriceUnit: "per million tokens"},
			{Type: "output_token", Price: "2", PriceUnit: "per million tokens"},
			{Type: "cached_token", Price: "0.1", PriceUnit: "per million tokens"},
		}}}},
		{Model: "odd-range", Prices: []customAlibabaPriceTier{tokenTier("Default", "1", "2"), tokenTier("Input<=32k", "1", "2")}},
		{Model: "negative", Prices: []customAlibabaPriceTier{tokenTier("Default", "-1", "2")}},
		{Model: "no-prices"},
	}

	got := convertCustomAlibabaModelsToBillingExpr(models)
	require.NotNil(t, got)
	exprs := got["billing_expr"].(map[string]string)
	modes := got["billing_mode"].(map[string]string)

	assert.Equal(t, map[string]string{
		"flat":   `tier("base", p * 2 + c * 8)`,
		"tiered": `len <= 32768 ? tier("le32k", p * 0.4 + c * 1.6) : len <= 131072 ? tier("le128k", p * 0.8 + c * 3.2) : tier("le256k", p * 1.2 + c * 6)`,
		"image":  `tier("image", fixed(0.03)) * image_count`,
	}, exprs)
	for name := range exprs {
		assert.Equal(t, "tiered_expr", modes[name])
	}
	assert.Nil(t, convertCustomAlibabaModelsToBillingExpr(models[3:]))
}

func TestCustomPricingSyncHostMatching(t *testing.T) {
	assert.True(t, isCustomAlibabaIntlBaseURL("https://dashscope-intl.aliyuncs.com"))
	assert.True(t, isCustomAlibabaIntlBaseURL("https://DashScope-Intl.aliyuncs.com/compatible-mode"))
	assert.False(t, isCustomAlibabaIntlBaseURL("https://dashscope.aliyuncs.com"))
	assert.False(t, isCustomAlibabaIntlBaseURL("https://dashscope-intl.aliyuncs.com.evil.example"))

	assert.True(t, isCustomBytePlusBaseURL("https://ark.ap-southeast.bytepluses.com/api/v3"))
	assert.False(t, isCustomBytePlusBaseURL("https://ark.cn-beijing.volces.com"))
	assert.False(t, isCustomBytePlusBaseURL("https://bytepluses.com.evil.example"))
}

func TestFetchCustomAlibabaModelsPaginatesWithBearerKey(t *testing.T) {
	pages := map[string]string{
		"1": `{"success":true,"output":{"total":3,"models":[{"model":"a"},{"model":"b"}]}}`,
		"2": `{"success":true,"output":{"total":3,"models":[{"model":"c"}]}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(pages[r.URL.Query().Get("page_no")]))
	}))
	defer server.Close()

	models, err := fetchCustomAlibabaModels(t.Context(), server.Client(), server.URL, "sk-test")
	require.NoError(t, err)
	require.Len(t, models, 3)
	assert.Equal(t, "c", models[2].Model)
}

func TestConvertCustomBytePlusPricesToBillingExpr(t *testing.T) {
	token := func(typ string, price float64) customBytePlusChargeItem {
		return customBytePlusChargeItem{Type: typ, OriginalPrice: price, UnitCode: "K tokens"}
	}
	piece := func(typ string, price float64) customBytePlusChargeItem {
		return customBytePlusChargeItem{Type: typ, OriginalPrice: price, UnitCode: "Piece"}
	}
	models := []customBytePlusModelPrice{
		{FoundationModelName: "flat", ChargeItems: []customBytePlusChargeItem{token("InferencePrompt", 0.0005), token("InferenceCompletion", 0.0025), token("ContextSessionHit", 0.0001), token("BatchInferencePrompt", 0.00025)}},
		{FoundationModelName: "tiered", MultiChargeItems: []customBytePlusPriceTier{
			{MaxPromptTokens: 0, ChargeItems: []customBytePlusChargeItem{token("InferencePrompt", 0.0005), token("InferenceCompletion", 0.004)}},
			{MaxPromptTokens: 131072, ChargeItems: []customBytePlusChargeItem{token("InferencePrompt", 0.00025), token("InferenceCompletion", 0.002), token("AudioPrompt", 0.00375)}},
		}},
		{FoundationModelName: "image", ChargeItems: []customBytePlusChargeItem{piece("Inference", 1), piece("T2ICompletion", 0.04), piece("I2ICompletion", 0.04)}},
		{FoundationModelName: "image-sizes", ChargeItems: []customBytePlusChargeItem{piece("ToICompletion", 0.045), piece("ToILargeCompletion", 0.09)}},
		{FoundationModelName: "video", ChargeItems: []customBytePlusChargeItem{token("V2VCompletion", 0.0043)}},
		{FoundationModelName: "no-completion", ChargeItems: []customBytePlusChargeItem{token("InferencePrompt", 0.1)}},
		{FoundationModelName: "inverted-tiers", MultiChargeItems: []customBytePlusPriceTier{
			{MaxPromptTokens: 0, ChargeItems: []customBytePlusChargeItem{token("InferencePrompt", 1), token("InferenceCompletion", 2)}},
			{MaxPromptTokens: 0, ChargeItems: []customBytePlusChargeItem{token("InferencePrompt", 1), token("InferenceCompletion", 2)}},
		}},
	}

	got := convertCustomBytePlusPricesToBillingExpr(models)
	require.NotNil(t, got)
	assert.Equal(t, map[string]string{
		"flat":   `tier("base", p * 0.5 + c * 2.5 + cr * 0.1)`,
		"tiered": `len <= 131072 ? tier("le131072", p * 0.25 + c * 2 + ai * 3.75) : tier("top", p * 0.5 + c * 4)`,
		"image":  `tier("image", fixed(0.04)) * image_count`,
	}, got["billing_expr"])
	assert.Nil(t, convertCustomBytePlusPricesToBillingExpr(models[4:]))
}

func TestCustomBytePlusRequestSigning(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://"+customBytePlusPricingHost+"/?"+customBytePlusPricingQuery, nil)
	require.NoError(t, err)
	signCustomBytePlusRequest(req, []byte(`{}`), "AKTEST", "secret", time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC))
	assert.Equal(t, "20261029T090000Z", req.Header.Get("X-Date"))
	assert.Contains(t, req.Header.Get("Authorization"), "HMAC-SHA256 Credential=AKTEST/20261029/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=")
}
