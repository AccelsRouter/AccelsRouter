// Package smartroute holds the Redis-backed per-(channel, model)
// performance stats and scoring logic behind "smart routing": choosing
// among several channels that all serve the same model by their recent
// success rate, latency, and (in channel-pricing mode) price, instead of
// an admin-configured static Priority/Weight.
//
// This is its own package — not part of model or service — specifically
// to avoid an import cycle: model.GetChannel/GetRandomSatisfiedChannel
// and model.GetChannelPricingChannel (which need to call PickBestChannel)
// are themselves called from service, so smartroute can depend only on
// common (mirroring common/limiter, which has the same constraint for the
// same reason).
package smartroute

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	keyPrefix = "smart_route:v1"

	// ewmaAlpha weights each new sample's influence on the rolling
	// average: 0.2 means the last sample moves the average 20% of the
	// way toward itself, so a handful of consecutive samples already
	// meaningfully shift it, while a single blip doesn't swing it wildly.
	ewmaAlpha = 0.2

	// ConsecutiveFailThreshold is channel affinity's circuit breaker: this
	// many failures in a row on the sticky channel (for this specific
	// model) is treated as "completely unusable right now", overriding
	// the otherwise-strong preference to keep reusing it. Anything short
	// of this — even if smart routing would currently score another
	// candidate higher — is not enough to give up the sticky channel.
	ConsecutiveFailThreshold = 3

	// statsTTLSeconds bounds how long a (channel, model) pair's stats
	// survive with no traffic at all — a channel that stops serving a
	// model shouldn't keep influencing (or being circuit-broken by) stale
	// numbers from weeks ago. Refreshed on every write, so any actively
	// used pair never expires.
	statsTTLSeconds = 7 * 24 * 60 * 60
)

// Stats is one (channel, model) pair's rolling performance signal, as
// stored in Redis.
type Stats struct {
	AvgLatencyMs     float64
	SuccessRate      float64
	SampleCount      int64
	ConsecutiveFails int64
	UpdatedAt        int64
}

func statsKey(channelId int, modelName string) string {
	return fmt.Sprintf("%s:channel:%d:model:%s", keyPrefix, channelId, modelName)
}

// GetStats reads channelId's rolling stats for modelName. found is false
// when Redis is disabled, this pair has no recorded traffic yet, or the
// read failed — every caller (PickBestChannel, IsCircuitOpen) treats that
// the same way: no data to judge by, so don't penalize or specially favor
// this candidate on that dimension.
func GetStats(channelId int, modelName string) (stats Stats, found bool) {
	if !common.RedisEnabled || common.RDB == nil || channelId <= 0 || modelName == "" {
		return Stats{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	raw, err := common.RDB.HGetAll(ctx, statsKey(channelId, modelName)).Result()
	if err != nil || len(raw) == 0 {
		return Stats{}, false
	}
	parseFloat := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	parseInt := func(s string) int64 {
		v, _ := strconv.ParseInt(s, 10, 64)
		return v
	}
	return Stats{
		AvgLatencyMs:     parseFloat(raw["avg_latency_ms"]),
		SuccessRate:      parseFloat(raw["success_rate"]),
		SampleCount:      parseInt(raw["sample_count"]),
		ConsecutiveFails: parseInt(raw["consecutive_fails"]),
		UpdatedAt:        parseInt(raw["updated_at"]),
	}, true
}

// RecordOutcome feeds one completed request's outcome into
// channelId+modelName's rolling stats. Call this once per request that
// actually reached this channel (success or failure) — a request rejected
// before ever dispatching to a channel (over budget, disabled, no
// candidates at all) was never that channel's fault and must not be
// recorded against it.
//
// The read-modify-write here is intentionally not wrapped in an atomic
// Lua script: under concurrent requests to the same (channel, model) it
// can lose an update to a race, but this is a soft, self-correcting
// signal feeding a routing decision — not billing or an enforced limit —
// so simple, easy-to-verify code wins over strict atomicity here. A lost
// update just means the average catches up on the very next request
// instead of this one.
func RecordOutcome(channelId int, modelName string, latencyMs int64, success bool) {
	if !common.RedisEnabled || common.RDB == nil || channelId <= 0 || modelName == "" {
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.RecordOutcome: skipped for channel=%d model=%q (redisEnabled=%v channelId>0=%v modelName!=\"\"=%v)",
			channelId, modelName, common.RedisEnabled, channelId > 0, modelName != "",
		))
		return
	}

	successValue := 0.0
	if success {
		successValue = 1.0
	}

	next := Stats{
		AvgLatencyMs: float64(latencyMs),
		SuccessRate:  successValue,
		SampleCount:  1,
	}
	if !success {
		next.ConsecutiveFails = 1
	}

	if prev, hadPrev := GetStats(channelId, modelName); hadPrev {
		next.AvgLatencyMs = prev.AvgLatencyMs*(1-ewmaAlpha) + float64(latencyMs)*ewmaAlpha
		next.SuccessRate = prev.SuccessRate*(1-ewmaAlpha) + successValue*ewmaAlpha
		next.SampleCount = prev.SampleCount + 1
		if success {
			next.ConsecutiveFails = 0
		} else {
			next.ConsecutiveFails = prev.ConsecutiveFails + 1
		}
	}
	next.UpdatedAt = time.Now().Unix()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	key := statsKey(channelId, modelName)
	err := common.RDB.HSet(ctx, key, map[string]interface{}{
		"avg_latency_ms":    next.AvgLatencyMs,
		"success_rate":      next.SuccessRate,
		"sample_count":      next.SampleCount,
		"consecutive_fails": next.ConsecutiveFails,
		"updated_at":        next.UpdatedAt,
	}).Err()
	if err != nil {
		common.SysLog(fmt.Sprintf("smart route record outcome failed for channel %d model %s: %v", channelId, modelName, err))
		return
	}
	_ = common.RDB.Expire(ctx, key, statsTTLSeconds*time.Second).Err()
	common.SysLog(fmt.Sprintf(
		"[DEBUG] smartroute.RecordOutcome: channel=%d model=%q success=%v latencyMs=%d -> avg_latency_ms=%.1f success_rate=%.3f sample_count=%d consecutive_fails=%d",
		channelId, modelName, success, latencyMs, next.AvgLatencyMs, next.SuccessRate, next.SampleCount, next.ConsecutiveFails,
	))
}

// IsCircuitOpen reports whether channelId has failed modelName
// ConsecutiveFailThreshold times in a row. This is channel affinity's
// signal to abandon a sticky channel and let PickBestChannel choose
// fresh, even though every other affinity usability check (enabled,
// request-path support, daily token budget) still passes. A channel with
// no recorded failures, or no data at all, is never considered open here
// — silence is not failure.
func IsCircuitOpen(channelId int, modelName string) bool {
	stats, found := GetStats(channelId, modelName)
	if !found {
		return false
	}
	open := stats.ConsecutiveFails >= ConsecutiveFailThreshold
	if open {
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.IsCircuitOpen: channel=%d model=%q consecutive_fails=%d threshold=%d -> OPEN (affinity will abandon this channel)",
			channelId, modelName, stats.ConsecutiveFails, ConsecutiveFailThreshold,
		))
	}
	return open
}

