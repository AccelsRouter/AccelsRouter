package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reconciliation contract: standard consumption comes from quota_data,
// the discount is the WHOLESALE amount the platform gave resellers (standard −
// what the reseller wallet paid), and standard = discount + received. The
// series is bucketed by calendar day in the admin's zone even though
// quota_data rows are hour buckets.
func TestGetReconciliationReceivedAndDayBuckets(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&QuotaData{}, &OrgUsageDaily{}))
	clear := func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM org_usage_daily")
	}
	clear()
	t.Cleanup(clear)

	cst := time.FixedZone("cst", 8*3600)
	// 2026-09-30 23:00 CST and 2026-10-01 01:00 CST: same UTC day, different CST days.
	h1 := time.Date(2026, 9, 30, 23, 0, 0, 0, cst).Unix()
	h2 := time.Date(2026, 10, 1, 1, 0, 0, 0, cst).Unix()
	require.NoError(t, DB.Create(&[]QuotaData{
		{UserID: 1, Username: "a", ModelName: "m", CreatedAt: h1, ChannelID: 7, Count: 2, Quota: 1000, TokenUsed: 10, PromptTokens: 8, CompletionTokens: 2, CacheReadTokens: 3},
		{UserID: 2, Username: "b", ModelName: "m", CreatedAt: h2, ChannelID: 0, Count: 1, Quota: 500, TokenUsed: 5, PromptTokens: 4, CompletionTokens: 1},
		// A task-billed model on the same channel: counted in requests, no tokens,
		// its usage in seconds; a mixed-unit model keeps the unit of its larger part.
		{UserID: 1, Username: "a", ModelName: "video", CreatedAt: h2, ChannelID: 7, Count: 3, Quota: 200, TokenUsed: 0, UsageUnit: UsageUnitSecond, UsageAmount: 15},
		{UserID: 1, Username: "a", ModelName: "video", CreatedAt: h2, ChannelID: 7, Count: 1, Quota: 50, TokenUsed: 0, UsageUnit: UsageUnitImage, UsageAmount: 4},
	}).Error)
	// Reseller traffic: standard 600, customers paid retail 540, platform got wholesale 480.
	require.NoError(t, DB.Create(&OrgUsageDaily{
		DayBucket: h1, OrgId: 20, ResellerOrgId: 10, ModelName: "m", UserId: 1,
		StandardQuota: 600, ChargedQuota: 540, CostQuota: 480, Requests: 2,
	}).Error)

	rep, err := GetReconciliation(h1-1, h2+1, "day", cst)
	require.NoError(t, err)

	assert.Equal(t, int64(1750), rep.Summary.StandardQuota)
	assert.Equal(t, int64(120), rep.Summary.DiscountQuota, "wholesale discount = standard - cost")
	assert.Equal(t, int64(1630), rep.Summary.ReceivedQuota)
	assert.Equal(t, rep.Summary.StandardQuota, rep.Summary.DiscountQuota+rep.Summary.ReceivedQuota)
	assert.Equal(t, int64(7), rep.Summary.Requests)
	assert.Equal(t, int64(1), rep.Summary.Channels, "channel_id 0 is not a channel")

	// Every (model, channel) pair, largest first; an unknown channel is "#<id>".
	assert.Equal(t, []ReconMatrixRow{
		{Model: "m", ChannelId: 7, Channel: "#7", Quota: 1000, Requests: 2, Tokens: 10, PromptTokens: 8, CompletionTokens: 2, CacheReadTokens: 3},
		{Model: "m", ChannelId: 0, Channel: "#0", Quota: 500, Requests: 1, Tokens: 5, PromptTokens: 4, CompletionTokens: 1},
		{Model: "video", ChannelId: 7, Channel: "#7", Quota: 250, Requests: 4, Tokens: 0, UsageUnit: UsageUnitSecond, UsageAmount: 15},
	}, rep.ByModelChannel)
	require.Len(t, rep.ByModel, 2)
	assert.Equal(t, ReconRow{Key: "video", Quota: 250, Requests: 4, Tokens: 0, UsageUnit: UsageUnitSecond, UsageAmount: 15}, rep.ByModel[1])
	require.Len(t, rep.ByChannel, 2)
	// Token rows dominate the channel: no unit, tokens apply.
	assert.Equal(t, ReconRow{Key: "#7", Quota: 1250, Requests: 6, Tokens: 10, PromptTokens: 8, CompletionTokens: 2, CacheReadTokens: 3, UsageUnit: UsageUnitSecond, UsageAmount: 15}, rep.ByChannel[0])

	require.Len(t, rep.ByReseller, 1)
	assert.Equal(t, ReconResellerRow{OrgId: 10, Name: "#10", StandardQuota: 600, DiscountQuota: 120, ReceivedQuota: 480, ChargedQuota: 540, Requests: 2}, rep.ByReseller[0])

	require.Len(t, rep.Series, 2, "one point per CST calendar day")
	assert.Equal(t, time.Date(2026, 9, 30, 0, 0, 0, 0, cst).Unix(), rep.Series[0].Period)
	assert.Equal(t, int64(1000), rep.Series[0].Quota)
	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, cst).Unix(), rep.Series[1].Period)
	assert.Equal(t, int64(750), rep.Series[1].Quota)

	utc, err := GetReconciliation(h1-1, h2+1, "day", time.UTC)
	require.NoError(t, err)
	require.Len(t, utc.Series, 1, "both hours fall on the same UTC day")
	assert.Equal(t, int64(1750), utc.Series[0].Quota)
}
