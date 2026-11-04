package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
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

func TestConvertCustomAlibabaModelsRealResponseShape(t *testing.T) {
	const body = `{"success":true,"output":{"models":[
{"model":"qwen3.8-max","prices":[{"range_name":"Default","prices":[
{"type":"input_token","price":"2","price_unit":"Per 1M tokens","time_band":"standard"},
{"type":"output_token","price":"6","price_unit":"Per 1M tokens","time_band":"standard"},
{"type":"input_token_cache","price":"0.25","price_unit":"Per 1M tokens","time_band":"standard"},
{"type":"input_token_batch","price":"1","price_unit":"Per 1M tokens","time_band":"standard"},
{"type":"output_token_batch","price":"3","price_unit":"Per 1M tokens","time_band":"standard"},
{"type":"input_token_cache_creation_5m","price":"2.5","price_unit":"Per 1M tokens","time_band":"standard"},
{"type":"input_token_cache_read","price":"0.17","price_unit":"Per 1M tokens","time_band":"standard"}]}]},
{"model":"peaky","prices":[{"range_name":"Default","prices":[
{"type":"input_token","price":"1","price_unit":"Per 1M tokens","time_band":"peak"},
{"type":"output_token","price":"2","price_unit":"Per 1M tokens","time_band":"peak"}]}]},
{"model":"free-input-only","prices":[{"range_name":"Default","prices":[
{"type":"input_token","price":"0","price_unit":"Per 1M tokens","time_band":null}]}]}]}}`
	var page customAlibabaModelsPage
	require.NoError(t, common.Unmarshal([]byte(body), &page))
	got := convertCustomAlibabaModelsToBillingExpr(page.Output.Models)
	exprs := got[billing_setting.BillingExprField].(map[string]string)
	assert.Equal(t, `tier("base", p * 2 + c * 6 + cr * 0.25 + cc * 2.5)`, exprs["qwen3.8-max"])
	assert.NotContains(t, exprs, "peaky")
	assert.NotContains(t, exprs, "free-input-only")
}

func TestCustomRemoveChannelModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	channel := &model.Channel{Name: "expensive", Key: "k", Status: common.ChannelStatusEnabled, Models: "a,b,c", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	var response struct {
		Success bool
		Message string
	}
	body := map[string]any{"items": []map[string]any{{"channel_id": channel.Id, "models": []string{"b", "missing"}}}}
	modelManagementRequest(t, CustomRemoveChannelModels, http.MethodPost, "/api/channel/remove_models", body, &response)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, "a,c", stored.Models)
	var abilities int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", channel.Id, "b").Count(&abilities).Error)
	assert.Zero(t, abilities)

	body["items"] = []map[string]any{{"channel_id": 0, "models": []string{"a"}}}
	modelManagementRequest(t, CustomRemoveChannelModels, http.MethodPost, "/api/channel/remove_models", body, &response)
	assert.False(t, response.Success)
}

func TestCustomVendorPricesFromModelsDev(t *testing.T) {
	const body = `{
"hpc-ai":{"models":{"glm-5.1":{"cost":{"input":0.615,"output":2.46}}}},
"zai":{"models":{"glm-5.1":{"cost":{"input":1.4,"output":4.4,"cache_read":0.26}},"free":{"cost":{"input":0,"output":0}}}},
"alibaba":{"models":{"glm-5.1":{"cost":{"input":9,"output":9}},"qwen3.7-plus":{"cost":{"input":0.4,"output":1.6}}}},
"alibaba-cn":{"models":{"qwen-cn-only":{"cost":{"input":0.3,"output":1}}}}}`
	prices, err := customVendorPricesFromModelsDev(strings.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, "zai", prices["glm-5.1"].Provider)
	assert.Equal(t, 1.4, prices["glm-5.1"].Input)
	assert.Equal(t, "alibaba", prices["qwen3.7-plus"].Provider)
	assert.NotContains(t, prices, "free")
	assert.NotContains(t, prices, "qwen-cn-only")
	_, err = customVendorPricesFromModelsDev(strings.NewReader(`{"hpc-ai":{"models":{}}}`))
	assert.Error(t, err)
}
