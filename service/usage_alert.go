package service

// Abnormal usage alerts. Once an hour (by default) every active user's
// consumption today is compared with their own recent past, and the Lark alert
// group is told about a spike or a crossed limit. "Today" starts at midnight in
// the configured UTC offset (UTC by default). The rules follow LiteLLM's
// per-user spend alerts and live in pkg/larknotify (EvaluateUsage).
//
// The numbers come from quota_data, the hourly consumption table that is filled
// while data export is on (DataExportEnabled).

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/larknotify"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var (
	usageAlertOnce   sync.Once
	usageAlertMu     sync.Mutex
	usageAlertLocal  = map[string]time.Time{} // when each reported key may be forgotten, used without Redis
	usageAlertWarned bool
)

// StartUsageAlertScheduler checks once a minute whether the configured interval
// has passed and runs the check then. Master node only.
func StartUsageAlertScheduler() {
	if !common.IsMasterNode {
		return
	}
	usageAlertOnce.Do(func() {
		go func() {
			// Let the process settle, and Redis come up, before the first check.
			time.Sleep(3 * time.Minute)
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			var last time.Time
			for ; ; <-ticker.C {
				s := operation_setting.GetLarkSetting()
				if !s.UsageAlertEnabled || time.Since(last) < time.Duration(s.UsageAlertCheckMinutes)*time.Minute {
					continue
				}
				last = time.Now()
				if err := RunUsageAlertCheck(time.Now().UTC()); err != nil {
					common.SysError("usage alert check failed: " + err.Error())
				}
			}
		}()
	})
}

// RunUsageAlertCheck evaluates every active user at the given time and sends one
// card with the users not yet reported for their day or month.
func RunUsageAlertCheck(now time.Time) error {
	target := LarkAlertTarget()
	if !target.Configured() {
		return nil
	}
	if !common.DataExportEnabled {
		if !usageAlertWarned {
			usageAlertWarned = true
			common.SysLog("usage alerts are on but data export is off: the hourly usage table stays empty, so nothing can be reported")
		}
		return nil
	}

	s := operation_setting.GetLarkSetting()
	cfg := larknotify.UsageConfig{
		Multiplier:       s.UsageAlertMultiplier,
		BaselineDays:     s.UsageAlertBaselineDays,
		MinSpend:         s.UsageAlertMinSpendUSD,
		DailyThreshold:   s.UsageAlertDailyThresholdUSD,
		MonthlyThreshold: s.UsageAlertMonthlyThresholdUSD,
	}
	// Days and months start at midnight in the configured zone. The offset is a
	// whole number of hours, so every start is also the edge of an hourly bucket.
	zone := time.FixedZone("usage-alert", s.UsageAlertUTCOffsetHours*3600)
	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, zone)
	baselineStart := today.AddDate(0, 0, -cfg.BaselineDays)

	rows, err := model.QueryUserSpend(today.Unix(), monthStart.Unix(), baselineStart.Unix())
	if err != nil {
		return fmt.Errorf("query user spend: %w", err)
	}
	var events []larknotify.UsageEvent
	for _, r := range rows {
		events = append(events, larknotify.EvaluateUsage(larknotify.UserSpend{
			UserID:   r.UserID,
			Today:    r.Daily / common.QuotaPerUnit,
			Month:    r.Monthly / common.QuotaPerUnit,
			Baseline: r.Baseline / common.QuotaPerUnit,
		}, cfg, today)...)
	}

	// Report each user and kind once per day or month. A key is claimed before
	// the card goes out and given back if it cannot be delivered.
	var fresh []larknotify.UsageEvent
	for _, e := range events {
		if claimUsageAlert(e.Key, e.TTL) {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	for i := range fresh {
		if name, nameErr := model.GetUsernameById(fresh[i].User.UserID, false); nameErr == nil {
			fresh[i].User.Username = name
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := SendLarkCard(ctx, target, larknotify.UsageCard(fresh, local, cfg, LarkTranslator)); err != nil {
		for _, e := range fresh {
			releaseUsageAlert(e.Key)
		}
		return fmt.Errorf("send usage alert: %w", err)
	}
	return nil
}

// claimUsageAlert reports whether key has not been reported yet, and marks it
// as reported for ttl. With Redis the mark is shared by every instance and
// survives a restart; without it, it lives in this process only.
func claimUsageAlert(key string, ttl time.Duration) bool {
	if common.RedisEnabled && common.RDB != nil {
		if ok, err := common.RDB.SetNX(context.Background(), key, "1", ttl).Result(); err == nil {
			return ok
		}
		// Redis is in trouble: fall back to the local record rather than stay silent.
	}
	usageAlertMu.Lock()
	defer usageAlertMu.Unlock()
	now := time.Now()
	for k, until := range usageAlertLocal {
		if now.After(until) {
			delete(usageAlertLocal, k)
		}
	}
	if _, reported := usageAlertLocal[key]; reported {
		return false
	}
	usageAlertLocal[key] = now.Add(ttl)
	return true
}

// releaseUsageAlert undoes claimUsageAlert, so that the next check tries again.
func releaseUsageAlert(key string) {
	if common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Del(context.Background(), key).Err()
	}
	usageAlertMu.Lock()
	delete(usageAlertLocal, key)
	usageAlertMu.Unlock()
}
