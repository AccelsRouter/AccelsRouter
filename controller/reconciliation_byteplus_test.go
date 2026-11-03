package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Split-bill rows become bill entries at their window start, with tokens
// classified by charge item (prompt / completion / KV-cache hit), the count
// scaled by its unit, the payable amount as cost, and non-token units kept
// as a usage amount.
func TestBytePlusBillEntries(t *testing.T) {
	rows := []bytePlusBillRow{
		{ExpenseTime: "2026-10-08 10:15:00-2026-10-08 10:20:00", ConfigName: "DeepSeek-V4.1-flash", ChargeItemCode: "DeepSeek_V4.1_flash_inference_peak_prompt_ap-southeast-1_realtime", Count: "0.072", Unit: "K tokens", PayableAmount: "0.00"},
		{ExpenseTime: "2026-10-08 10:15:00-2026-10-08 10:20:00", ConfigName: "DeepSeek-V4.1-flash", ChargeItemCode: "DeepSeek_V4.1_flash_inference_peak_completion_ap-southeast-1_realtime", Count: "2.858", Unit: "K tokens", PayableAmount: "0.01"},
		{ExpenseTime: "2026-10-09 12:55:00-2026-10-09 13:00:00", ConfigName: "DeepSeek-V4.1-flash", ChargeItemCode: "DeepSeek_V4.1_flash_inference_offpeak_KVcache_hit_ap-southeast-1_realtime", Count: "189.056", Unit: "K tokens", OriginalBillAmount: "0.0010", DiscountBillAmount: "0.0007", PayableAmount: "0.00"},
		{ExpenseTime: "2026-10-09 12:55:00-2026-10-09 13:00:00", ConfigName: "Seed-2.0-lite", ChargeItemCode: "Seed_2.0_lite_128k_infer_output_ap-southeast-1_realtime", Count: "1,234", Unit: "tokens", PayableAmount: "0.1"},
		{ExpenseTime: "2026-10-09 12:55:00-2026-10-09 13:00:00", ConfigName: "Seedream-4.0", ChargeItemCode: "Seedream_4.0_t2i", Count: "3", Unit: "images", PayableAmount: "0.09"},
		// Day 20-29: the start stamp itself contains "-20".
		{ExpenseTime: "2026-10-20 16:10:00-2026-10-20 16:15:00", ConfigName: "DeepSeek-V4.1-flash", ChargeItemCode: "DeepSeek_V4.1_flash_inference_peak_prompt_ap-southeast-1_realtime", Count: "1", Unit: "K tokens", PayableAmount: "0.001"},
		// Not model usage: a resource-pack purchase and object storage.
		{ExpenseTime: "2026-10-01 00:00:00-2026-10-01 00:05:00", BillCategory: "Consumption-purchase", Product: "ModelArk_resource_packages", ConfigName: "Seed-2.0-pro-free-inference-res-pack", Count: "500000", Unit: "tokens", PayableAmount: "0.00"},
		{ExpenseTime: "2026-10-01 00:00:00-2026-10-01 00:05:00", BillCategory: "Consumption-usage", Product: "TOS", ConfigName: "TOS", ChargeItemCode: "standard_storage_ap-southeast-1", Count: "0.046", Unit: "GiB", PayableAmount: "0.00"},
	}
	entries, err := bytePlusBillEntries(rows)
	require.NoError(t, err)
	w1 := time.Date(2026, 10, 8, 10, 15, 0, 0, time.UTC).Unix()
	w2 := time.Date(2026, 10, 9, 12, 55, 0, 0, time.UTC).Unix()
	w3 := time.Date(2026, 10, 20, 16, 10, 0, 0, time.UTC).Unix()
	assert.Equal(t, []model.UpstreamBillEntry{
		{Wall: w1, Model: "DeepSeek-V4.1-flash", PromptTokens: 72, Cost: 0},
		{Wall: w1, Model: "DeepSeek-V4.1-flash", CompletionTokens: 2858, Cost: 0, Payable: 0.01},
		{Wall: w2, Model: "DeepSeek-V4.1-flash", PromptTokens: 189056, CacheReadTokens: 189056, Cost: 0.0007},
		{Wall: w2, Model: "Seed-2.0-lite", CompletionTokens: 1234, Cost: 0, Payable: 0.1},
		{Wall: w2, Model: "Seedream-4.0", UsageUnit: "images", UsageAmount: 3, Cost: 0, Payable: 0.09},
		{Wall: w3, Model: "DeepSeek-V4.1-flash", PromptTokens: 1000, Cost: 0, Payable: 0.001},
	}, entries)

	_, err = bytePlusBillEntries([]bytePlusBillRow{{ExpenseTime: "-", ConfigName: "x", Count: "1", Unit: "K tokens", PayableAmount: "0"}})
	require.Error(t, err)
	_, err = bytePlusBillEntries(nil)
	require.Error(t, err)

	assert.Equal(t, "deepseek-v4-1-flash", bytePlusModelKey("DeepSeek-V4.1-flash"))
	assert.Equal(t, "glm-5-2", bytePlusModelKey("GLM_5.2"))
}

// The bill names the product; our endpoint model ids carry a "-YYMMDD"
// version that the key drops, so both sides meet on one row.
func TestBytePlusModelKey(t *testing.T) {
	assert.Equal(t, "deepseek-v4-1-flash", bytePlusModelKey("DeepSeek-V4.1-flash"))
	assert.Equal(t, "deepseek-v4-1-flash", bytePlusModelKey("deepseek-v4-1-flash-261017"))
	assert.Equal(t, "dola-seed-2-1-turbo", bytePlusModelKey("dola-seed-2-1-turbo-260628"))
	assert.Equal(t, "deepseek-v4-flash-0731", bytePlusModelKey("DeepSeek-V4-flash-0731"))
	assert.Equal(t, "glm-5-2", bytePlusModelKey(" GLM_5.2 "))
}

// The split-bill query asks for 5-minute settlement rows; with a split item
// it also asks the bill to split by API key and filters to that key.
func TestBytePlusSplitBillBody(t *testing.T) {
	body := bytePlusSplitBillBody("2026-10", 2, "")
	assert.Equal(t, map[string]any{"BillPeriod": "2026-10", "Limit": 300, "Offset": 600, "GroupTerm": 1, "GroupPeriod": 2, "NeedRecordNum": 1}, body)

	body = bytePlusSplitBillBody("2026-10", 0, "apikey-20261019133633-rp9x5")
	assert.Equal(t, "apikey-20261019133633-rp9x5", body["SplitItemID"])
	var dims map[string]string
	require.NoError(t, common.Unmarshal([]byte(body["SplitDimension"].(string)), &dims))
	assert.Equal(t, "apikey_id", dims["ModelArk"])
	assert.Equal(t, "apikey_id", dims["ModelArk_video_generation"])
}
