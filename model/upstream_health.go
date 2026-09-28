package model

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Upstream health: an hourly rollup per (channel, model) of what actually
// happened upstream — every relayed request's outcome and latency, plus
// channel test probes. Fed from RecordConsumeLog / RecordErrorLog and the
// channel tester through an in-memory aggregator flushed every few seconds,
// so the hot path never waits on this table. Backs the admin upstream monitor
// (status, historical availability). Own table; nothing existing is touched.

type UpstreamModelHourly struct {
	Id            int64  `json:"id" gorm:"primarykey"`
	HourBucket    int64  `json:"hour_bucket" gorm:"index;uniqueIndex:uq_upstream_model_hourly,priority:1"`
	ChannelId     int    `json:"channel_id" gorm:"index;uniqueIndex:uq_upstream_model_hourly,priority:2"`
	ModelName     string `json:"model_name" gorm:"type:varchar(191);uniqueIndex:uq_upstream_model_hourly,priority:3"`
	Requests      int64  `json:"requests"`       // relayed requests that succeeded
	Failures      int64  `json:"failures"`       // relayed requests that failed upstream
	LatencySum    int64  `json:"latency_sum"`    // ms, over successful requests
	Probes        int64  `json:"probes"`         // channel tests run
	ProbeFailures int64  `json:"probe_failures"` // channel tests that failed
	LastOkAt      int64  `json:"last_ok_at"`
	LastErrorAt   int64  `json:"last_error_at"`
	LastError     string `json:"last_error" gorm:"type:varchar(512)"`
	UpdatedAt     int64  `json:"updated_at"`
}

func (UpstreamModelHourly) TableName() string { return "upstream_model_hourly" }

const (
	upstreamHealthFlushEvery = 15 * time.Second
	upstreamHealthRetention  = 30 * 24 * time.Hour
)

type upstreamHealthKey struct {
	bucket    int64
	channelId int
	model     string
}

type upstreamHealthDelta struct {
	requests, failures, latencySum, probes, probeFailures int64
	lastOkAt, lastErrorAt                                 int64
	lastError                                             string
}

var (
	upstreamHealthMu      sync.Mutex
	upstreamHealthPending = map[upstreamHealthKey]*upstreamHealthDelta{}
	upstreamHealthOnce    sync.Once
	upstreamHealthPruned  int64
)

func upstreamHealthBucket(ts int64) int64 { return ts - ts%3600 }

func upstreamHealthModel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	if len(name) > 191 {
		return name[:191]
	}
	return name
}

func upstreamHealthErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 512 {
		return msg[:512]
	}
	return msg
}

func upstreamHealthAdd(channelId int, modelName string, apply func(d *upstreamHealthDelta)) {
	if channelId <= 0 {
		return
	}
	k := upstreamHealthKey{bucket: upstreamHealthBucket(common.GetTimestamp()), channelId: channelId, model: upstreamHealthModel(modelName)}
	upstreamHealthMu.Lock()
	d := upstreamHealthPending[k]
	if d == nil {
		d = &upstreamHealthDelta{}
		upstreamHealthPending[k] = d
	}
	apply(d)
	upstreamHealthMu.Unlock()
}

// RecordUpstreamOutcome notes one relayed request's upstream outcome.
func RecordUpstreamOutcome(channelId int, modelName string, ok bool, latencyMs int64, errMsg string) {
	now := common.GetTimestamp()
	upstreamHealthAdd(channelId, modelName, func(d *upstreamHealthDelta) {
		if ok {
			d.requests++
			if latencyMs > 0 {
				d.latencySum += latencyMs
			}
			d.lastOkAt = now
			return
		}
		d.failures++
		d.lastErrorAt = now
		d.lastError = upstreamHealthErr(errMsg)
	})
}

// RecordUpstreamProbe notes one channel test (manual or scheduled).
func RecordUpstreamProbe(channelId int, modelName string, ok bool, latencyMs int64, errMsg string) {
	now := common.GetTimestamp()
	upstreamHealthAdd(channelId, modelName, func(d *upstreamHealthDelta) {
		d.probes++
		if ok {
			d.lastOkAt = now
			return
		}
		d.probeFailures++
		d.lastErrorAt = now
		d.lastError = upstreamHealthErr(errMsg)
	})
}

