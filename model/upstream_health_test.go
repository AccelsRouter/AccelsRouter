package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Relay outcomes and probes accumulate in memory, flush into one hourly row per
// (channel, model) with additive upserts, and read back as per-model stats and
// per-channel buckets with the most recent error preserved.
func TestUpstreamHealthRollup(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UpstreamModelHourly{}))
	DB.Exec("DELETE FROM upstream_model_hourly")
	upstreamHealthMu.Lock()
	upstreamHealthPending = map[upstreamHealthKey]*upstreamHealthDelta{}
	upstreamHealthMu.Unlock()

	RecordUpstreamOutcome(7, "gpt-4o", true, 900, "")
	RecordUpstreamOutcome(7, "gpt-4o", true, 1100, "")
	RecordUpstreamOutcome(7, "gpt-4o", false, 0, "upstream 502")
	RecordUpstreamProbe(7, "gpt-4o", true, 300, "")
	RecordUpstreamProbe(7, "claude-3", false, 0, "invalid api key")
	RecordUpstreamOutcome(0, "ignored", false, 0, "no channel chosen")
	require.NoError(t, FlushUpstreamHealth())
	// A second flush with more traffic increments the same row.
	RecordUpstreamOutcome(7, "gpt-4o", true, 1000, "")
	require.NoError(t, FlushUpstreamHealth())

	var rows []UpstreamModelHourly
	require.NoError(t, DB.Where("channel_id = ?", 7).Find(&rows).Error)
	assert.Len(t, rows, 2, "one row per (channel, model, hour)")

	since := common.GetTimestamp() - 3600*2
	stats, err := GetUpstreamModelStats(since)
	require.NoError(t, err)
	byModel := map[string]UpstreamModelStat{}
	for _, s := range stats {
		if s.ChannelId == 7 {
			byModel[s.ModelName] = s
		}
	}
	gpt := byModel["gpt-4o"]
	assert.EqualValues(t, 3, gpt.Requests)
	assert.EqualValues(t, 1, gpt.Failures)
	assert.EqualValues(t, 3000, gpt.LatencySum)
	assert.EqualValues(t, 1, gpt.Probes)
	assert.EqualValues(t, 0, gpt.ProbeFailures)
	assert.Equal(t, "upstream 502", gpt.LastError)
	claude := byModel["claude-3"]
	assert.EqualValues(t, 1, claude.Probes)
	assert.EqualValues(t, 1, claude.ProbeFailures)
	assert.Equal(t, "invalid api key", claude.LastError)
	_, ignored := byModel["ignored"]
	assert.False(t, ignored, "channel 0 is never recorded")

	buckets, err := GetUpstreamChannelBuckets(since)
	require.NoError(t, err)
	var mine []UpstreamBucket
	for _, b := range buckets {
		if b.ChannelId == 7 {
			mine = append(mine, b)
		}
	}
	require.Len(t, mine, 1, "both models fold into the channel's single hour bucket")
	assert.EqualValues(t, 3, mine[0].Requests)
	assert.EqualValues(t, 1, mine[0].Failures)
	assert.EqualValues(t, 2, mine[0].Probes)
	assert.EqualValues(t, 1, mine[0].ProbeFailures)

	single, err := GetUpstreamModelBuckets(7, "claude-3", since)
	require.NoError(t, err)
	require.Len(t, single, 1)
	assert.EqualValues(t, 0, single[0].Requests)
	assert.EqualValues(t, 1, single[0].ProbeFailures)
}
