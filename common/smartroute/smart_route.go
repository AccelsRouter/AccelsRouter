// Package smartroute holds the Redis-backed per-(channel, model) health
// tracking and ordering logic behind "smart routing": choosing among
// several channels that all serve the same model by recent reliability and
// (in channel-pricing mode) price, instead of an admin-configured static
// Priority/Weight. The approach follows OpenRouter's default routing:
//
//  1. Channels that have recently been failing are tried last.
//  2. Among the rest, selection is a weighted random draw favoring the
//     cheaper channel (weight = 1/price^2), so traffic is spread out
//     rather than always landing on a single winner.
//  3. Latency has no scoring role of its own: a response slower than the
//     limit for its kind (see MaxLatencyStreamMs / MaxLatencyNonStreamMs)
//     is simply recorded as a failure, and so feeds the health check in
//     (1).
//
// This is its own package — not part of model or service — specifically
// to avoid an import cycle: model.GetChannel/GetRandomSatisfiedChannel
// and model.GetChannelPricingChannel (which call RankChannels) are
// themselves called from service, so smartroute can depend only on common
// (mirroring common/limiter, which has the same constraint for the same
// reason).
package smartroute

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	keyPrefix = "smart_route:v1"

	// ewmaAlpha weights each new sample's influence on the rolling
	// averages kept for diagnostics (avg_latency_ms, success_rate): the
	// last sample moves the average 20% of the way toward itself. The
	// health check itself only uses consecutive_fails and last_fail_at.
	ewmaAlpha = 0.2

	// ConsecutiveFailThreshold and UnhealthyWindow together define
	// "recently failing": a (channel, model) pair is unhealthy while it
	// has failed this many times in a row AND its latest failure is
	// within the window. Once the window passes with no new failure the
	// pair is healthy again and gets traffic, so a recovered channel is
	// not shut out forever; if it is still broken, a single further
	// failure (the count is still at or above the threshold) marks it
	// unhealthy again immediately.
	ConsecutiveFailThreshold = 3
	UnhealthyWindow          = 5 * time.Minute

	// The slowest latency still counted as a success, set for the slowest
	// legitimate case; a response at or beyond it is recorded as a failure.
	// Streaming and non-streaming measure different things, so they get
	// separate limits: for a stream the latency is time to the first
	// response chunk (a reasoning model can take tens of seconds to
	// produce its first token), for a non-streaming request it is the
	// time until the complete response is back (a long generation can
	// take minutes). Note this only changes the health statistics — the
	// request itself already completed.
	MaxLatencyStreamMs    = 60 * 1000
	MaxLatencyNonStreamMs = 300 * 1000

	// statsTTLSeconds bounds how long a (channel, model) pair's stats
	// survive with no traffic at all. Refreshed on every write, so any
	// actively used pair never expires.
	statsTTLSeconds = 7 * 24 * 60 * 60
)

// Stats is one (channel, model) pair's tracked state, as stored in Redis.
type Stats struct {
	AvgLatencyMs     float64
	SuccessRate      float64
	SampleCount      int64
	ConsecutiveFails int64
	LastFailAt       int64
	UpdatedAt        int64
}

// Unhealthy reports whether the pair is currently in its failure window.
// A pair with no recorded failure time (including data written before
// last_fail_at existed) is never unhealthy — silence is not failure.
func (s Stats) Unhealthy(now time.Time) bool {
	if s.ConsecutiveFails < ConsecutiveFailThreshold || s.LastFailAt <= 0 {
		return false
	}
	return now.Sub(time.Unix(s.LastFailAt, 0)) < UnhealthyWindow
}

func statsKey(channelId int, modelName string) string {
	return fmt.Sprintf("%s:channel:%d:model:%s", keyPrefix, channelId, modelName)
}

// GetStats reads channelId's tracked state for modelName. found is false
// when Redis is disabled, this pair has no recorded traffic yet, or the
// read failed — callers treat all of those as "no data, assume healthy".
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
		LastFailAt:       parseInt(raw["last_fail_at"]),
		UpdatedAt:        parseInt(raw["updated_at"]),
	}, true
}

