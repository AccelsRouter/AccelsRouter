package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/model"
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
// (no output price, DeepSeek's peak-only entry) are left out.
func TestCustomVendorPricesServeThePriceSync(t *testing.T) {
	db := setupCustomVendorCatalogDB(t)
	_, handled, err := fetchCustomPricingSync(t.Context(), nil, dto.UpstreamDTO{ID: customVendorPresetID, Name: customVendorPresetName, BaseURL: customVendorPresetBaseURL})
	require.True(t, handled)
	require.ErrorContains(t, err, "catalog is empty")

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
		"deepseek-v4-pro":  {Provider: customVendorDeepSeekProvider, Input: 1.32, Output: out(3.96)},
	}
	for name, price := range rows {
		row := customVendorRowFromPrice(name, "kimi", price, 100)
		require.NoError(t, db.Create(&row).Error)
	}
	manual := customVendorRowFromPrice("kimi-k3", customVendorManualSource, customVendorPrice{Provider: "manual", Input: 2.5, Output: out(12)}, 100)
	require.NoError(t, db.Create(&manual).Error)

	data, handled, err := fetchCustomPricingSync(t.Context(), nil, dto.UpstreamDTO{ID: customVendorPresetID})
	require.True(t, handled)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"glm-5.1":         `tier("base", p * 1.4 + c * 4.4 + cr * 0.26)`,
		"kimi-k3":         `tier("base", p * 2.5 + c * 12)`,
		"claude-sonnet-5": `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`,
		"grok-4.7":        `len <= 200000 ? tier("base", p * 2 + c * 6 + cr * 0.5) : tier("tier_2", p * 4 + c * 12 + cr * 1)`,
	}, data[billing_setting.BillingExprField])
	modes := data[billing_setting.BillingModeField].(map[string]string)
	assert.Len(t, modes, 4)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, modes["grok-4.7"])
}
