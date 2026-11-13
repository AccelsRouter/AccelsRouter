package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/larknotify"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCustomVendorCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CustomVendorPrice{}, &model.CustomVendorPriceChange{}, &model.CustomVendorPriceSource{}))
	return db
}

func customVendorTestPrice(input, output float64) customVendorPrice {
	return customVendorPrice{Provider: "test", Input: input, Output: &output}
}

func TestApplyCustomVendorSourceStoresAndTracksChanges(t *testing.T) {
	db := setupCustomVendorCatalogDB(t)
	summary := &customVendorChangeSummary{}

	// The first fill records nothing as a change.
	require.NoError(t, applyCustomVendorSource("z.ai", map[string]customVendorPrice{
		"glm-5.1": customVendorTestPrice(1.4, 4.4),
		"glm-5.3": customVendorTestPrice(1.4, 4.4),
	}, false, 100, summary))
	var changes int64
	require.NoError(t, db.Model(&model.CustomVendorPriceChange{}).Count(&changes).Error)
	assert.Zero(t, changes)
	assert.Zero(t, summary.Applied+summary.Pending)

	// A small move is applied and recorded; an identical price is left alone.
	require.NoError(t, applyCustomVendorSource("z.ai", map[string]customVendorPrice{
		"glm-5.1": customVendorTestPrice(1.5, 4.4),
		"glm-5.3": customVendorTestPrice(1.4, 4.4),
	}, false, 200, summary))
	assert.Equal(t, 1, summary.Applied)
	require.Len(t, summary.Report.Changes, 1)
	change := summary.Report.Changes[0]
	assert.Equal(t, larknotify.VendorPriceChanged, change.Kind)
	assert.Equal(t, "glm-5.1", change.Model)
	assert.Equal(t, 1.4, change.Old[0].Input)
	assert.Equal(t, 1.5, change.New[0].Input)
	var row model.CustomVendorPrice
	require.NoError(t, db.Where("model_name = ? AND source = ?", "glm-5.1", "z.ai").First(&row).Error)
	assert.InDelta(t, 1.5, row.InputPrice, 1e-9)
	require.NoError(t, db.Model(&model.CustomVendorPriceChange{}).Where("status = ?", "applied").Count(&changes).Error)
	assert.EqualValues(t, 1, changes)
}

func TestApplyCustomVendorSourceHoldsLargeJumpsForReview(t *testing.T) {
	db := setupCustomVendorCatalogDB(t)
	summary := &customVendorChangeSummary{}
	require.NoError(t, applyCustomVendorSource("kimi", map[string]customVendorPrice{"kimi-k3": customVendorTestPrice(3, 15)}, false, 100, summary))

	jump := map[string]customVendorPrice{"kimi-k3": customVendorTestPrice(30, 150)}
	require.NoError(t, applyCustomVendorSource("kimi", jump, false, 200, summary))
	assert.Equal(t, 1, summary.Pending)
	var row model.CustomVendorPrice
	require.NoError(t, db.Where("model_name = ?", "kimi-k3").First(&row).Error)
	assert.InDelta(t, 3, row.InputPrice, 1e-9, "the stored price is untouched")

	// A price converted at a new rate carries a new provider label and is the
	// administrator's own move, however far it goes.
	require.NoError(t, applyCustomVendorSource("volcengine", map[string]customVendorPrice{"doubao-x": {Provider: "Ark (CNY at 1 per USD)", Input: 70, Output: row.OutputPrice}}, false, 200, summary))
	require.NoError(t, applyCustomVendorSource("volcengine", map[string]customVendorPrice{"doubao-x": {Provider: "Ark (CNY at 6.67 per USD)", Input: 10.494753, Output: row.OutputPrice}}, false, 250, summary))
	var converted model.CustomVendorPrice
	require.NoError(t, db.Where("model_name = ?", "doubao-x").First(&converted).Error)
	assert.InDelta(t, 10.494753, converted.InputPrice, 1e-9)
	assert.Equal(t, 1, summary.Pending)

	// The same suspicious value does not queue a second review.
	require.NoError(t, applyCustomVendorSource("kimi", jump, false, 300, summary))
	var pending []model.CustomVendorPriceChange
	require.NoError(t, db.Where("status = ?", "pending").Find(&pending).Error)
	require.Len(t, pending, 1)

	accept := func(id int, accepted bool) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(id)}}
		resolveCustomVendorChange(c, accepted)
		require.Contains(t, recorder.Body.String(), `"success":true`)
	}
	accept(pending[0].ID, true)
	require.NoError(t, db.Where("model_name = ?", "kimi-k3").First(&row).Error)
	assert.InDelta(t, 30, row.InputPrice, 1e-9, "accepting applies the held price")

	// A rejected value is not queued again.
	require.NoError(t, applyCustomVendorSource("kimi", map[string]customVendorPrice{"kimi-k3": customVendorTestPrice(300, 1500)}, false, 400, summary))
	require.NoError(t, db.Where("status = ?", "pending").Find(&pending).Error)
	require.Len(t, pending, 1)
	accept(pending[0].ID, false)
	require.NoError(t, applyCustomVendorSource("kimi", map[string]customVendorPrice{"kimi-k3": customVendorTestPrice(300, 1500)}, false, 500, summary))
	require.NoError(t, db.Where("status = ?", "pending").Find(&pending).Error)
	assert.Empty(t, pending)
}