// RecordOutcome feeds one completed request's outcome into
// channelId+modelName's tracked state. Call this once per request that
// actually reached this channel (success or failure) — a request rejected
// before ever dispatching to a channel was never that channel's fault and
// must not be recorded against it.
//
// latencyMs is time to the first response chunk when stream is true, or
// time until the full response when it is false. A "successful" request at
// or beyond the limit for its kind is recorded as a failure instead.
//
// The read-modify-write here is intentionally not atomic: under concurrent
// requests to the same pair it can lose an update to a race, but this is a
// soft, self-correcting signal feeding a routing decision — not billing or
// an enforced limit — so simple code wins over strict atomicity.
func RecordOutcome(channelId int, modelName string, latencyMs int64, stream bool, success bool) {
	if !common.RedisEnabled || common.RDB == nil || channelId <= 0 || modelName == "" {
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.RecordOutcome: skipped for channel=%d model=%q (redisEnabled=%v channelId>0=%v modelName!=\"\"=%v)",
			channelId, modelName, common.RedisEnabled, channelId > 0, modelName != "",
		))
		return
	}

	maxLatencyMs := int64(MaxLatencyNonStreamMs)
	if stream {
		maxLatencyMs = MaxLatencyStreamMs
	}
	if success && latencyMs >= maxLatencyMs {
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.RecordOutcome: channel=%d model=%q stream=%v latencyMs=%d >= %d, counting as a failure",
			channelId, modelName, stream, latencyMs, maxLatencyMs,
		))
		success = false
	}

	successValue := 0.0
	if success {
		successValue = 1.0
	}
	now := time.Now().Unix()

	next := Stats{
		AvgLatencyMs: float64(latencyMs),
		SuccessRate:  successValue,
		SampleCount:  1,
	}
	if !success {
		next.ConsecutiveFails = 1
		next.LastFailAt = now
	}

	if prev, hadPrev := GetStats(channelId, modelName); hadPrev {
		next.AvgLatencyMs = prev.AvgLatencyMs*(1-ewmaAlpha) + float64(latencyMs)*ewmaAlpha
		next.SuccessRate = prev.SuccessRate*(1-ewmaAlpha) + successValue*ewmaAlpha
		next.SampleCount = prev.SampleCount + 1
		next.LastFailAt = prev.LastFailAt
		if success {
			next.ConsecutiveFails = 0
		} else {
			next.ConsecutiveFails = prev.ConsecutiveFails + 1
			next.LastFailAt = now
		}
	}
	next.UpdatedAt = now

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	key := statsKey(channelId, modelName)
	err := common.RDB.HSet(ctx, key, map[string]interface{}{
		"avg_latency_ms":    next.AvgLatencyMs,
		"success_rate":      next.SuccessRate,
		"sample_count":      next.SampleCount,
		"consecutive_fails": next.ConsecutiveFails,
		"last_fail_at":      next.LastFailAt,
		"updated_at":        next.UpdatedAt,
	}).Err()
	if err != nil {
		common.SysLog(fmt.Sprintf("smart route record outcome failed for channel %d model %s: %v", channelId, modelName, err))
		return
	}
	_ = common.RDB.Expire(ctx, key, statsTTLSeconds*time.Second).Err()
	common.SysLog(fmt.Sprintf(
		"[DEBUG] smartroute.RecordOutcome: channel=%d model=%q stream=%v success=%v latencyMs=%d -> avg_latency_ms=%.1f success_rate=%.3f sample_count=%d consecutive_fails=%d",
		channelId, modelName, stream, success, latencyMs, next.AvgLatencyMs, next.SuccessRate, next.SampleCount, next.ConsecutiveFails,
	))
}

// IsCircuitOpen reports whether channelId is currently unhealthy for
// modelName (see Stats.Unhealthy). Channel affinity uses it to abandon a
// sticky channel even though every other usability check still passes.
func IsCircuitOpen(channelId int, modelName string) bool {
	stats, found := GetStats(channelId, modelName)
	if !found {
		return false
	}
	open := stats.Unhealthy(time.Now())
	if open {
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.IsCircuitOpen: channel=%d model=%q consecutive_fails=%d threshold=%d -> OPEN (affinity will abandon this channel)",
			channelId, modelName, stats.ConsecutiveFails, ConsecutiveFailThreshold,
		))
	}
	return open
}