// FlushUpstreamHealth writes the pending deltas (upsert per key). Safe to call
// from several nodes: increments are additive.
func FlushUpstreamHealth() error {
	upstreamHealthMu.Lock()
	pending := upstreamHealthPending
	upstreamHealthPending = map[upstreamHealthKey]*upstreamHealthDelta{}
	upstreamHealthMu.Unlock()
	var firstErr error
	for k, d := range pending {
		if err := upsertUpstreamHealth(k, d); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	now := common.GetTimestamp()
	if now-upstreamHealthPruned > 3600 {
		upstreamHealthPruned = now
		cutoff := now - int64(upstreamHealthRetention.Seconds())
		if err := DB.Where("hour_bucket < ?", cutoff).Delete(&UpstreamModelHourly{}).Error; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func upsertUpstreamHealth(k upstreamHealthKey, d *upstreamHealthDelta) error {
	now := common.GetTimestamp()
	updates := map[string]interface{}{
		"requests":       gorm.Expr("requests + ?", d.requests),
		"failures":       gorm.Expr("failures + ?", d.failures),
		"latency_sum":    gorm.Expr("latency_sum + ?", d.latencySum),
		"probes":         gorm.Expr("probes + ?", d.probes),
		"probe_failures": gorm.Expr("probe_failures + ?", d.probeFailures),
		"updated_at":     now,
	}
	if d.lastOkAt > 0 {
		updates["last_ok_at"] = d.lastOkAt
	}
	if d.lastErrorAt > 0 {
		updates["last_error_at"] = d.lastErrorAt
		updates["last_error"] = d.lastError
	}
	increment := func() (int64, error) {
		res := DB.Model(&UpstreamModelHourly{}).
			Where("hour_bucket = ? AND channel_id = ? AND model_name = ?", k.bucket, k.channelId, k.model).
			Updates(updates)
		return res.RowsAffected, res.Error
	}
	affected, err := increment()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	createErr := DB.Create(&UpstreamModelHourly{
		HourBucket: k.bucket, ChannelId: k.channelId, ModelName: k.model,
		Requests: d.requests, Failures: d.failures, LatencySum: d.latencySum,
		Probes: d.probes, ProbeFailures: d.probeFailures,
		LastOkAt: d.lastOkAt, LastErrorAt: d.lastErrorAt, LastError: d.lastError,
		UpdatedAt: now,
	}).Error
	if createErr == nil {
		return nil
	}
	// Unique-conflict race with another node: the row now exists, increment it.
	if affected, err = increment(); err != nil {
		return err
	} else if affected == 0 {
		return fmt.Errorf("upstream health upsert lost race for channel %d model %s: %w", k.channelId, k.model, createErr)
	}
	return nil
}

// StartUpstreamHealthFlusher runs the periodic flush (once per process).
func StartUpstreamHealthFlusher() {
	upstreamHealthOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(upstreamHealthFlushEvery)
			defer ticker.Stop()
			for range ticker.C {
				if err := FlushUpstreamHealth(); err != nil {
					common.SysError("upstream health flush failed: " + err.Error())
				}
			}
		}()
	})
}

// UpstreamModelStat is the per-(channel, model) aggregate over a window.
type UpstreamModelStat struct {
	ChannelId     int    `json:"channel_id"`
	ModelName     string `json:"model_name"`
	Requests      int64  `json:"requests"`
	Failures      int64  `json:"failures"`
	LatencySum    int64  `json:"latency_sum"`
	Probes        int64  `json:"probes"`
	ProbeFailures int64  `json:"probe_failures"`
	LastOkAt      int64  `json:"last_ok_at"`
	LastErrorAt   int64  `json:"last_error_at"`
	LastError     string `json:"last_error"`
}