func TestApplyCustomVendorSourceRefusesABrokenSource(t *testing.T) {
	db := setupCustomVendorCatalogDB(t)
	summary := &customVendorChangeSummary{}
	full := make(map[string]customVendorPrice)
	for i := 0; i < 12; i++ {
		full[fmt.Sprintf("model-%d", i)] = customVendorTestPrice(1, 2)
	}
	require.NoError(t, applyCustomVendorSource("xai", full, false, 100, summary))

	err := applyCustomVendorSource("xai", map[string]customVendorPrice{"model-0": customVendorTestPrice(1, 2)}, false, 200, summary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "suspect")
	var rows int64
	require.NoError(t, db.Model(&model.CustomVendorPrice{}).Where("source = ?", "xai").Count(&rows).Error)
	assert.EqualValues(t, 12, rows, "good data is kept")

	require.NoError(t, applyCustomVendorSource("xai", map[string]customVendorPrice{"model-0": customVendorTestPrice(1, 2)}, true, 300, summary))
}

func TestResolveCustomVendorPricesPrefersManualThenVendorSources(t *testing.T) {
	setupCustomVendorCatalogDB(t)
	now := int64(100)
	for _, entry := range []struct {
		source string
		price  customVendorPrice
	}{
		{"kimi", customVendorTestPrice(0.615, 2.46)},
		{"z.ai", customVendorTestPrice(1.4, 4.4)},
	} {
		row := customVendorRowFromPrice("glm-5.1", entry.source, entry.price, now)
		require.NoError(t, model.DB.Create(&row).Error)
	}
	prices, manual, err := resolveCustomVendorPrices()
	require.NoError(t, err)
	assert.InDelta(t, 1.4, prices["glm-5.1"].Input, 1e-9, "the higher-priority source wins")
	assert.False(t, manual["glm-5.1"])

	body := map[string]any{"model_name": "glm-5.1", "input": 1.2, "output": 4.0, "note": "confirmed"}
	var response struct{ Success bool }
	modelManagementRequest(t, PutCustomVendorManualPrice, http.MethodPut, "/api/ratio_sync/vendor_prices/manual", body, &response)
	require.True(t, response.Success)
	prices, manual, err = resolveCustomVendorPrices()
	require.NoError(t, err)
	assert.InDelta(t, 1.2, prices["glm-5.1"].Input, 1e-9, "a manual entry wins")
	assert.True(t, manual["glm-5.1"])

	// A refresh never touches the manual entry.
	require.NoError(t, applyCustomVendorSource("z.ai", map[string]customVendorPrice{"glm-5.1": customVendorTestPrice(1.4, 4.4)}, false, 200, &customVendorChangeSummary{}))
	prices, _, err = resolveCustomVendorPrices()
	require.NoError(t, err)
	assert.InDelta(t, 1.2, prices["glm-5.1"].Input, 1e-9)

	bad := map[string]any{"model_name": "glm-5.1", "input": 0}
	modelManagementRequest(t, PutCustomVendorManualPrice, http.MethodPut, "/api/ratio_sync/vendor_prices/manual", bad, &response)
	assert.False(t, response.Success, "a zero input price is rejected")
}

