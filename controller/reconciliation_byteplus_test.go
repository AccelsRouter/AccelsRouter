package controller

import (
	"testing"
	"time"

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
		{ExpenseTime: "2026-10-09 12:55:00-2026-10-09 13:00:00", ConfigName: "DeepSeek-V4.1-flash", ChargeItemCode: "DeepSeek_V4.1_flash_inference_offpeak_KVcache_hit_ap-southeast-1_realtime", Count: "189.056", Unit: "K tokens", PayableAmount: "0.0005"},
		{ExpenseTime: "2026-10-09 12:55:00-2026-10-09 13:00:00", ConfigName: "Seed-2.0-lite", ChargeItemCode: "Seed_2.0_lite_128k_infer_output_ap-southeast-1_realtime", Count: "1,234", Unit: "tokens", PayableAmount: "0.1"},
		{ExpenseTime: "2026-10-09 12:55:00-2026-10-09 13:00:00", ConfigName: "Seedream-4.0", ChargeItemCode: "Seedream_4.0_t2i", Count: "3", Unit: "images", PayableAmount: "0.09"},
		// Day 20-29: the start stamp itself contains "-20".
		{ExpenseTime: "2026-10-20 16:10:00-2026-10-20 16:15:00", ConfigName: "DeepSeek-V4.1-flash", ChargeItemCode: "DeepSeek_V4.1_flash_inference_peak_prompt_ap-southeast-1_realtime", Count: "1", Unit: "K tokens", PayableAmount: "0.001"},
	}
	entries, err := bytePlusBillEntries(rows)
	require.NoError(t, err)
	w1 := time.Date(2026, 10, 8, 10, 15, 0, 0, time.UTC).Unix()
	w2 := time.Date(2026, 10, 9, 12, 55, 0, 0, time.UTC).Unix()
	w3 := time.Date(2026, 10, 20, 16, 10, 0, 0, time.UTC).Unix()
	assert.Equal(t, []model.UpstreamBillEntry{
		{Wall: w1, Model: "DeepSeek-V4.1-flash", PromptTokens: 72, Cost: 0},
		{Wall: w1, Model: "DeepSeek-V4.1-flash", CompletionTokens: 2858, Cost: 0.01},
		{Wall: w2, Model: "DeepSeek-V4.1-flash", CacheReadTokens: 189056, Cost: 0.0005},
		{Wall: w2, Model: "Seed-2.0-lite", CompletionTokens: 1234, Cost: 0.1},
		{Wall: w2, Model: "Seedream-4.0", UsageUnit: "images", UsageAmount: 3, Cost: 0.09},
		{Wall: w3, Model: "DeepSeek-V4.1-flash", PromptTokens: 1000, Cost: 0.001},
	}, entries)

	_, err = bytePlusBillEntries([]bytePlusBillRow{{ExpenseTime: "-", ConfigName: "x", Count: "1", Unit: "K tokens", PayableAmount: "0"}})
	require.Error(t, err)
	_, err = bytePlusBillEntries(nil)
	require.Error(t, err)

	assert.Equal(t, "deepseek-v4-1-flash", bytePlusModelKey("DeepSeek-V4.1-flash"))
	assert.Equal(t, "glm-5-2", bytePlusModelKey("GLM_5.2"))
}