// Candidate is one channel being ordered by RankChannels.
type Candidate struct {
	ChannelId int
	// Price is this candidate's effective cost multiplier: the per-(user,
	// channel, model) binding ratio in channel-pricing mode. Group mode
	// has no per-channel price, so callers pass 0 and RankChannels draws
	// uniformly. Lower is cheaper and more likely to be drawn.
	Price float64
}

// uniform returns a number in (0, 1) for (seed, channelId). The same seed
// always yields the same number for the same channel, so every attempt of
// one request (retries included) sees one consistent order; an empty seed
// yields a fresh random number on every call.
func uniform(seed string, channelId int) float64 {
	if seed == "" {
		u := rand.Float64()
		if u <= 0 {
			u = math.SmallestNonzeroFloat64
		}
		return u
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%d", seed, channelId)))
	// FNV alone leaves the high bits almost unchanged between seeds that
	// differ only in their last characters, which made the draw nearly
	// deterministic; run its output through the murmur3 finalizer so every
	// input bit affects every output bit before taking the top 53 bits.
	x := h.Sum64()
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return (float64(x>>11) + 0.5) / float64(uint64(1)<<53)
}

// RankChannels returns candidate channel IDs in the order they should be
// tried: every healthy channel first, then the unhealthy ones (so they
// still serve if nothing healthy is left). Within each group the order is
// a weighted random draw without replacement, weight = 1/price^2 when
// every candidate has a price (so a channel at half the price is four
// times as likely to come first), equal weights otherwise.
//
// Callers retrying after a failure index further into this same list
// (rank 0 first, rank 1 next, ...). Pass the same non-empty seed for every
// attempt of one request — the request ID — so those attempts agree on
// the order and a retry cannot land on the channel that just failed.
//
// Candidates are whatever the caller's own filtering already narrowed
// down to (matching group+model+enabled, or a user's own bindings, minus
// anything disabled or over its daily token budget); this function only
// orders them. Candidate IDs must be unique.
func RankChannels(candidates []Candidate, modelName string, seed string) []int {
	switch len(candidates) {
	case 0:
		return nil
	case 1:
		common.SysLog(fmt.Sprintf(
			"[DEBUG] smartroute.RankChannels: model=%q single candidate channel=%d, skipping ordering",
			modelName, candidates[0].ChannelId,
		))
		return []int{candidates[0].ChannelId}
	}

	usePrice := true
	for _, c := range candidates {
		if c.Price <= 0 {
			usePrice = false
			break
		}
	}

	type entry struct {
		channelId int
		weight    float64
		fails     int64
		healthy   bool
		key       float64
	}
	now := time.Now()
	entries := make([]entry, 0, len(candidates))
	for _, c := range candidates {
		weight := 1.0
		if usePrice {
			weight = 1 / (c.Price * c.Price)
		}
		e := entry{channelId: c.ChannelId, weight: weight, healthy: true}
		if stats, found := GetStats(c.ChannelId, modelName); found {
			e.fails = stats.ConsecutiveFails
			e.healthy = !stats.Unhealthy(now)
		}
		// Weighted sampling without replacement (Efraimidis–Spirakis):
		// sorting by u^(1/w) descending, computed as ln(u)/w for
		// numerical stability, draws each channel first with probability
		// proportional to its weight.
		e.key = math.Log(uniform(seed, c.ChannelId)) / weight
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].healthy != entries[j].healthy {
			return entries[i].healthy
		}
		return entries[i].key > entries[j].key
	})

	ranked := make([]int, len(entries))
	parts := make([]string, len(entries))
	for i, e := range entries {
		ranked[i] = e.channelId
		parts[i] = fmt.Sprintf("ch%d(weight=%.3f consecutive_fails=%d healthy=%v)", e.channelId, e.weight, e.fails, e.healthy)
	}
	common.SysLog(fmt.Sprintf(
		"[DEBUG] smartroute.RankChannels: model=%q seeded=%v priceWeighted=%v order=[%s]",
		modelName, seed != "", usePrice, strings.Join(parts, " "),
	))
	return ranked
}