// The vendor list prices are a price-sync source: a manual entry wins over the
// fetched one, input-length tiers become branches, Claude models carry
// Anthropic's cache-write multiples, and prices that cannot be charged exactly
// (no output price, a DeepSeek entry with the peak price alone) are left out.
func TestCustomVendorPricesServeThePriceSync(t *testing.T) {
	db := setupCustomVendorCatalogDB(t)
	_, handled, err := fetchCustomPricingSync(t.Context(), nil, dto.UpstreamDTO{ID: customVendorPresetID, Name: customVendorPresetName, BaseURL: customVendorPresetBaseURL})
	require.True(t, handled)
	require.ErrorContains(t, err, "catalog is empty")

	// The built-in price of a model no saved price overrides.
	builtinDeepSeekPro, ok := billing_setting.GetBillingExpr("deepseek-v4-pro")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(builtinDeepSeekPro, customVendorDeepSeekPeak), "the peak window follows the built-in DeepSeek expressions")

	out := func(value float64) *float64 { return &value }
	rows := map[string]customVendorPrice{
		"glm-5.1":         {Provider: "z.ai pricing page", Input: 1.4, Output: out(4.4), CacheRead: out(0.26)},
		"kimi-k3":         {Provider: "Kimi pricing page", Input: 3, Output: out(15)},
		"claude-sonnet-5": {Provider: "Anthropic pricing page", Input: 3, Output: out(15), CacheRead: out(0.3)},
		"grok-4.7": {Provider: "x.ai pricing page", Input: 2, Output: out(6), CacheRead: out(0.5), Tiers: []customVendorTier{
			{Upper: 200000, Input: 2, Output: out(6), CacheRead: out(0.5)},
			{Input: 4, Output: out(12), CacheRead: out(1)},
		}},
		"text-embedding-x": {Provider: "z.ai pricing page", Input: 0.1},
		// A DeepSeek entry with the peak price alone, as stored before both bands were read.
		"deepseek-flash": {Provider: customVendorDeepSeekProvider + " (peak)", Input: 0.3, Output: out(1.2)},
		"deepseek-v4-pro": {Provider: customVendorDeepSeekProvider, Input: 1.32, Output: out(3.96), CacheRead: out(0.044), Tiers: []customVendorTier{
			{Band: "peak", Input: 1.32, Output: out(3.96), CacheRead: out(0.044)},
			{Band: "off_peak", Input: 0.66, Output: out(1.98), CacheRead: out(0.022)},
		}},
		"gpt-6.1-sol": {Provider: customVendorOpenAIProvider, Input: 2, Output: out(10), CacheRead: out(0.1), Tiers: []customVendorTier{
			{Upper: 272000, Input: 2, Output: out(10), CacheRead: out(0.1), CacheWrite: out(2.5)},
			{Input: 4, Output: out(15), CacheRead: out(0.2), CacheWrite: out(5)},
		}},
		"gemini-2.5-flash": {Provider: customVendorGoogleProvider, Input: 0.3, Output: out(2.5), CacheRead: out(0.03), Tiers: []customVendorTier{
			{Input: 0.3, Output: out(2.5), CacheRead: out(0.03), AudioInput: out(1)},
		}},
	}
	for name, price := range rows {
		row := customVendorRowFromPrice(name, "kimi", price, 100)
		require.NoError(t, db.Create(&row).Error)
	}
	manual := customVendorRowFromPrice("kimi-k3", customVendorManualSource, customVendorPrice{Provider: "manual", Input: 2.5, Output: out(12)}, 100)
	require.NoError(t, db.Create(&manual).Error)

	// ByteDance prices a model family once: a dated snapshot this gateway
	// serves takes its family's price, a snapshot of another vendor does not.
	family := customVendorRowFromPrice("doubao-seed-2-1-pro", "volcengine", customVendorPrice{Provider: customVendorVolcengineProvider + " (CNY at 7.3 per USD)", Input: 0.82, Output: out(4.1)}, 100)
	require.NoError(t, db.Create(&family).Error)
	for _, served := range []string{"doubao-seed-2-1-pro-260715", "glm-5.1-260101"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: served, ChannelId: 1, Enabled: true}).Error)
	}
	resolved, _, err := resolveCustomVendorPrices()
	require.NoError(t, err)
	assert.Equal(t, resolved["doubao-seed-2-1-pro"], resolved["doubao-seed-2-1-pro-260715"])
	assert.NotContains(t, resolved, "glm-5.1-260101")

	data, handled, err := fetchCustomPricingSync(t.Context(), nil, dto.UpstreamDTO{ID: customVendorPresetID})
	require.True(t, handled)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"glm-5.1":         `tier("base", p * 1.4 + c * 4.4 + cr * 0.26)`,
		"kimi-k3":         `tier("base", p * 2.5 + c * 12)`,
		"claude-sonnet-5": `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`,
		"grok-4.7":        `len <= 200000 ? tier("base", p * 2 + c * 6 + cr * 0.5) : tier("tier_2", p * 4 + c * 12 + cr * 1)`,
		// DeepSeek's two bands render as the built-in expression does.
		"deepseek-v4-pro":            builtinDeepSeekPro,
		"gpt-6.1-sol":                `len <= 272000 ? tier("base", p * 2 + c * 10 + cr * 0.1 + cc * 2.5) : tier("tier_2", p * 4 + c * 15 + cr * 0.2 + cc * 5)`,
		"gemini-2.5-flash":           `tier("base", p * 0.3 + c * 2.5 + cr * 0.03 + ai * 1)`,
		"doubao-seed-2-1-pro":        `tier("base", p * 0.82 + c * 4.1)`,
		"doubao-seed-2-1-pro-260715": `tier("base", p * 0.82 + c * 4.1)`,
	}, data[billing_setting.BillingExprField])
	modes := data[billing_setting.BillingModeField].(map[string]string)
	assert.Len(t, modes, 9)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, modes["grok-4.7"])

	// The daily check names the served models that charge less than listed:
	// an expression short on input, and a ratio price short on output.
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	for key, value := range map[string]string{
		"billing_setting.billing_mode": `{"kimi-k3":"tiered_expr","grok-4.7":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"kimi-k3":"tier(\"base\", p * 2 + c * 12)","grok-4.7":"len <= 200000 ? tier(\"a\", p * 2 + c * 6 + cr * 0.5) : tier(\"b\", p * 4 + c * 12 + cr * 1)"}`,
		"ModelRatio":                   `{"glm-5.1":0.5}`,
		"CompletionRatio":              `{"glm-5.1":3}`,
	} {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	for _, served := range []string{"kimi-k3", "grok-4.7", "glm-5.1"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: served, ChannelId: 1, Enabled: true}).Error)
	}
	below, err := customVendorUnderpriced()
	require.NoError(t, err)
	assert.Equal(t, []larknotify.VendorPriceBelow{
		{Model: "glm-5.1", What: "output", Charged: 3, Listed: 4.4},
		{Model: "kimi-k3", What: "input", Charged: 2, Listed: 2.5},
	}, below)
}

