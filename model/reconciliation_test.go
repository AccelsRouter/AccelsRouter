package model

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
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

// An exported bill is recognised by its header row wherever that sits (a
// workbook's summary sheets come first), token and cost columns are optional,
// and the rows are compared with the channel's quota_data per upstream model
// name: our rows pass through the channel's model mapping, and the bill's
// zone is detected from the hour buckets its requests line up with.
func TestUpstreamBillParseAndCompare(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&QuotaData{}, &Channel{}))
	clear := func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM channels WHERE id IN (9, 10)")
	}
	clear()
	t.Cleanup(clear)

	wb := excelize.NewFile()
	require.NoError(t, wb.SetSheetRow("Sheet1", "A1", &[]any{"项目", "值"}))
	require.NoError(t, wb.SetSheetRow("Sheet1", "A2", &[]any{"费用(CNY)", 1.5}))
	_, err := wb.NewSheet("账单明细")
	require.NoError(t, err)
	require.NoError(t, wb.SetSheetRow("账单明细", "A1", &[]any{"时间", "用户名", "令牌名", "模型名", "输入Token", "输出Token", "缓存读取", "缓存写入", "原价(CNY)", "折扣", "折后价(CNY)"}))
	require.NoError(t, wb.SetSheetRow("账单明细", "A2", &[]any{"2026-10-12 14:10:00", "u", "k", "claude-sonnet-4-6", 1000, 20, 0, 0, 0.5, nil, 0.4}))
	require.NoError(t, wb.SetSheetRow("账单明细", "A3", &[]any{"2026-10-12 14:20:00", "u", "k", "claude-sonnet-4-6", 2000, 40, 100, 0, 0.6, nil, 0.6}))
	require.NoError(t, wb.SetSheetRow("账单明细", "A4", &[]any{"2026-10-12 14:30:00", "u", "k", "claude-sonnet-4-6", 3000, 60, 0, 0, 0.7, nil, 0.7}))
	require.NoError(t, wb.SetSheetRow("账单明细", "A5", &[]any{"2026-10-13 09:00:00", "u", "k", "doubao-seedance-2-5-260628", 0, 0, 0, 0, 60, nil, 60}))
	var buf bytes.Buffer
	require.NoError(t, wb.Write(&buf))
	entries, err := ParseUpstreamBill("bill.xlsx", &buf)
	require.NoError(t, err)
	require.Len(t, entries, 4)
	wall := time.Date(2026, 10, 12, 14, 20, 0, 0, time.UTC).Unix()
	assert.Equal(t, UpstreamBillEntry{Wall: wall, Model: "claude-sonnet-4-6", PromptTokens: 2000, CompletionTokens: 40, CacheReadTokens: 100, Cost: 0.6, Requests: 1}, entries[1])

	csvEntries, err := ParseUpstreamBill("bill.csv", strings.NewReader("\ufeff时间,模型名,费用\n2026-10-12 14:10,m,\"1,000.5\"\n合计,,1000.5\n"))
	require.NoError(t, err)
	assert.Equal(t, []UpstreamBillEntry{{Wall: time.Date(2026, 10, 12, 14, 10, 0, 0, time.UTC).Unix(), Model: "m", Cost: 1000.5, Requests: 1}}, csvEntries)
	_, err = ParseUpstreamBill("x.csv", strings.NewReader("a,b\n1,2\n"))
	assert.ErrorContains(t, err, "unrecognized bill format")

	mapping := `{"sonnet":"claude-sonnet-4-6"}`
	require.NoError(t, DB.Create(&[]Channel{{Id: 9, Name: "aigc", ModelMapping: &mapping}, {Id: 10, Name: "other"}}).Error)
	// 14:00 on the bill is 06:00 UTC when the bill is UTC+8.
	h := time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, DB.Create(&[]QuotaData{
		{UserID: 1, Username: "a", ModelName: "sonnet", CreatedAt: h, ChannelID: 9, Count: 3, Quota: 900, TokenUsed: 6120, PromptTokens: 6000, CompletionTokens: 120, CacheReadTokens: 100},
		// Same hour on another channel, and the hour before on ours: neither is the bill's.
		{UserID: 1, Username: "a", ModelName: "sonnet", CreatedAt: h, ChannelID: 10, Count: 5, Quota: 1, TokenUsed: 1},
		{UserID: 1, Username: "a", ModelName: "sonnet", CreatedAt: h - 3600, ChannelID: 9, Count: 7, Quota: 1, TokenUsed: 1},
		// A model we served that the bill does not list.
		{UserID: 1, Username: "a", ModelName: "gpt-4o", CreatedAt: h, ChannelID: 9, Count: 1, Quota: 10, TokenUsed: 5},
	}).Error)

	rep, err := CompareUpstreamBill(9, entries, nil, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, "aigc", rep.Channel)
	assert.True(t, rep.OffsetDetected)
	assert.Equal(t, 480, rep.OffsetMinutes)
	assert.Equal(t, int64(3), rep.MatchedRequests)
	assert.Equal(t, h, rep.From)
	assert.Equal(t, time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC).Unix(), rep.To)
	require.Len(t, rep.Rows, 3)
	assert.Equal(t, UpstreamBillRow{Model: "doubao-seedance-2-5-260628", Bill: UpstreamBillSide{Requests: 1, Cost: 60, Rows: 1}}, rep.Rows[0])
	assert.Equal(t, UpstreamBillRow{
		Model: "claude-sonnet-4-6",
		Ours:  UpstreamBillSide{Requests: 3, Tokens: 6120, PromptTokens: 6000, CompletionTokens: 120, CacheReadTokens: 100, Quota: 900},
		Bill:  UpstreamBillSide{Requests: 3, Tokens: 6120, PromptTokens: 6000, CompletionTokens: 120, CacheReadTokens: 100, Cost: 1.7, Rows: 3},
	}, rep.Rows[1])
	assert.Equal(t, UpstreamBillRow{Model: "gpt-4o", Ours: UpstreamBillSide{Requests: 1, Tokens: 5, Quota: 10}}, rep.Rows[2])
	assert.Equal(t, int64(4), rep.Ours.Requests)
	assert.Equal(t, int64(4), rep.Bill.Requests)
	assert.InDelta(t, 61.7, rep.Bill.Cost, 1e-9)

	// A pinned zone is used as given even when nothing lines up.
	pinned := 0
	rep, err = CompareUpstreamBill(9, entries, &pinned, 480, nil)
	require.NoError(t, err)
	assert.False(t, rep.OffsetDetected)
	assert.Equal(t, 0, rep.OffsetMinutes)
	assert.Equal(t, int64(0), rep.MatchedRequests)
	assert.Equal(t, int64(0), rep.Ours.Requests)
}

