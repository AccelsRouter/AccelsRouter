package larknotify

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Abnormal usage alerts. The rules follow LiteLLM's per-user spend alerts:
// a spike against the user's own recent average, and optional daily and monthly
// limits. Days and months are those of the time zone the caller's "today" is in.

// UsageKind says why a user is reported.
type UsageKind string

const (
	UsageAnomaly UsageKind = "anomaly" // today's spend far above the user's own average
	UsageDaily   UsageKind = "daily"   // today's spend reached the daily limit
	UsageMonthly UsageKind = "monthly" // this month's spend reached the monthly limit
)

// UsageConfig holds the thresholds. Amounts are in USD.
type UsageConfig struct {
	Multiplier       float64 // a spike is more than this many times the daily average
	BaselineDays     int     // the average is taken over this many days before today
	MinSpend         float64 // a spike needs at least this much spent today
	DailyThreshold   float64 // 0 = off
	MonthlyThreshold float64 // 0 = off
}

// UserSpend is one user's consumption in USD.
type UserSpend struct {
	UserID   int
	Username string
	Today    float64 // since 00:00 UTC today
	Month    float64 // since the 1st of this month, 00:00 UTC, today included
	Baseline float64 // the BaselineDays days before today, today excluded
}

// UsageEvent is one reason to report a user.
type UsageEvent struct {
	Kind      UsageKind
	User      UserSpend
	Average   float64       // UsageAnomaly: the daily average over the baseline
	NoHistory bool          // UsageAnomaly: no spend at all in the baseline days
	Threshold float64       // UsageDaily, UsageMonthly
	Key       string        // identifies this user, kind and period; report it once
	TTL       time.Duration // how long to remember Key
}

// EvaluateUsage returns the reasons to report one user. today is midnight, in
// the time zone the days are counted in, of the day being checked.
//
// A spike needs the minimum spend today and then either no spend at all in the
// baseline days (a user who has just started) or more than Multiplier times the
// baseline's daily average. Days without spend count in that average.
func EvaluateUsage(u UserSpend, cfg UsageConfig, today time.Time) []UsageEvent {
	if u.Month <= 0 {
		return nil
	}
	days := max(cfg.BaselineDays, 1)
	date := today.Format("2006-01-02")
	var events []UsageEvent

	if cfg.DailyThreshold > 0 && u.Today >= cfg.DailyThreshold {
		events = append(events, UsageEvent{
			Kind: UsageDaily, User: u, Threshold: cfg.DailyThreshold,
			Key: fmt.Sprintf("usage_alert:daily:%d:%s", u.UserID, date), TTL: 24 * time.Hour,
		})
	}
	if cfg.MonthlyThreshold > 0 && u.Month >= cfg.MonthlyThreshold {
		events = append(events, UsageEvent{
			Kind: UsageMonthly, User: u, Threshold: cfg.MonthlyThreshold,
			Key: fmt.Sprintf("usage_alert:monthly:%d:%s", u.UserID, today.Format("2006-01")), TTL: 32 * 24 * time.Hour,
		})
	}
	if u.Today >= cfg.MinSpend {
		average := u.Baseline / float64(days)
		if u.Baseline <= 0 || u.Today > cfg.Multiplier*average {
			events = append(events, UsageEvent{
				Kind: UsageAnomaly, User: u, Average: average, NoHistory: u.Baseline <= 0,
				Key: fmt.Sprintf("usage_alert:anomaly:%d:%s", u.UserID, date), TTL: 24 * time.Hour,
			})
		}
	}
	return events
}

// Message keys of the usage card. They are declared again in i18n/keys.go and
// written in i18n/locales/*.yaml.
const (
	MsgUsageTitle        = "lark.usage_title"
	MsgUsageKindAnomaly  = "lark.usage_kind_anomaly"
	MsgUsageKindDaily    = "lark.usage_kind_daily"
	MsgUsageKindMonthly  = "lark.usage_kind_monthly"
	MsgUsageAnomalyValue = "lark.usage_anomaly_value"
	MsgUsageNewUserValue = "lark.usage_new_user_value"
	MsgUsageDailyValue   = "lark.usage_daily_value"
	MsgUsageMonthlyValue = "lark.usage_monthly_value"
	MsgUsageMore         = "lark.usage_more"
	MsgUsageNote         = "lark.usage_note"
)

// MaxUsageEvents is how many users one card lists; the rest are counted.
const MaxUsageEvents = 20

// UsageCard is the message for the users reported in one check. at is the time
// of the check in the zone the days are counted in; the card names that zone. Spikes come
// first, and within a kind the biggest spender first.
func UsageCard(events []UsageEvent, at time.Time, cfg UsageConfig, tr Translator) Card {
	order := map[UsageKind]int{UsageAnomaly: 0, UsageDaily: 1, UsageMonthly: 2}
	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, func(a, b UsageEvent) int {
		if a.Kind != b.Kind {
			return order[a.Kind] - order[b.Kind]
		}
		switch {
		case a.User.Today > b.User.Today:
			return -1
		case a.User.Today < b.User.Today:
			return 1
		}
		return 0
	})

	usd := func(v float64) string { return fmt.Sprintf("%.2f", v) }
	var fields []Field
	for _, e := range sorted[:min(len(sorted), MaxUsageEvents)] {
		name := strings.TrimSpace(e.User.Username)
		if name == "" {
			name = tr(MsgAlertUnknownUser, nil)
		}
		if e.User.UserID > 0 {
			name = fmt.Sprintf("%s (ID %d)", name, e.User.UserID)
		}
		var kind, value string
		switch e.Kind {
		case UsageAnomaly:
			kind = tr(MsgUsageKindAnomaly, nil)
			if e.NoHistory {
				value = tr(MsgUsageNewUserValue, map[string]any{"Today": usd(e.User.Today), "Days": cfg.BaselineDays, "MinSpend": usd(cfg.MinSpend)})
			} else {
				value = tr(MsgUsageAnomalyValue, map[string]any{
					"Today": usd(e.User.Today), "Days": cfg.BaselineDays, "Average": usd(e.Average),
					"Multiplier": cfg.Multiplier, "MinSpend": usd(cfg.MinSpend),
				})
			}
		case UsageDaily:
			kind = tr(MsgUsageKindDaily, nil)
			value = tr(MsgUsageDailyValue, map[string]any{"Spend": usd(e.User.Today), "Threshold": usd(e.Threshold)})
		case UsageMonthly:
			kind = tr(MsgUsageKindMonthly, nil)
			value = tr(MsgUsageMonthlyValue, map[string]any{"Spend": usd(e.User.Month), "Threshold": usd(e.Threshold)})
		}
		fields = append(fields, Field{Name: kind + " · " + name, Value: value})
	}
	if extra := len(sorted) - MaxUsageEvents; extra > 0 {
		fields = append(fields, Field{Name: "…", Value: tr(MsgUsageMore, map[string]any{"Count": extra})})
	}
	zone := "UTC"
	if _, offset := at.Zone(); offset != 0 {
		zone = fmt.Sprintf("UTC%+d", offset/3600)
	}
	return Card{
		Title:  tr(MsgUsageTitle, nil),
		Color:  "orange",
		Fields: fields,
		Note:   tr(MsgUsageNote, map[string]any{"Time": at.Format("2006-01-02 15:04:05"), "Zone": zone}),
	}
}