// One refresh reports to Lark what a person has to know: a model the vendor
// lists for the first time, a price held for review, and a source that stops
// being readable. The first fill of a source and a source that stays broken
// are not reported again.
func TestCustomVendorRefreshReportsNewModelsAndBrokenSources(t *testing.T) {
	setupCustomVendorCatalogDB(t)
	output := func(value float64) *float64 { return &value }
	tiered := customVendorPrice{Provider: "test", Input: 2, Output: output(10), Tiers: []customVendorTier{
		{Upper: 200000, Input: 2, Output: output(10)},
		{Input: 4, Output: output(15)},
	}}
	first, err := storeCustomVendorResults([]customVendorSourceResult{
		{Name: "google", Prices: map[string]customVendorPrice{"gemini-a": customVendorTestPrice(1, 5)}},
		{Name: "kimi", Prices: map[string]customVendorPrice{"kimi-a": customVendorTestPrice(3, 15)}},
	}, false, 100)
	require.NoError(t, err)
	assert.Empty(t, first.Report.Changes, "the first fill is not a change")
	assert.Empty(t, first.Report.Failures)

	second, err := storeCustomVendorResults([]customVendorSourceResult{
		{Name: "google", Prices: map[string]customVendorPrice{"gemini-a": customVendorTestPrice(1, 5), "gemini-b": tiered}},
		{Name: "kimi", Err: fmt.Errorf("503 Service Unavailable")},
		{Name: "openai", Err: fmt.Errorf("403 Forbidden")},
	}, false, 200)
	require.NoError(t, err)
	require.Len(t, second.Report.Changes, 1)
	listed := second.Report.Changes[0]
	assert.Equal(t, larknotify.VendorPriceNew, listed.Kind)
	assert.Equal(t, "gemini-b", listed.Model)
	assert.Empty(t, listed.Old)
	require.Len(t, listed.New, 2)
	assert.Equal(t, "≤200k", listed.New[0].Range)
	assert.Equal(t, ">200k", listed.New[1].Range)
	assert.Equal(t, []larknotify.VendorPriceFailure{
		{Source: "kimi", Message: "503 Service Unavailable"},
		{Source: "openai", Message: "403 Forbidden"},
	}, second.Report.Failures, "a source that could be read before, and one failing on its first refresh")

	third, err := storeCustomVendorResults([]customVendorSourceResult{
		{Name: "kimi", Err: fmt.Errorf("503 Service Unavailable")},
	}, false, 300)
	require.NoError(t, err)
	assert.Empty(t, third.Report.Failures, "a source that stays broken is not reported again")
}