// upstreamRowsSince loads the rollup rows of the window; aggregation happens
// in Go so the query stays identical on SQLite, MySQL and PostgreSQL (SUM
// result types differ). The table is sparse — only (channel, model, hour)
// triples with traffic or probes exist — so this stays small.
func upstreamRowsSince(since int64, channelId int, modelName string) ([]UpstreamModelHourly, error) {
	q := DB.Model(&UpstreamModelHourly{}).Where("hour_bucket >= ?", since)
	if channelId > 0 {
		q = q.Where("channel_id = ?", channelId)
	}
	if modelName != "" {
		q = q.Where("model_name = ?", modelName)
	}
	var rows []UpstreamModelHourly
	err := q.Order("hour_bucket").Find(&rows).Error
	return rows, err
}

// GetUpstreamModelStats aggregates the window [since, now] per (channel, model),
// carrying the most recent error text.
func GetUpstreamModelStats(since int64) ([]UpstreamModelStat, error) {
	rows, err := upstreamRowsSince(since, 0, "")
	if err != nil {
		return nil, err
	}
	type key struct {
		c int
		m string
	}
	agg := map[key]*UpstreamModelStat{}
	for _, r := range rows {
		k := key{r.ChannelId, r.ModelName}
		st := agg[k]
		if st == nil {
			st = &UpstreamModelStat{ChannelId: r.ChannelId, ModelName: r.ModelName}
			agg[k] = st
		}
		st.Requests += r.Requests
		st.Failures += r.Failures
		st.LatencySum += r.LatencySum
		st.Probes += r.Probes
		st.ProbeFailures += r.ProbeFailures
		if r.LastOkAt > st.LastOkAt {
			st.LastOkAt = r.LastOkAt
		}
		if r.LastErrorAt > st.LastErrorAt {
			st.LastErrorAt = r.LastErrorAt
			st.LastError = r.LastError
		}
	}
	stats := make([]UpstreamModelStat, 0, len(agg))
	for _, st := range agg {
		stats = append(stats, *st)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].ChannelId != stats[j].ChannelId {
			return stats[i].ChannelId < stats[j].ChannelId
		}
		return stats[i].ModelName < stats[j].ModelName
	})
	return stats, nil
}

// UpstreamBucket is one hour of a channel's (or a channel+model's) history.
type UpstreamBucket struct {
	ChannelId     int   `json:"channel_id,omitempty"`
	Bucket        int64 `json:"bucket"`
	Requests      int64 `json:"requests"`
	Failures      int64 `json:"failures"`
	LatencySum    int64 `json:"latency_sum"`
	Probes        int64 `json:"probes"`
	ProbeFailures int64 `json:"probe_failures"`
}

func bucketize(rows []UpstreamModelHourly, perChannel bool) []UpstreamBucket {
	type key struct {
		c int
		b int64
	}
	agg := map[key]*UpstreamBucket{}
	for _, r := range rows {
		k := key{b: r.HourBucket}
		if perChannel {
			k.c = r.ChannelId
		}
		b := agg[k]
		if b == nil {
			b = &UpstreamBucket{ChannelId: k.c, Bucket: r.HourBucket}
			agg[k] = b
		}
		b.Requests += r.Requests
		b.Failures += r.Failures
		b.LatencySum += r.LatencySum
		b.Probes += r.Probes
		b.ProbeFailures += r.ProbeFailures
	}
	out := make([]UpstreamBucket, 0, len(agg))
	for _, b := range agg {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChannelId != out[j].ChannelId {
			return out[i].ChannelId < out[j].ChannelId
		}
		return out[i].Bucket < out[j].Bucket
	})
	return out
}

// GetUpstreamChannelBuckets returns hourly buckets per channel (all models
// merged) since the given time, ordered by channel then time.
func GetUpstreamChannelBuckets(since int64) ([]UpstreamBucket, error) {
	rows, err := upstreamRowsSince(since, 0, "")
	if err != nil {
		return nil, err
	}
	return bucketize(rows, true), nil
}

// GetUpstreamModelBuckets returns hourly buckets for one channel, optionally
// narrowed to one model.
func GetUpstreamModelBuckets(channelId int, modelName string, since int64) ([]UpstreamBucket, error) {
	rows, err := upstreamRowsSince(since, channelId, modelName)
	if err != nil {
		return nil, err
	}
	return bucketize(rows, false), nil
}
