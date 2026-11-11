package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
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
	assert.Equal(t, map[string]string{
		"cache-item": "unsupported price item cached_token (per million tokens, standard)",
		"odd-range":  `unreadable input range "Default"`,
		"negative":   "unreadable price of input_token",
		"no-prices":  "Model Studio lists no price",
	}, got[customPricingSkippedField])
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
{"model":"deepseek-v4.1-flash","provider":"deepseek","prices":[{"range_name":"Default","prices":[
{"type":"input_token","price":"0.44","price_unit":"Per 1M tokens","time_band":"peak"},
{"type":"output_token","price":"1.32","price_unit":"Per 1M tokens","time_band":"peak"},
{"type":"input_token_cache","price":"0.044","price_unit":"Per 1M tokens","time_band":"peak"},
{"type":"input_token","price":"0.22","price_unit":"Per 1M tokens","time_band":"off_peak"},
{"type":"output_token","price":"0.66","price_unit":"Per 1M tokens","time_band":"off_peak"},
{"type":"input_token_cache","price":"0.022","price_unit":"Per 1M tokens","time_band":"off_peak"}]}]},
{"model":"qwen-timed","provider":"qwen","prices":[{"range_name":"Default","prices":[
{"type":"input_token","price":"1","price_unit":"Per 1M tokens","time_band":"peak"},
{"type":"output_token","price":"2","price_unit":"Per 1M tokens","time_band":"peak"},
{"type":"input_token","price":"0.5","price_unit":"Per 1M tokens","time_band":"off_peak"},
{"type":"output_token","price":"1","price_unit":"Per 1M tokens","time_band":"off_peak"}]}]},
{"model":"wan2.7-image","provider":"wan","prices":[{"range_name":"Default","prices":[
{"type":"image_number","price":"0.03","price_unit":"Per image","time_band":"standard"}]}]},
{"model":"free-input-only","prices":[{"range_name":"Default","prices":[
{"type":"input_token","price":"0","price_unit":"Per 1M tokens","time_band":null}]}]}]}}`
	var page customAlibabaModelsPage
	require.NoError(t, common.Unmarshal([]byte(body), &page))
	got := convertCustomAlibabaModelsToBillingExpr(page.Output.Models)
	exprs := got[billing_setting.BillingExprField].(map[string]string)
	assert.Equal(t, `tier("base", p * 2 + c * 6 + cr * 0.25 + cc * 2.5)`, exprs["qwen3.8-max"])
	// DeepSeek models priced by time of day charge the peak price 08:00-22:00 Beijing time.
	timed := `hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 22 ? tier("peak", p * 0.44 + c * 1.32 + cr * 0.044) : tier("off_peak", p * 0.22 + c * 0.66 + cr * 0.022)`
	assert.Equal(t, timed, exprs["deepseek-v4.1-flash"])
	assert.NoError(t, billing_setting.SmokeTestExpr(timed))
	// The alibaba task plugin serves this model, so its price reads the plugin's image_count fact.
	assert.Equal(t, `tier("image", u("image_count") * 0.03)`, exprs["wan2.7-image"])
	assert.Empty(t, customBillingExprError("wan2.7-image", exprs["wan2.7-image"]))
	assert.Equal(t, map[string]string{
		"peaky":           "time-of-day prices without both a peak and an off-peak input and output price",
		"qwen-timed":      "priced by time of day, and the peak hours are only known for DeepSeek models",
		"free-input-only": "no input and output token price",
	}, got[customPricingSkippedField])
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
	// The removed model is ignored by the upstream model sync, which would add it back.
	ignored := stored.GetOtherSettings().UpstreamModelUpdateIgnoredModels
	assert.Equal(t, []string{"b"}, ignored)
	pendingAdd, _ := collectPendingUpstreamModelChangesFromModels(stored.GetModels(), []string{"a", "b", "c", "d"}, ignored, nil)
	assert.Equal(t, []string{"d"}, pendingAdd)
	var abilities int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", channel.Id, "b").Count(&abilities).Error)
	assert.Zero(t, abilities)

	body["items"] = []map[string]any{{"channel_id": 0, "models": []string{"a"}}}
	modelManagementRequest(t, CustomRemoveChannelModels, http.MethodPost, "/api/channel/remove_models", body, &response)
	assert.False(t, response.Success)
}

// ByteDance's two pricing pages: the international one in USD under dated
// model IDs, the mainland China one in CNY under family names. Only the
// standard online inference table counts, only ByteDance's own models, and a
// model priced by time of day is left out.
func TestParseCustomByteDancePricing(t *testing.T) {
	value := func(v float64) *float64 { return &v }

	byteplus, err := parseCustomBytePlusVendorPricing(customVendorFixture(t, "byteplus.md"))
	require.NoError(t, err)
	assert.Equal(t, customVendorPrice{Provider: customVendorBytePlusProvider, Input: 0.5, Output: value(2.5), CacheRead: value(0.1)}, byteplus["dola-seed-2-1-turbo"])
	pro := customVendorPrice{Provider: customVendorBytePlusProvider, Input: 0.5, Output: value(3), CacheRead: value(0.1), Tiers: []customVendorTier{
		{Upper: 128000, Input: 0.5, Output: value(3), CacheRead: value(0.1)},
		{Input: 1, Output: value(6), CacheRead: value(0.2)},
	}}
	assert.Equal(t, pro, byteplus["seed-2-0-pro-260328"])
	assert.Equal(t, pro, byteplus["seed-2-0-pro"], "a model ID also answers without its snapshot date")
	// Audio input is priced in a table of its own.
	expr, ok := customVendorSyncExpr("seed-2-0-lite-260428", byteplus["seed-2-0-lite-260428"])
	require.True(t, ok)
	assert.Equal(t, `len <= 128000 ? tier("base", p * 0.25 + c * 2 + cr * 0.05 + ai * 3.75) : tier("tier_2", p * 0.5 + c * 4 + cr * 0.1 + ai * 7.5)`, expr)
	assert.NotContains(t, byteplus, "deepseek-v4-pro-ga-260813")
	assert.NotContains(t, byteplus, "glm-5-2-260617")
	// Video, image and embedding models: the price sync offers each as the
	// expression the task plugin serving that name can charge.
	syncExpr := func(prices map[string]customVendorPrice, listed, served string) string {
		t.Helper()
		require.Contains(t, prices, listed)
		expr, ok := customVendorSyncExpr(served, prices[listed])
		require.True(t, ok, served)
		return expr
	}
	assert.Equal(t, `tier("base", u("tokens") * 2.5 / 1000000)`, syncExpr(byteplus, "seedance-1-0-pro-250528", "seedance-1-0-pro-250528"))
	assert.Equal(t, `u("resolution") == "480p" && u("video_input") == "none" ? tier("480p·none", u("tokens") * 5.6 / 1000000) : `+
		`u("resolution") == "480p" && u("video_input") == "video" ? tier("480p·video", u("tokens") * 3.3 / 1000000) : `+
		`u("resolution") == "720p" && u("video_input") == "none" ? tier("720p·none", u("tokens") * 5.6 / 1000000) : `+
		`tier("720p·video", u("tokens") * 3.3 / 1000000)`, syncExpr(byteplus, "dreamina-seedance-2-0-fast-260128", "dreamina-seedance-2-0-fast-260128"))
	assert.Equal(t, `u("layer_decomposition") == true ? `+
		`tier("layer_decomposition", u("images_above_1_5k") * 0.045 + u("images_up_to_1_5k") * 0.0225 + max(u("input_images") - 1, 0) * 0.003) : `+
		`tier("base", u("images_above_1_5k") * 0.09 + u("images_up_to_1_5k") * 0.045 + max(u("input_images") - 1, 0) * 0.003)`,
		syncExpr(byteplus, "dola-seedream-5-0-pro-260628", "dola-seedream-5-0-pro-260628"))
	// This model only outputs images above 1.5K, so its plugin has no lower tier.
	assert.Equal(t, `tier("base", u("images_above_1_5k") * 0.04)`, syncExpr(byteplus, "seedream-4-5-251128", "seedream-4-5-251128"))
	assert.Equal(t, `tier("base", p * 0.125 + c * 0 + img * 0.325)`, syncExpr(byteplus, "skylark-embedding-vision", "skylark-embedding-vision"))
	// The billing examples further down the page are not prices.
	assert.NotContains(t, byteplus, "1080p")
	assert.NotContains(t, byteplus, "dreamina")

	setRate := func(value string) {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if common.OptionMap == nil {
			common.OptionMap = make(map[string]string)
		}
		common.OptionMap[customVendorCNYPerUSDOption] = value
	}
	t.Cleanup(func() { setRate("") })
	setRate("8")
	volcengine, err := parseCustomVolcenginePricing(customVendorFixture(t, "volcengine.md"))
	require.NoError(t, err)
	provider := customVendorVolcengineProvider + " (CNY at 8 per USD)"
	assert.Equal(t, customVendorPrice{Provider: provider, Input: 0.75, Output: value(3.75), CacheRead: value(0.15)}, volcengine["doubao-seed-2.1-pro"])
	assert.Equal(t, volcengine["doubao-seed-2.1-pro"], volcengine["doubao-seed-2-1-pro"], "model IDs write the version with dashes")
	assert.Equal(t, customVendorPrice{Provider: provider, Input: 0.4, Output: value(2), CacheRead: value(0.08), Tiers: []customVendorTier{
		{Upper: 32000, Input: 0.4, Output: value(2), CacheRead: value(0.08)},
		{Upper: 128000, Input: 0.6, Output: value(3), CacheRead: value(0.12)},
		{Input: 1.2, Output: value(6), CacheRead: value(0.24)},
	}}, volcengine["doubao-seed-2.0-pro"])
	assert.Equal(t, customVendorPrice{Provider: provider, Input: 0.1, Output: value(0.3375), CacheRead: value(0.02), Tiers: []customVendorTier{
		{Input: 0.1, Output: value(0.3375), CacheRead: value(0.02), AudioInput: value(1.5)},
	}}, volcengine["doubao-seed-2.1-lite"], "the low-latency table below does not replace the standard price")
	assert.Equal(t, customVendorPrice{Provider: provider, Input: 0.15, Output: value(0.45)}, volcengine["doubao-seed-translation"])
	for name := range volcengine {
		assert.True(t, strings.HasPrefix(name, "doubao-"), name)
	}
	// A family price is charged under the dated name this gateway serves.
	assert.Equal(t, `tier("base", u("tokens") * 1.875 / 1000000)`, syncExpr(volcengine, "doubao-seedance-1-0-pro", "doubao-seedance-1-0-pro-250528"))
	assert.Equal(t, `u("resolution") == "480p" && u("video_input") == "none" ? tier("480p·none", u("tokens") * 4.625 / 1000000) : `+
		`u("resolution") == "480p" && u("video_input") == "video" ? tier("480p·video", u("tokens") * 2.75 / 1000000) : `+
		`u("resolution") == "720p" && u("video_input") == "none" ? tier("720p·none", u("tokens") * 4.625 / 1000000) : `+
		`tier("720p·video", u("tokens") * 2.75 / 1000000)`, syncExpr(volcengine, "doubao-seedance-2-0-fast", "doubao-seedance-2-0-fast-260128"))
	assert.Equal(t, `u("layer_decomposition") == true ? `+
		`tier("layer_decomposition", u("images_above_1_5k") * 0.0375 + u("images_up_to_1_5k") * 0.01875 + max(u("input_images") - 1, 0) * 0.0025) : `+
		`tier("base", u("images_above_1_5k") * 0.075 + u("images_up_to_1_5k") * 0.0375 + max(u("input_images") - 1, 0) * 0.0025)`,
		syncExpr(volcengine, "doubao-seedream-5-0-pro", "doubao-seedream-5-0-pro-260628"))
	assert.Equal(t, `tier("base", p * 0.0875 + c * 0 + img * 0.225)`, syncExpr(volcengine, "doubao-embedding-vision", "doubao-embedding-vision"))
	// The family name itself is no model a plugin serves.
	_, ok = customVendorSyncExpr("doubao-seedance-2-0-fast", volcengine["doubao-seedance-2-0-fast"])
	assert.False(t, ok)

	// Without the rate nothing is read: the payment exchange rate is no substitute.
	setRate("")
	_, err = parseCustomVolcenginePricing(customVendorFixture(t, "volcengine.md"))
	assert.ErrorContains(t, err, "CNY per USD")
}

func customVendorFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/vendor_pages/" + name)
	require.NoError(t, err)
	return data
}

func assertCustomVendorPrice(t *testing.T, prices map[string]customVendorPrice, model string, input, output float64) {
	t.Helper()
	price, ok := prices[model]
	require.True(t, ok, "missing %s", model)
	assert.InDelta(t, input, price.Input, 1e-9, model)
	require.NotNil(t, price.Output, model)
	assert.InDelta(t, output, *price.Output, 1e-9, model)
}

func TestCustomVendorPageParsers(t *testing.T) {
	zai, err := parseCustomZaiPricing(customVendorFixture(t, "zai.md"))
	require.NoError(t, err)
	assertCustomVendorPrice(t, zai, "glm-5.3", 1.4, 4.4)
	assertCustomVendorPrice(t, zai, "glm-5.1", 1.4, 4.4)
	assertCustomVendorPrice(t, zai, "glm-5.3-flash", 0.15, 0.5)
	assert.NotContains(t, zai, "glm-4.7-flash", "free models are not priced")
	require.NotNil(t, zai["glm-5.3"].CacheRead)
	assert.InDelta(t, 0.26, *zai["glm-5.3"].CacheRead, 1e-9)

	deepseek, err := parseCustomDeepSeekPricing(customVendorFixture(t, "deepseek.md"))
	require.NoError(t, err)
	assertCustomVendorPrice(t, deepseek, "deepseek-v4-pro", 1.32, 3.96)
	assertCustomVendorPrice(t, deepseek, "deepseek-v4-pro-0813", 1.32, 3.96)
	assertCustomVendorPrice(t, deepseek, "deepseek-flash", 0.3, 1.2)
	assertCustomVendorPrice(t, deepseek, "deepseek-v4-flash", 0.3, 1.2)
	assertCustomVendorPrice(t, deepseek, "deepseek-v4.1-flash-1017", 0.3, 1.2)

	kimi, err := parseCustomKimiPricing(customVendorFixture(t, "kimi.html"))
	require.NoError(t, err)
	assertCustomVendorPrice(t, kimi, "kimi-k3", 3, 15)
	assertCustomVendorPrice(t, kimi, "kimi-k3.1-pro", 2, 10)
	assertCustomVendorPrice(t, kimi, "kimi-k2.7-code", 0.95, 4)

	claude, err := parseCustomClaudePricing(customVendorFixture(t, "claude.md"))
	require.NoError(t, err)
	assertCustomVendorPrice(t, claude, "claude-opus-5-5", 4, 20)
	assertCustomVendorPrice(t, claude, "claude-sonnet-4-6", 3, 15)
	assertCustomVendorPrice(t, claude, "claude-haiku-5-5", 0.1, 0.5)

	xai, err := parseCustomXAIPricing(customVendorFixture(t, "xai.md"))
	require.NoError(t, err)
	assertCustomVendorPrice(t, xai, "grok-4.7", 2, 6)
	assertCustomVendorPrice(t, xai, "grok-4.3", 1.25, 2.5)
	require.Len(t, xai["grok-4.7"].Tiers, 2)
	assert.Equal(t, 200000.0, xai["grok-4.7"].Tiers[0].Upper)
	assert.Equal(t, 2.0, xai["grok-4.7"].Tiers[0].Input)
	assert.Equal(t, 0.0, xai["grok-4.7"].Tiers[1].Upper)
	assert.Equal(t, 4.0, xai["grok-4.7"].Tiers[1].Input)
	require.NotNil(t, xai["grok-4.7"].Tiers[1].Output)
	assert.Equal(t, 12.0, *xai["grok-4.7"].Tiers[1].Output)
	require.Len(t, claude["claude-haiku-5-5"].Tiers, 2)
	assert.Equal(t, 100000.0, claude["claude-haiku-5-5"].Tiers[0].Upper)
	assert.Equal(t, 0.5, claude["claude-haiku-5-5"].Tiers[1].Input)
	assert.Empty(t, claude["claude-opus-5-5"].Tiers, "untiered models carry no tier list")

	// DeepSeek charges half off-peak; both prices are kept as time bands.
	require.Len(t, deepseek["deepseek-v4-pro"].Tiers, 2)
	assert.Equal(t, "peak", deepseek["deepseek-v4-pro"].Tiers[0].Band)
	assert.Equal(t, "off_peak", deepseek["deepseek-v4-pro"].Tiers[1].Band)
	assert.Equal(t, 0.66, deepseek["deepseek-v4-pro"].Tiers[1].Input)
	require.NotNil(t, deepseek["deepseek-flash"].Tiers[1].CacheRead)
	assert.Equal(t, 0.003, *deepseek["deepseek-flash"].Tiers[1].CacheRead)
	assertCustomVendorPrice(t, deepseek, "deepseek-v4-flash-vision-exp", 0.3, 1.2)

	// Gemini: a price with an effective date follows the date, prompt-length
	// tiers and a separate audio input price are kept, and models priced per
	// minute, per image or by output modality are left out.
	google, err := parseCustomGooglePricing(customVendorFixture(t, "google.md"), time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assertCustomVendorPrice(t, google, "gemini-4-argon", 2, 10)
	require.NotNil(t, google["gemini-4-argon"].CacheRead)
	assert.Equal(t, 0.1, *google["gemini-4-argon"].CacheRead)
	assertCustomVendorPrice(t, google, "gemini-2.5-pro", 1.25, 10)
	require.Len(t, google["gemini-2.5-pro"].Tiers, 2)
	assert.Equal(t, 200000.0, google["gemini-2.5-pro"].Tiers[0].Upper)
	assert.Equal(t, 2.5, google["gemini-2.5-pro"].Tiers[1].Input)
	assert.Equal(t, 15.0, *google["gemini-2.5-pro"].Tiers[1].Output)
	assert.Equal(t, 0.25, *google["gemini-2.5-pro"].Tiers[1].CacheRead)
	assertCustomVendorPrice(t, google, "gemini-2.5-flash", 0.3, 2.5)
	require.Len(t, google["gemini-2.5-flash"].Tiers, 1)
	assert.Equal(t, 1.0, *google["gemini-2.5-flash"].Tiers[0].AudioInput)
	for _, name := range []string{"gemini-3.1-flash-tts-preview", "gemini-3.5-transcribe", "gemini-3.1-flash-image", "gemini-embedding-001"} {
		assert.NotContains(t, google, name)
	}
	later, err := parseCustomGooglePricing(customVendorFixture(t, "google.md"), time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assertCustomVendorPrice(t, later, "gemini-4-argon", 4, 20)

	// OpenAI's own models from OpenRouter's list: long-prompt overrides become
	// tiers and the cache write price is kept; batch variants, gpt-oss, models
	// with audio prices and other providers are left out.
	openai, err := parseCustomOpenAIPricing(customVendorFixture(t, "openrouter.json"))
	require.NoError(t, err)
	assert.Len(t, openai, 3)
	assertCustomVendorPrice(t, openai, "gpt-5", 1.25, 10)
	assert.Empty(t, openai["gpt-5"].Tiers)
	assertCustomVendorPrice(t, openai, "gpt-6.1-sol", 2, 10)
	require.Len(t, openai["gpt-6.1-sol"].Tiers, 2)
	assert.Equal(t, 272000.0, openai["gpt-6.1-sol"].Tiers[0].Upper)
	assert.Equal(t, 2.5, *openai["gpt-6.1-sol"].Tiers[0].CacheWrite)
	assert.Equal(t, 4.0, openai["gpt-6.1-sol"].Tiers[1].Input)
	assert.Equal(t, 15.0, *openai["gpt-6.1-sol"].Tiers[1].Output)
	assert.Contains(t, openai, "gpt-5.4")

	_, err = parseCustomZaiPricing([]byte("nothing here"))
	assert.Error(t, err)
}

func TestCustomAlibabaVendorPrices(t *testing.T) {
	const body = `{"success":true,"output":{"models":[
{"model":"qwen3.7-flash","provider":"qwen","prices":[
{"range_name":"256k<Input<=1m","prices":[{"type":"input_token","price":"0.2","price_unit":"Per 1M tokens","time_band":"standard"},{"type":"output_token","price":"0.8","price_unit":"Per 1M tokens","time_band":"standard"}]},
{"range_name":"Input<=32k","prices":[{"type":"input_token","price":"0.03","price_unit":"Per 1M tokens","time_band":"standard"},{"type":"output_token","price":"0.13","price_unit":"Per 1M tokens","time_band":"standard"},{"type":"input_token_cache","price":"0.006","price_unit":"Per 1M tokens","time_band":"standard"}]},
{"range_name":"32k<Input<=256k","prices":[{"type":"input_token","price":"0.1","price_unit":"Per 1M tokens","time_band":"standard"},{"type":"output_token","price":"0.4","price_unit":"Per 1M tokens","time_band":"standard"}]}]},
{"model":"qwen-max","provider":"qwen","prices":[{"range_name":"Default","prices":[{"type":"input_token","price":"1.6","price_unit":"Per 1M tokens","time_band":"standard"},{"type":"output_token","price":"6.4","price_unit":"Per 1M tokens","time_band":"standard"}]}]},
{"model":"deepseek-v4-pro","provider":"deepseek","prices":[{"range_name":"Default","prices":[{"type":"input_token","price":"2.4","price_unit":"Per 1M tokens","time_band":"standard"},{"type":"output_token","price":"4.8","price_unit":"Per 1M tokens","time_band":"standard"}]}]},
{"model":"qwen-peaky","provider":"qwen","prices":[{"range_name":"Default","prices":[{"type":"input_token","price":"1","price_unit":"Per 1M tokens","time_band":"peak"},{"type":"output_token","price":"2","price_unit":"Per 1M tokens","time_band":"peak"}]}]}]}}`
	var page customAlibabaModelsPage
	require.NoError(t, common.Unmarshal([]byte(body), &page))
	prices := customAlibabaVendorPrices(page.Output.Models)

	flash := prices["qwen3.7-flash"]
	require.Len(t, flash.Tiers, 3, "tiers are sorted by input length")
	assert.Equal(t, 0.03, flash.Input)
	assert.Equal(t, 32.0*1024, flash.Tiers[0].Upper)
	assert.Equal(t, 256.0*1024, flash.Tiers[1].Upper)
	assert.Equal(t, 0.0, flash.Tiers[2].Upper, "the last tier has no upper bound")
	assert.Equal(t, 0.2, flash.Tiers[2].Input)
	require.NotNil(t, flash.CacheRead)
	assert.Equal(t, 0.006, *flash.CacheRead)

	assert.Empty(t, prices["qwen-max"].Tiers, "a single tier needs no tier list")
	assert.Equal(t, 1.6, prices["qwen-max"].Input)
	assert.NotContains(t, prices, "deepseek-v4-pro", "third-party models are not Alibaba's own price")
	assert.NotContains(t, prices, "qwen-peaky", "time-band pricing is not guessed")
}

func TestCustomAlibabaVendorNoteExplainsSkippedModels(t *testing.T) {
	models := []customAlibabaModel{
		{Model: "qwen-max", Provider: "qwen"},
		{Model: "qwen-peaky", Provider: "qwen"},
		{Model: "deepseek-v4-pro", Provider: "deepseek"},
	}
	prices := map[string]customVendorPrice{"qwen-max": {}}
	note := customAlibabaVendorNote(models, prices)
	assert.Contains(t, note, "lists 3 models; 1 are third-party")
	assert.Contains(t, note, "1 Qwen models skipped for unreadable prices (e.g. qwen-peaky)")
}

// An upstream's usage expression for a model no local task plugin serves is
// flagged for the sync preview instead of failing the whole save; ordinary
// token expressions and usage-free candidates are not.
func TestCustomBillingExprErrorFlagsUnservedUsageExpressions(t *testing.T) {
	var errors, conversions map[string]string
	usage := map[string]any{billing_setting.BillingExprField: `u("audio") == "on" ? tier("on", u("seconds") * 0.13) : tier("off", u("seconds") * 0.09)`}
	// Built-in plugins are registered here, so the model must be one none of them serves.
	customRecordBillingExprError(&errors, &conversions, "aigc", "vendor-video-x", usage)
	require.Contains(t, errors, "aigc")
	assert.Contains(t, errors["aigc"], "no task plugin usage schema")
	assert.Contains(t, errors["aigc"], "seconds")

	customRecordBillingExprError(&errors, &conversions, "other", "gpt-image-2.6-flare", map[string]any{billing_setting.BillingExprField: `tier("base", p * 5 + c * 1.25 + img * 8)`})
	customRecordBillingExprError(&errors, &conversions, "other", "gpt-image-2.6-flare", map[string]any{"model_ratio": 2.5})
	assert.NotContains(t, errors, "other")
	assert.Len(t, errors, 1)
	assert.Empty(t, conversions)
}

// A per-character task price of a model no local plugin serves becomes the
// ordinary expression the speech relay bills with (characters are the prompt
// tokens), and the original is reported; non-linear character prices and
// prices with a constant or request rules stay errors.
func TestCustomCharacterExpressionConversion(t *testing.T) {
	var errors, conversions map[string]string
	candidate := map[string]any{billing_setting.BillingModeField: billing_setting.BillingModeTieredExpr, billing_setting.BillingExprField: `u("characters") / 10000 * 0.4412`}
	customRecordBillingExprError(&errors, &conversions, "aigc", "seed-tts-2.0", candidate)
	assert.Empty(t, errors)
	assert.Equal(t, `tier("base", p * 44.12)`, candidate[billing_setting.BillingExprField])
	assert.Equal(t, `u("characters") / 10000 * 0.4412`, conversions["aigc"])

	assert.Equal(t, `tier("base", p * 50)`, customConvertCharacterExpr(`tier("base", u("characters") * 0.00005)`))
	assert.Equal(t, `tier("base", p * 0)`, customConvertCharacterExpr(`u("characters") * 0`))
	for name, expression := range map[string]string{
		"tiered":        `u("characters") > 10000 ? tier("big", u("characters") * 0.00004) : tier("small", u("characters") * 0.00005)`,
		"constant":      `tier("base", 0.01 + u("characters") * 0.00005)`,
		"other key":     `tier("base", u("characters") * 0.00005 + u("seconds") * 0.1)`,
		"request rule":  `(tier("base", u("characters") * 0.00005)) * (header("x-fast") == "1" ? 2 : 1)`,
		"not a usage":   `tier("base", p * 5)`,
		"fixed pricing": `tier("base", fixed(0.01))`,
	} {
		assert.Empty(t, customConvertCharacterExpr(expression), name)
	}
	errors, conversions = nil, nil
	customRecordBillingExprError(&errors, &conversions, "aigc", "seed-tts-2.0", map[string]any{billing_setting.BillingExprField: `tier("base", 0.01 + u("characters") * 0.00005)`})
	assert.Contains(t, errors, "aigc")
	assert.Empty(t, conversions)
}
