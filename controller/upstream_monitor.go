package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// Admin upstream monitor: per-channel / per-model status and historical
// availability (from the upstream_model_hourly rollup) and a live comparison
// of upstream pricing against the platform's own. Admin-only, read-only;
// probes are triggered through the existing channel test endpoint.

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

type upstreamModelHealth struct {
	Model         string  `json:"model"`
	Listed        bool    `json:"listed"` // declared on the channel (vs seen only in traffic)
	Requests      int64   `json:"requests"`
	Failures      int64   `json:"failures"`
	Probes        int64   `json:"probes"`
	ProbeFailures int64   `json:"probe_failures"`
	Availability  float64 `json:"availability"` // 0..1 over requests+probes; -1 = no data
	AvgLatencyMs  int64   `json:"avg_latency_ms"`
	LastOkAt      int64   `json:"last_ok_at"`
	LastErrorAt   int64   `json:"last_error_at"`
	LastError     string  `json:"last_error"`
}

type upstreamChannelHealth struct {
	Id           int                    `json:"id"`
	Name         string                 `json:"name"`
	Type         int                    `json:"type"`
	TypeName     string                 `json:"type_name"`
	Status       int                    `json:"status"`
	TestTime     int64                  `json:"test_time"`
	ResponseTime int                    `json:"response_time"`
	Requests     int64                  `json:"requests"`
	Failures     int64                  `json:"failures"`
	Probes       int64                  `json:"probes"`
	Availability float64                `json:"availability"` // -1 = no data
	Models       []upstreamModelHealth  `json:"models"`
	Buckets      []model.UpstreamBucket `json:"buckets"`
}

func upstreamWindowHours(c *gin.Context) int {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	switch {
	case hours <= 0:
		return 24
	case hours > 24*30:
		return 24 * 30
	}
	return hours
}

func availabilityOf(ok, total int64) float64 {
	if total <= 0 {
		return -1
	}
	return float64(ok) / float64(total)
}

