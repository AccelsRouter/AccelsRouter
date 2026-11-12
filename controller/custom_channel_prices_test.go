package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The daily record keeps what a channel charges for the models it serves, a
// new row only when a price changes, and the bill comparison prices our usage
// with the price in force in each hour. A task plugin's own price at the
// upstream is the one recorded, not the model's.
func TestCustomChannelPricesAreRecordedAndPriceTheBill(t *testing.T) {
	db := setupCustomVendorCatalogDB(t)
	require.NoError(t, db.AutoMigrate(&model.CustomChannelPrice{}, &model.QuotaData{}))
	video := `u("resolution") == "480P" ? tier("480P", u("seconds") * 0.05) : u("resolution") == "720P" ? tier("720P", u("seconds") * 0.1) : tier("1080P", u("seconds") * 0.2)`
	chatRatio := 1.0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, err := common.Marshal(map[string]any{"success": true, "data": []map[string]any{
			{"model_name": "up-chat", "model_ratio": chatRatio, "completion_ratio": 4, "cache_ratio": 0.1},
			{"model_name": "wan3.0-video", "billing_mode": "tiered_expr", "billing_expr": `tier("base", u("seconds") * 0)`,
				"billing_plugin_variants": []map[string]any{{"plugin_key": "alibaba", "billing_mode": "tiered_expr", "billing_expr": video}}},
			{"model_name": "not-served", "model_ratio": 9},
		}})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()
	mapping := `{"our-chat":"up-chat"}`
	require.NoError(t, db.Create(&model.Channel{
		Id: 7, Name: "gateway", Type: 1, Key: "k", Status: common.ChannelStatusEnabled, BaseURL: &server.URL,
		Models: "our-chat,wan3.0-video", ModelMapping: &mapping,
	}).Error)

	changed, err := recordCustomChannelPrices(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, changed, "the two models the channel serves, under the upstream's names")
	changed, err = recordCustomChannelPrices(t.Context())
	require.NoError(t, err)
	assert.Zero(t, changed, "an unchanged price adds nothing")

	// The first prices took effect at 05:00; the chat price doubles at 07:00.
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, db.Model(&model.CustomChannelPrice{}).Where("channel_id = ?", 7).Update("effective_at", day+5*3600).Error)
	chatRatio = 2
	changed, err = recordCustomChannelPrices(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, changed)
	require.NoError(t, db.Model(&model.CustomChannelPrice{}).Where("effective_at > ?", day+5*3600).Update("effective_at", day+7*3600).Error)
	var rows []model.CustomChannelPrice
	require.NoError(t, db.Order("effective_at, model_name").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"up-chat", "wan3.0-video", "up-chat"}, []string{rows[0].ModelName, rows[1].ModelName, rows[2].ModelName})
	assert.Contains(t, rows[1].Pricing, `seconds\") * 0.2`, "the plugin's price, not the model's")

	// The history is served newest first, for one channel or all of them.
	var listed struct {
		Success bool
		Data    []model.CustomChannelPrice
	}
	modelManagementRequest(t, ListCustomChannelPrices, http.MethodGet, "/api/ratio_sync/channel_prices?channel_id=7", nil, &listed)
	require.True(t, listed.Success)
	require.Len(t, listed.Data, 3)
	assert.Equal(t, "up-chat", listed.Data[0].ModelName)
	assert.Equal(t, day+7*3600, listed.Data[0].EffectiveAt)
	modelManagementRequest(t, ListCustomChannelPrices, http.MethodGet, "/api/ratio_sync/channel_prices?channel_id=8", nil, &listed)
	assert.Empty(t, listed.Data)

	require.NoError(t, db.Create(&[]model.QuotaData{
		// Before any price was recorded: priced at the first record, and said so.
		{ModelName: "our-chat", CreatedAt: day + 4*3600, ChannelID: 7, Count: 1, Quota: 500, TokenUsed: 1000000, PromptTokens: 1000000},
		{ModelName: "our-chat", CreatedAt: day + 6*3600, ChannelID: 7, Count: 2, Quota: 2000, TokenUsed: 1100000, PromptTokens: 1000000, CompletionTokens: 100000, CacheReadTokens: 200000},
		{ModelName: "our-chat", CreatedAt: day + 8*3600, ChannelID: 7, Count: 1, Quota: 3000, TokenUsed: 1000000, PromptTokens: 1000000},
		{ModelName: "wan3.0-video", CreatedAt: day + 6*3600, ChannelID: 7, Count: 2, Quota: 900, UsageUnit: model.UsageUnitSecond, UsageAmount: 10},
		{ModelName: "unrecorded", CreatedAt: day + 6*3600, ChannelID: 7, Count: 1, Quota: 5, TokenUsed: 10, PromptTokens: 10},
	}).Error)
	utc := 0
	report, err := model.CompareUpstreamBill(7, []model.UpstreamBillEntry{
		{Wall: day + 4*3600, Model: "up-chat", Requests: 1, PromptTokens: 1000000, Cost: 2},
		{Wall: day + 8*3600, Model: "up-chat", Requests: 1, PromptTokens: 1000000, Cost: 4},
	}, &utc, 0, nil)
	require.NoError(t, err)
	expected := make(map[string]*model.UpstreamBillExpected)
	for _, row := range report.Rows {
		expected[row.Model] = row.Expected
	}
	// 04:00 at ratio 1: 1M input at $2. 06:00: 0.8M input, 0.2M cached at a
	// tenth, 0.1M output at four times. 08:00 at ratio 2: 1M input at $4.
	require.NotNil(t, expected["up-chat"])
	assert.InDelta(t, 2+1.6+0.04+0.8+4, expected["up-chat"].Lowest, 1e-9)
	assert.InDelta(t, expected["up-chat"].Lowest, expected["up-chat"].Highest, 1e-9)
	assert.True(t, expected["up-chat"].LaterPrice)
	assert.Len(t, expected["up-chat"].Prices, 2)
	// Ten seconds of video at an unrecorded resolution: 480P at the least, 1080P at the most.
	require.NotNil(t, expected["wan3.0-video"])
	assert.InDelta(t, 0.5, expected["wan3.0-video"].Lowest, 1e-9)
	assert.InDelta(t, 2, expected["wan3.0-video"].Highest, 1e-9)
	assert.False(t, expected["wan3.0-video"].LaterPrice)
	assert.Nil(t, expected["unrecorded"], "a model the channel has no recorded price for")
	require.NotNil(t, report.Expected)
	assert.InDelta(t, 8.44+0.5, report.Expected.Lowest, 1e-9)
	assert.InDelta(t, 8.44+2, report.Expected.Highest, 1e-9)
}