// An API bill arrives as settlement windows: entries carry no call count, so
// the bill's requests stay unknown while tokens and cost still compare, and
// model names are matched through the caller's normalizer.
func TestCompareUpstreamBillAggregatedEntries(t *testing.T) {
	DB.Exec("DELETE FROM channels WHERE id = 11")
	DB.Exec("DELETE FROM quota_data WHERE channel_id = 11")
	require.NoError(t, DB.Create(&Channel{Id: 11, Name: "bytepluses"}).Error)
	h := time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC).Unix() // 10:15 UTC+8
	require.NoError(t, DB.Create(&QuotaData{
		UserID: 1, Username: "a", ModelName: "deepseek-v4-1-flash", CreatedAt: h, ChannelID: 11,
		Count: 4, Quota: 50, TokenUsed: 3000, PromptTokens: 2000, CompletionTokens: 1000,
	}).Error)
	wall := time.Date(2026, 10, 8, 10, 15, 0, 0, time.UTC).Unix()
	entries := []UpstreamBillEntry{
		{Wall: wall, Model: "DeepSeek-V4.1-flash", PromptTokens: 2000, Cost: 0.001},
		{Wall: wall, Model: "DeepSeek-V4.1-flash", CompletionTokens: 1000, Cost: 0.002},
		{Wall: wall, Model: "Seedream-4.0", UsageUnit: "images", UsageAmount: 3, Cost: 0.09},
	}
	key := func(s string) string { return strings.ToLower(strings.NewReplacer(".", "-", "_", "-").Replace(s)) }
	rep, err := CompareUpstreamBill(11, entries, nil, 0, key)
	require.NoError(t, err)
	assert.False(t, rep.BillRequestsKnown)
	assert.True(t, rep.OffsetDetected)
	assert.Equal(t, 480, rep.OffsetMinutes, "settlement windows line up with our hour at UTC+8")
	require.Len(t, rep.Rows, 2)
	assert.Equal(t, UpstreamBillRow{
		Model: "deepseek-v4-1-flash",
		Ours:  UpstreamBillSide{Requests: 4, Tokens: 3000, PromptTokens: 2000, CompletionTokens: 1000, Quota: 50},
		Bill:  UpstreamBillSide{Tokens: 3000, PromptTokens: 2000, CompletionTokens: 1000, Cost: 0.003, Rows: 2},
	}, rep.Rows[1])
	assert.Equal(t, UpstreamBillRow{Model: "seedream-4-0", Bill: UpstreamBillSide{UsageUnit: "images", UsageAmount: 3, Cost: 0.09, Rows: 1}}, rep.Rows[0])
	assert.Equal(t, int64(0), rep.Bill.Requests)
}