// AdminUpstreamHealth — GET /api/admin/upstream/health?hours=24
func AdminUpstreamHealth(c *gin.Context) {
	hours := upstreamWindowHours(c)
	since := common.GetTimestamp() - int64(hours)*3600
	var channels []model.Channel
	if err := model.DB.Select("id", "name", "type", "status", "models", "test_time", "response_time").
		Order("id asc").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	stats, err := model.GetUpstreamModelStats(since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	buckets, err := model.GetUpstreamChannelBuckets(since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	statsByChannel := map[int][]model.UpstreamModelStat{}
	for _, s := range stats {
		statsByChannel[s.ChannelId] = append(statsByChannel[s.ChannelId], s)
	}
	bucketsByChannel := map[int][]model.UpstreamBucket{}
	for _, b := range buckets {
		id := b.ChannelId
		b.ChannelId = 0
		bucketsByChannel[id] = append(bucketsByChannel[id], b)
	}

	out := make([]upstreamChannelHealth, 0, len(channels))
	for _, ch := range channels {
		row := upstreamChannelHealth{
			Id: ch.Id, Name: ch.Name, Type: ch.Type, TypeName: constant.GetChannelTypeName(ch.Type),
			Status: ch.Status, TestTime: ch.TestTime, ResponseTime: ch.ResponseTime,
			Models: []upstreamModelHealth{}, Buckets: bucketsByChannel[ch.Id],
		}
		if row.Buckets == nil {
			row.Buckets = []model.UpstreamBucket{}
		}
		byModel := map[string]*upstreamModelHealth{}
		for _, m := range ch.GetModels() {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			byModel[m] = &upstreamModelHealth{Model: m, Listed: true, Availability: -1}
		}
		var okTotal, allTotal int64
		for _, s := range statsByChannel[ch.Id] {
			mh := byModel[s.ModelName]
			if mh == nil {
				mh = &upstreamModelHealth{Model: s.ModelName, Availability: -1}
				byModel[s.ModelName] = mh
			}
			mh.Requests, mh.Failures, mh.Probes, mh.ProbeFailures = s.Requests, s.Failures, s.Probes, s.ProbeFailures
			mh.LastOkAt, mh.LastErrorAt, mh.LastError = s.LastOkAt, s.LastErrorAt, s.LastError
			if s.Requests > 0 {
				mh.AvgLatencyMs = s.LatencySum / s.Requests
			}
			ok := s.Requests + (s.Probes - s.ProbeFailures)
			total := s.Requests + s.Failures + s.Probes
			mh.Availability = availabilityOf(ok, total)
			okTotal += ok
			allTotal += total
			row.Requests += s.Requests
			row.Failures += s.Failures
			row.Probes += s.Probes
		}
		row.Availability = availabilityOf(okTotal, allTotal)
		for _, mh := range byModel {
			row.Models = append(row.Models, *mh)
		}
		sort.Slice(row.Models, func(i, j int) bool {
			// worst availability first, unknown last, then name
			ai, aj := row.Models[i].Availability, row.Models[j].Availability
			if (ai < 0) != (aj < 0) {
				return aj < 0
			}
			if ai != aj {
				return ai < aj
			}
			return row.Models[i].Model < row.Models[j].Model
		})
		out = append(out, row)
	}
	common.ApiSuccess(c, gin.H{"hours": hours, "since": since, "channels": out})
}

// AdminUpstreamHistory — GET /api/admin/upstream/history?channel_id=&model=&hours=
func AdminUpstreamHistory(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	if channelId <= 0 {
		common.ApiErrorMsg(c, "channel_id is required")
		return
	}
	hours := upstreamWindowHours(c)
	since := common.GetTimestamp() - int64(hours)*3600
	buckets, err := model.GetUpstreamModelBuckets(channelId, strings.TrimSpace(c.Query("model")), since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"hours": hours, "since": since, "buckets": buckets})
}

// ---------------------------------------------------------------------------
// Prices
// ---------------------------------------------------------------------------

// upstreamPrice is one model's price in either ratio form (per-token: input
// = model_ratio, output = model_ratio × completion_ratio, in the platform's
// ratio unit) or per-call form (model_price).
type upstreamPrice struct {
	PerCall         bool    `json:"per_call"`
	ModelRatio      float64 `json:"model_ratio"`
	CompletionRatio float64 `json:"completion_ratio"`
	ModelPrice      float64 `json:"model_price"`
}

type upstreamModelPriceRow struct {
	Model    string         `json:"model"`
	Local    *upstreamPrice `json:"local"`
	Upstream *upstreamPrice `json:"upstream"`
	// match | platform_higher | platform_lower | mixed | type_mismatch |
	// missing_upstream | unpriced_local
	Status string `json:"status"`
}

type upstreamChannelPrices struct {
	Id        int                     `json:"id"`
	Name      string                  `json:"name"`
	Type      int                     `json:"type"`
	TypeName  string                  `json:"type_name"`
	Status    int                     `json:"status"`
	Host      string                  `json:"host"`
	Ok        bool                    `json:"ok"`
	Error     string                  `json:"error,omitempty"`
	FetchedAt int64                   `json:"fetched_at"`
	Source    string                  `json:"source,omitempty"` // endpoint that answered
	Models    []upstreamModelPriceRow `json:"models"`
	Summary   map[string]int          `json:"summary"`
}

type upstreamPricing struct {
	modelRatio      map[string]float64
	completionRatio map[string]float64
	modelPrice      map[string]float64
}

const (
	upstreamPriceCacheTTL = 10 * time.Minute
	upstreamPriceTimeout  = 8 * time.Second
	upstreamPriceMaxBytes = 10 << 20
)

var (
	upstreamPriceCacheMu sync.Mutex
	upstreamPriceCache   = map[int]upstreamChannelPrices{}
)

// parseUpstreamPricing understands the two pricing payloads a new-api style
// upstream exposes: /api/ratio_config ({model_ratio:{}, completion_ratio:{},
// model_price:{}}) and /api/pricing ([{model_name, quota_type, ...}]).
func parseUpstreamPricing(body []byte) (*upstreamPricing, error) {
	var env struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := common.DecodeJson(bytes.NewReader(body), &env); err != nil {
		return nil, fmt.Errorf("not a pricing payload: %w", err)
	}
	if !env.Success {
		if env.Message == "" {
			env.Message = "upstream reported failure"
		}
		return nil, fmt.Errorf("%s", env.Message)
	}
	out := &upstreamPricing{modelRatio: map[string]float64{}, completionRatio: map[string]float64{}, modelPrice: map[string]float64{}}
	var cfg struct {
		ModelRatio      map[string]float64 `json:"model_ratio"`
		CompletionRatio map[string]float64 `json:"completion_ratio"`
		ModelPrice      map[string]float64 `json:"model_price"`
	}
	if err := common.Unmarshal(env.Data, &cfg); err == nil && (len(cfg.ModelRatio) > 0 || len(cfg.ModelPrice) > 0) {
		out.modelRatio, out.completionRatio, out.modelPrice = cfg.ModelRatio, cfg.CompletionRatio, cfg.ModelPrice
		if out.completionRatio == nil {
			out.completionRatio = map[string]float64{}
		}
		if out.modelPrice == nil {
			out.modelPrice = map[string]float64{}
		}
		return out, nil
	}
	var items []struct {
		ModelName       string  `json:"model_name"`
		QuotaType       int     `json:"quota_type"`
		ModelRatio      float64 `json:"model_ratio"`
		ModelPrice      float64 `json:"model_price"`
		CompletionRatio float64 `json:"completion_ratio"`
	}
	if err := common.Unmarshal(env.Data, &items); err != nil {
		return nil, fmt.Errorf("unrecognized pricing format")
	}
	for _, it := range items {
		if it.ModelName == "" {
			continue
		}
		if it.QuotaType == 1 {
			out.modelPrice[it.ModelName] = it.ModelPrice
		} else {
			out.modelRatio[it.ModelName] = it.ModelRatio
			out.completionRatio[it.ModelName] = it.CompletionRatio
		}
	}
	if len(out.modelRatio) == 0 && len(out.modelPrice) == 0 {
		return nil, fmt.Errorf("pricing payload lists no models")
	}
	return out, nil
}

func fetchUpstreamPricing(ctx context.Context, client *http.Client, base string) (*upstreamPricing, string, error) {
	var lastErr error
	for _, ep := range []string{"/api/pricing", "/api/ratio_config"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+ep, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, upstreamPriceMaxBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: %s", ep, resp.Status)
			continue
		}
		parsed, err := parseUpstreamPricing(body)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", ep, err)
			continue
		}
		return parsed, ep, nil
	}
	return nil, "", lastErr
}