// Candidate is one channel being scored by PickBestChannel.
type Candidate struct {
	ChannelId int
	// Price is this candidate's effective per-unit cost for the request:
	// the group ratio in group-mode selection (identical for every
	// candidate there, so it never actually changes the ranking — group
	// pricing doesn't vary by which channel within the group serves the
	// request; callers may simply pass 0 for every candidate in that
	// mode) or the per-(user, channel, model) binding ratio in
	// channel-pricing-mode selection (genuinely different per candidate,
	// so it does matter there). Lower is better. Zero or negative means
	// "no usable price signal for this candidate" and is excluded from
	// scoring entirely (see PickBestChannel) rather than scored as
	// free/best.
	Price float64
}

// Weights controls how much each dimension counts toward
// PickBestChannel's composite score.
type Weights struct {
	SuccessRate float64
	Latency     float64
	Price       float64
}

// DefaultWeights: success rate matters most (an unreliable channel is the
// worst choice regardless of how cheap or fast it is when it does work),
// latency second, price least.
var DefaultWeights = Weights{
	SuccessRate: 0.5,
	Latency:     0.3,
	Price:       0.2,
}

// RankChannels scores every candidate on success rate, latency, and
// price, and returns their channel IDs sorted best-first. This is the
// entire selection mechanism now — it replaces channel/ability Priority
// and Weight, for both group-mode and channel-pricing-mode selection.
// Candidates are whatever the caller's own filtering already narrowed
// down to (matching group+model+enabled, or a user's own channel
// bindings, minus anything already excluded for being disabled or over
// its daily token budget) — this function only ranks what it's given, it
// doesn't re-check usability.
//
// Callers retrying after a failure index further into this same ranked
// list (rank 0 = best, rank 1 = second-best, ...) instead of excluding
// the failed channel and re-ranking — simpler, and every attempt for a
// given request still sees a consistent order.
//
// A candidate with no recorded traffic yet for one or more dimensions is
// scored as exactly the average of its peers who do have data on that
// dimension (not worst, not best), so a newly added channel gets a real
// chance to be picked and start accumulating its own stats instead of
// being starved forever by channels with an established track record. If
// literally nobody in the candidate set has data yet, everyone scores the
// same on that dimension and it doesn't influence the outcome at all.
func RankChannels(candidates []Candidate, modelName string, weights Weights) []int {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.RankChannels: model=%q single candidate channel=%d, skipping scoring",
			modelName, candidates[0].ChannelId,
		))
		return []int{candidates[0].ChannelId}
	}

	candidateIds := make([]int, len(candidates))
	for i, c := range candidates {
		candidateIds[i] = c.ChannelId
	}
	common.SysLog(fmt.Sprintf(
		"[DEBUG] smartroute.RankChannels: model=%q scoring %d candidates=%v weights={success:%.2f latency:%.2f price:%.2f}",
		modelName, len(candidates), candidateIds, weights.SuccessRate, weights.Latency, weights.Price,
	))

	statsByChannel := make(map[int]Stats, len(candidates))
	var latencySamples, successSamples, priceSamples []float64

	for _, c := range candidates {
		if stats, found := GetStats(c.ChannelId, modelName); found {
			statsByChannel[c.ChannelId] = stats
			latencySamples = append(latencySamples, stats.AvgLatencyMs)
			successSamples = append(successSamples, stats.SuccessRate)
		}
		if c.Price > 0 {
			priceSamples = append(priceSamples, c.Price)
		}
	}

	avgOf := func(values []float64) float64 {
		if len(values) == 0 {
			return 0
		}
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	}
	minMaxOf := func(values []float64) (lo, hi float64) {
		if len(values) == 0 {
			return 0, 0
		}
		lo, hi = values[0], values[0]
		for _, v := range values[1:] {
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		return lo, hi
	}

	fallbackLatency := avgOf(latencySamples)
	// Fork: unlike latency (which naturally neutralizes to a full score
	// for everyone when minLatency==maxLatency==0, see
	// normalizeLowerIsBetter) and price (explicitly excluded via
	// priceWeight=0 below when nobody has a sample), success rate is used
	// directly as a 0..1 score with no such safety net. avgOf an empty
	// slice returns 0 — literally "always fails" — which would wrongly
	// tank every candidate's score whenever NONE of them have data yet
	// (the common cold-start case), violating the "not worst, not best"
	// promise documented above. Default to 1.0 (optimistic: assume it
	// works until proven otherwise) in that specific case instead; when
	// at least one candidate does have data, the average of those real
	// samples is used exactly as before.
	fallbackSuccess := 1.0
	if len(successSamples) > 0 {
		fallbackSuccess = avgOf(successSamples)
	}
	fallbackPrice := avgOf(priceSamples)
	minLatency, maxLatency := minMaxOf(latencySamples)
	minPrice, maxPrice := minMaxOf(priceSamples)

	// Normalizes to "higher is better", 0..1, relative to this candidate
	// set only — smart routing only ever needs to rank these candidates
	// against each other, never against some absolute scale.
	normalizeLowerIsBetter := func(value, lo, hi float64) float64 {
		if hi <= lo {
			return 1 // every candidate with data lands at the same spot (or there's only one data point) — don't let a degenerate range arbitrarily favor anyone
		}
		return 1 - (value-lo)/(hi-lo)
	}

	priceWeight := weights.Price
	if len(priceSamples) == 0 {
		priceWeight = 0 // nobody in this candidate set has a usable price signal — exclude the dimension rather than treating everyone as free
	}
	totalWeight := weights.SuccessRate + weights.Latency + priceWeight
	if totalWeight <= 0 {
		totalWeight = 1
	}

	type scored struct {
		channelId int
		score     float64
		hasStats  bool
	}
	results := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		latencyMs, successRate := fallbackLatency, fallbackSuccess
		_, hasStats := statsByChannel[c.ChannelId]
		if hasStats {
			latencyMs, successRate = statsByChannel[c.ChannelId].AvgLatencyMs, statsByChannel[c.ChannelId].SuccessRate
		}
		price := fallbackPrice
		if c.Price > 0 {
			price = c.Price
		}

		score := weights.SuccessRate*successRate +
			weights.Latency*normalizeLowerIsBetter(latencyMs, minLatency, maxLatency) +
			priceWeight*normalizeLowerIsBetter(price, minPrice, maxPrice)
		score /= totalWeight

		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.RankChannels:   candidate channel=%d hasStats=%v avg_latency_ms=%.1f success_rate=%.3f price=%.4f -> score=%.4f",
			c.ChannelId, hasStats, latencyMs, successRate, price, score,
		))

		results = append(results, scored{channelId: c.ChannelId, score: score, hasStats: hasStats})
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	common.SysLog(fmt.Sprintf(
		"[DEBUG] smartroute.RankChannels: model=%q final ranking (best first)=%+v",
		modelName, results,
	))

	ranked := make([]int, len(results))
	for i, r := range results {
		ranked[i] = r.channelId
	}
	return ranked
}

// PickBestChannel is a convenience wrapper over RankChannels for callers
// that only ever want the single top-ranked candidate (channel affinity's
// "score every candidate" fallback path, channel-pricing's initial pick).
func PickBestChannel(candidates []Candidate, modelName string, weights Weights) int {
	ranked := RankChannels(candidates, modelName, weights)
	if len(ranked) == 0 {
		return 0
	}
	return ranked[0]
}