func TestCustomChargesBelowVendorListPrice(t *testing.T) {
	seedance, served := customTaskUsageSchema("doubao-seedance-2-5-260628")
	require.True(t, served)
	listed := `u("resolution") == "480p" && u("video_input") == "none" ? tier("480p·none", u("tokens") * 10.7 / 1000000) : ` +
		`u("resolution") == "480p" && u("video_input") == "video" ? tier("480p·video", u("tokens") * 6.4 / 1000000) : ` +
		`u("resolution") == "720p" && u("video_input") == "none" ? tier("720p·none", u("tokens") * 10.7 / 1000000) : ` +
		`u("resolution") == "720p" && u("video_input") == "video" ? tier("720p·video", u("tokens") * 6.4 / 1000000) : ` +
		`u("resolution") == "1080p" && u("video_input") == "none" ? tier("1080p·none", u("tokens") * 11.7 / 1000000) : tier("1080p·video", u("tokens") * 7 / 1000000)`
	// The same prices written by hand, with the cheapest tier as the fallback.
	byHand := `u("video_input") == "video" && u("resolution") == "1080p" ? tier("v2v_1080p", u("tokens") * 7 / 1000000) : ` +
		`u("video_input") == "video" ? tier("v2v", u("tokens") * 6.4 / 1000000) : ` +
		`u("resolution") == "1080p" ? tier("1080p", u("tokens") * 11.7 / 1000000) : tier("base", u("tokens") * 10.7 / 1000000)`
	tieredQwen := `len <= 32000 ? tier("base", p * 0.03 + c * 0.13) : tier("tier_2", p * 0.1 + c * 0.4)`
	cases := []struct {
		name            string
		current, vendor string
		what            string
		length          int64
		charged, listed float64
	}{
		{name: "the same task prices in another shape", current: byHand, vendor: listed},
		{name: "one task tier short", current: strings.Replace(byHand, "* 7 /", "* 6.4 /", 1), vendor: listed, what: "1080p · video · tokens", charged: 6.4, listed: 7},
		{name: "a rounded conversion", current: `tier("base", p * 10.494753 + c * 20)`, vendor: `tier("base", p * 10.4948 + c * 20)`},
		{name: "cache writes charged as input", current: `tier("base", p * 2 + c * 6)`, vendor: `tier("base", p * 2 + c * 6 + cr * 0.4 + cc * 2.5)`, what: "cache write", charged: 2, listed: 2.5},
		{name: "one price where the vendor has two", current: `tier("base", p * 0.03 + c * 0.13)`, vendor: tieredQwen, what: "input", length: 32001, charged: 0.03, listed: 0.1},
		{name: "a price per request against a token price", current: `tier("request", fixed(0.001))`, vendor: tieredQwen},
		{name: "a token price against a task price", current: `tier("base", p * 1 + c * 1)`, vendor: listed},
	}
	for _, c := range cases {
		schema := map[string]jsplugin.UsageFieldSchema(nil)
		if strings.Contains(c.vendor, `u("`) {
			schema = seedance
		}
		what, length, charged, vendor, below := customChargesBelow(c.current, c.vendor, schema)
		assert.Equal(t, c.what != "", below, c.name)
		assert.Equal(t, c.what, what, c.name)
		assert.Equal(t, c.length, length, c.name)
		assert.InDelta(t, c.charged, charged, 1e-9, c.name)
		assert.InDelta(t, c.listed, vendor, 1e-9, c.name)
	}

	// The CNY rate is raised once the market buys 1% less CNY per USD than it.
	assert.Nil(t, customVendorRateDrift(6.67, 6.6707, "2026-11-11"))
	assert.Nil(t, customVendorRateDrift(6.67, 6.62, "2026-11-11"))
	assert.Nil(t, customVendorRateDrift(0, 6.5, "2026-11-11"))
	assert.Equal(t, &larknotify.VendorRateDrift{Set: 6.67, Market: 6.5, Date: "2026-11-11"}, customVendorRateDrift(6.67, 6.5, "2026-11-11"))
}