func localPriceOf(modelName string) *upstreamPrice {
	if price, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return &upstreamPrice{PerCall: true, ModelPrice: price}
	}
	ratio, ok, _ := ratio_setting.GetModelRatio(modelName)
	if !ok {
		return nil
	}
	return &upstreamPrice{ModelRatio: ratio, CompletionRatio: ratio_setting.GetCompletionRatio(modelName)}
}

func (p *upstreamPricing) priceOf(modelName string) *upstreamPrice {
	if v, ok := p.modelPrice[modelName]; ok {
		return &upstreamPrice{PerCall: true, ModelPrice: v}
	}
	if v, ok := p.modelRatio[modelName]; ok {
		return &upstreamPrice{ModelRatio: v, CompletionRatio: p.completionRatio[modelName]}
	}
	return nil
}

func priceCmp(a, b float64) int {
	tol := 1e-6 * math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	switch {
	case a > b+tol:
		return 1
	case a < b-tol:
		return -1
	}
	return 0
}

// comparePrices classifies the platform price against the upstream one.
func comparePrices(local, upstream *upstreamPrice) string {
	switch {
	case local == nil:
		return "unpriced_local"
	case upstream == nil:
		return "missing_upstream"
	case local.PerCall != upstream.PerCall:
		return "type_mismatch"
	case local.PerCall:
		switch priceCmp(local.ModelPrice, upstream.ModelPrice) {
		case 1:
			return "platform_higher"
		case -1:
			return "platform_lower"
		}
		return "match"
	}
	in := priceCmp(local.ModelRatio, upstream.ModelRatio)
	out := priceCmp(local.ModelRatio*local.CompletionRatio, upstream.ModelRatio*upstream.CompletionRatio)
	switch {
	case in == 0 && out == 0:
		return "match"
	case in >= 0 && out >= 0:
		return "platform_higher"
	case in <= 0 && out <= 0:
		return "platform_lower"
	}
	return "mixed"
}

// AdminUpstreamPrices — GET /api/admin/upstream/prices?refresh=1
func AdminUpstreamPrices(c *gin.Context) {
	refresh := c.Query("refresh") == "1"
	var channels []model.Channel
	if err := model.DB.Select("id", "name", "type", "status", "models", "base_url").
		Order("id asc").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	client := &http.Client{Timeout: upstreamPriceTimeout}
	if common.TLSInsecureSkipVerify {
		client.Transport = &http.Transport{TLSClientConfig: common.InsecureTLSConfig}
	}
	now := common.GetTimestamp()
	results := make([]upstreamChannelPrices, len(channels))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range channels {
		ch := channels[i]
		base := strings.TrimRight(strings.TrimSpace(ch.GetBaseURL()), "/")
		row := upstreamChannelPrices{
			Id: ch.Id, Name: ch.Name, Type: ch.Type, TypeName: constant.GetChannelTypeName(ch.Type),
			Status: ch.Status, Models: []upstreamModelPriceRow{}, Summary: map[string]int{},
		}
		if u, err := url.Parse(base); err == nil {
			row.Host = u.Host
		}
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			row.Error = "no http base URL"
			results[i] = row
			continue
		}
		upstreamPriceCacheMu.Lock()
		cached, hit := upstreamPriceCache[ch.Id]
		upstreamPriceCacheMu.Unlock()
		if hit && !refresh && now-cached.FetchedAt < int64(upstreamPriceCacheTTL.Seconds()) {
			results[i] = cached
			continue
		}
		wg.Add(1)
		go func(i int, row upstreamChannelPrices, base string, models []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(c.Request.Context(), upstreamPriceTimeout)
			defer cancel()
			row.FetchedAt = common.GetTimestamp()
			pricing, source, err := fetchUpstreamPricing(ctx, client, base)
			if err != nil {
				row.Error = err.Error()
			} else {
				row.Ok = true
				row.Source = source
				for _, m := range models {
					m = strings.TrimSpace(m)
					if m == "" {
						continue
					}
					r := upstreamModelPriceRow{Model: m, Local: localPriceOf(m), Upstream: pricing.priceOf(m)}
					r.Status = comparePrices(r.Local, r.Upstream)
					row.Models = append(row.Models, r)
					row.Summary[r.Status]++
				}
			}
			upstreamPriceCacheMu.Lock()
			upstreamPriceCache[row.Id] = row
			upstreamPriceCacheMu.Unlock()
			results[i] = row
		}(i, row, base, ch.GetModels())
	}
	wg.Wait()
	common.ApiSuccess(c, gin.H{"fetched_at": now, "channels": results})
}