// A daily bill row spans its whole day: with the zone pinned, our records
// from every hour of that day fall in the range, and the row matches when
// we saw the model in any of those hours.
func TestCompareUpstreamBillDailyRows(t *testing.T) {
	DB.Exec("DELETE FROM channels WHERE id = 12")
	DB.Exec("DELETE FROM quota_data WHERE channel_id = 12")
	require.NoError(t, DB.Create(&Channel{Id: 12, Name: "alibaba"}).Error)
	// 2026-10-08 23:00 UTC+8 = 15:00 UTC: inside the bill's day, outside it read as UTC.
	h := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, DB.Create(&QuotaData{
		UserID: 1, Username: "a", ModelName: "qwen-plus", CreatedAt: h, ChannelID: 12,
		Count: 2, Quota: 10, TokenUsed: 300, PromptTokens: 200, CompletionTokens: 100,
	}).Error)
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC).Unix()
	entries := []UpstreamBillEntry{
		{Wall: day, Span: 86400, Model: "qwen-plus", PromptTokens: 200, Cost: 0.001},
		{Wall: day, Span: 86400, Model: "qwen-plus", CompletionTokens: 100, Cost: 0.002},
	}
	pinned := 480
	rep, err := CompareUpstreamBill(12, entries, &pinned, 480, nil)
	require.NoError(t, err)
	assert.Equal(t, day-8*3600, rep.From)
	assert.Equal(t, day-8*3600+23*3600, rep.To)
	assert.Equal(t, int64(2), rep.MatchedRequests, "each daily row matches once")
	require.Len(t, rep.Rows, 1)
	assert.Equal(t, UpstreamBillRow{
		Model: "qwen-plus",
		Ours:  UpstreamBillSide{Requests: 2, Tokens: 300, PromptTokens: 200, CompletionTokens: 100, Quota: 10},
		Bill:  UpstreamBillSide{Tokens: 300, PromptTokens: 200, CompletionTokens: 100, Cost: 0.003, Rows: 2},
	}, rep.Rows[0])
}

// Request counts from an upstream's usage statistics land on the bill side
// of the matching rows (through the model key), a model only the usage saw
// becomes a row after the others, and the total follows.
func TestUpstreamBillReportSetBillRequests(t *testing.T) {
	report := &UpstreamBillReport{Rows: []UpstreamBillRow{
		{Model: "deepseek-v4-1-flash", Ours: UpstreamBillSide{Requests: 12}, Bill: UpstreamBillSide{Tokens: 500, Cost: 1}},
		{Model: "seed-2-0-lite", Ours: UpstreamBillSide{Requests: 3}},
	}, Ours: UpstreamBillSide{Requests: 15}}
	lower := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, ".", "-")) }
	report.SetBillRequests(map[string]int64{"DeepSeek-V4.1-flash": 7, "deepseek-v4-1-flash": 4, "Seed-2.0-lite": 0, "Seedream-4.0": 2}, lower)
	assert.True(t, report.BillRequestsFromUsage)
	assert.Equal(t, []UpstreamBillRow{
		{Model: "deepseek-v4-1-flash", Ours: UpstreamBillSide{Requests: 12}, Bill: UpstreamBillSide{Requests: 11, Tokens: 500, Cost: 1}},
		{Model: "seed-2-0-lite", Ours: UpstreamBillSide{Requests: 3}},
		{Model: "seedream-4-0", Bill: UpstreamBillSide{Requests: 2}},
	}, report.Rows)
	assert.Equal(t, int64(13), report.Bill.Requests)
	assert.Equal(t, int64(15), report.Ours.Requests)
}
