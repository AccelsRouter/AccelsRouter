package controller

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// Upstream probe sweep: on a schedule (monitor_setting.upstream_probe_*), send
// one minimal test request per enabled channel × declared model and record
// the outcome in the upstream health rollup. Observation only: unlike the
// channel health check it never disables, enables or re-times a channel. Runs
// on the master node only so a multi-node deployment probes once.

type upstreamProbeSummary struct {
	StartedAt  int64 `json:"started_at"`
	FinishedAt int64 `json:"finished_at"`
	Channels   int   `json:"channels"`
	Probed     int   `json:"probed"`
	Failed     int   `json:"failed"`
	Skipped    int   `json:"skipped"` // unsupported channel types
}

var (
	upstreamProbeMu      sync.Mutex
	upstreamProbeRunning bool
	upstreamProbeLast    upstreamProbeSummary
	upstreamProbeOnce    sync.Once
)

// upstreamProbeStatus is what the monitor page shows about the scheduler.
func upstreamProbeStatus() gin.H {
	cfg := operation_setting.GetMonitorSetting()
	upstreamProbeMu.Lock()
	defer upstreamProbeMu.Unlock()
	return gin.H{
		"enabled":    cfg.UpstreamProbeEnabled,
		"minutes":    cfg.UpstreamProbeMinutes,
		"all_models": cfg.UpstreamProbeAllModels,
		"running":    upstreamProbeRunning,
		"last":       upstreamProbeLast,
	}
}

// runUpstreamProbeSweep probes every enabled channel's models once. Returns
// false when a sweep is already running.
func runUpstreamProbeSweep(ctx context.Context) bool {
	upstreamProbeMu.Lock()
	if upstreamProbeRunning {
		upstreamProbeMu.Unlock()
		return false
	}
	upstreamProbeRunning = true
	upstreamProbeMu.Unlock()
	summary := upstreamProbeSummary{StartedAt: common.GetTimestamp()}
	defer func() {
		summary.FinishedAt = common.GetTimestamp()
		if err := model.FlushUpstreamHealth(); err != nil {
			common.SysError("upstream probe sweep flush failed: " + err.Error())
		}
		upstreamProbeMu.Lock()
		upstreamProbeRunning = false
		upstreamProbeLast = summary
		upstreamProbeMu.Unlock()
		common.SysLog("upstream probe sweep: " + common.GetJsonString(summary))
	}()

	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		common.SysError("upstream probe sweep: " + err.Error())
		return true
	}
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.SysError("upstream probe sweep: " + err.Error())
		return true
	}
	cfg := operation_setting.GetMonitorSetting()
	type job struct {
		channel *model.Channel
		model   string
	}
	var jobs []job
	for _, ch := range channels {
		if ch.Status != common.ChannelStatusEnabled {
			continue
		}
		if lo.Contains(unsupportedTestChannelTypes, ch.Type) {
			summary.Skipped++
			continue
		}
		summary.Channels++
		if !cfg.UpstreamProbeAllModels {
			jobs = append(jobs, job{ch, resolveChannelTestModel(ch, "")})
			continue
		}
		seen := map[string]bool{}
		for _, m := range ch.GetModels() {
			m = strings.TrimSpace(m)
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			jobs = append(jobs, job{ch, m})
		}
	}
	concurrency := cfg.ChannelTestConcurrency
	if concurrency < 2 {
		concurrency = 2
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			tik := time.Now()
			result := testChannel(probeCtx, j.channel, testUserID, j.model, "", false)
			recordProbe(j.channel, result, time.Since(tik).Milliseconds())
			mu.Lock()
			summary.Probed++
			if result.localErr != nil || result.newAPIError != nil {
				summary.Failed++
			}
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return true
}

// StartUpstreamProbeScheduler ticks every minute and runs a sweep whenever
// probing is enabled and the configured interval has elapsed since the last
// one. Master node only.
func StartUpstreamProbeScheduler() {
	if !common.IsMasterNode {
		return
	}
	upstreamProbeOnce.Do(func() {
		go func() {
			// Let the process settle before the first sweep.
			time.Sleep(2 * time.Minute)
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for ; ; <-ticker.C {
				cfg := operation_setting.GetMonitorSetting()
				if !cfg.UpstreamProbeEnabled {
					continue
				}
				interval := time.Duration(cfg.UpstreamProbeMinutes * float64(time.Minute))
				if interval < 5*time.Minute {
					interval = 5 * time.Minute
				}
				upstreamProbeMu.Lock()
				last := upstreamProbeLast.StartedAt
				running := upstreamProbeRunning
				upstreamProbeMu.Unlock()
				if running || time.Since(time.Unix(last, 0)) < interval {
					continue
				}
				runUpstreamProbeSweep(context.Background())
			}
		}()
	})
}

// AdminUpstreamProbeAll — POST /api/admin/upstream/probe-all
// Starts a sweep now (in the background) unless one is already running.
func AdminUpstreamProbeAll(c *gin.Context) {
	upstreamProbeMu.Lock()
	running := upstreamProbeRunning
	upstreamProbeMu.Unlock()
	if running {
		common.ApiErrorMsg(c, "a probe sweep is already running")
		return
	}
	go runUpstreamProbeSweep(context.Background())
	common.ApiSuccess(c, gin.H{"started": true})
}
