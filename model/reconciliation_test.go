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
	DB.Exec("DELETE FROM quota_data")
	DB.Exec("DELETE FROM org_usage_daily")

	cst := time.FixedZone("cst", 8*3600)
	// 2026-09-30 23:00 CST and 2026-10-01 01:00 CST: same UTC day, different CST days.
	h1 := time.Date(2026, 9, 30, 23, 0, 0, 0, cst).Unix()
	h2 := time.Date(2026, 10, 1, 1, 0, 0, 0, cst).Unix()
	require.NoError(t, DB.Create(&[]QuotaData{
		{UserID: 1, Username: "a", ModelName: "m", CreatedAt: h1, ChannelID: 7, Count: 2, Quota: 1000, TokenUsed: 10},
		{UserID: 2, Username: "b", ModelName: "m", CreatedAt: h2, ChannelID: 0, Count: 1, Quota: 500, TokenUsed: 5},
	}).Error)
	// Reseller traffic: standard 600, customers paid retail 540, platform got wholesale 480.
	require.NoError(t, DB.Create(&OrgUsageDaily{
		DayBucket: h1, OrgId: 20, ResellerOrgId: 10, ModelName: "m", UserId: 1,
		StandardQuota: 600, ChargedQuota: 540, CostQuota: 480, Requests: 2,
	}).Error)

	rep, err := GetReconciliation(h1-1, h2+1, "day", cst)
	require.NoError(t, err)

	assert.Equal(t, int64(1500), rep.Summary.StandardQuota)
	assert.Equal(t, int64(120), rep.Summary.DiscountQuota, "wholesale discount = standard - cost")
	assert.Equal(t, int64(1380), rep.Summary.ReceivedQuota)
	assert.Equal(t, rep.Summary.StandardQuota, rep.Summary.DiscountQuota+rep.Summary.ReceivedQuota)
	assert.Equal(t, int64(3), rep.Summary.Requests)
	assert.Equal(t, int64(1), rep.Summary.Channels, "channel_id 0 is not a channel")

	require.Len(t, rep.ByReseller, 1)
	assert.Equal(t, ReconResellerRow{OrgId: 10, Name: "#10", StandardQuota: 600, DiscountQuota: 120, ReceivedQuota: 480, ChargedQuota: 540, Requests: 2}, rep.ByReseller[0])

	require.Len(t, rep.Series, 2, "one point per CST calendar day")
	assert.Equal(t, time.Date(2026, 9, 30, 0, 0, 0, 0, cst).Unix(), rep.Series[0].Period)
	assert.Equal(t, int64(1000), rep.Series[0].Quota)
	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, cst).Unix(), rep.Series[1].Period)
	assert.Equal(t, int64(500), rep.Series[1].Quota)

	utc, err := GetReconciliation(h1-1, h2+1, "day", time.UTC)
	require.NoError(t, err)
	require.Len(t, utc.Series, 1, "both hours fall on the same UTC day")
	assert.Equal(t, int64(1500), utc.Series[0].Quota)
}
