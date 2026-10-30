package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// LarkSetting holds the Lark (Feishu) bot settings, so nothing about Lark has to
// be set through environment variables.
//
// Webhook addresses and signing secrets are secrets. Their option keys end in
// "secret", so the generic option list skips them; GetOptions lists these four
// by hand, for the root administrator only, so the settings page can show them.
type LarkSetting struct {
	// The bot that receives enterprise inquiries and refund requests.
	EnterpriseWebhookURLSecret string `json:"enterprise_webhook_url_secret"`
	EnterpriseSignSecret       string `json:"enterprise_sign_secret"`

	// The bot that receives upstream channel failure alerts.
	AlertEnabled          bool   `json:"alert_enabled"`
	AlertWebhookURLSecret string `json:"alert_webhook_url_secret"`
	AlertSignSecret       string `json:"alert_sign_secret"`
	AlertWindowMinutes    int    `json:"alert_window_minutes"`    // failures are counted within this long
	AlertFailureThreshold int    `json:"alert_failure_threshold"` // this many failures raise an alert
	AlertMuteMinutes      int    `json:"alert_mute_minutes"`      // quiet time after an alert

	// Abnormal usage alerts, sent to the alert bot. Amounts are in USD; days and
	// months start at midnight in the UTC offset set below (UTC by default).
	UsageAlertEnabled             bool    `json:"usage_alert_enabled"`
	UsageAlertMultiplier          float64 `json:"usage_alert_multiplier"`            // a spike is more than this many times the user's daily average
	UsageAlertBaselineDays        int     `json:"usage_alert_baseline_days"`         // the average is taken over this many days before today
	UsageAlertMinSpendUSD         float64 `json:"usage_alert_min_spend_usd"`         // a spike needs at least this much spent today
	UsageAlertDailyThresholdUSD   float64 `json:"usage_alert_daily_threshold_usd"`   // 0 = off
	UsageAlertMonthlyThresholdUSD float64 `json:"usage_alert_monthly_threshold_usd"` // 0 = off
	UsageAlertCheckMinutes        int     `json:"usage_alert_check_minutes"`
	UsageAlertUTCOffsetHours      int     `json:"usage_alert_utc_offset_hours"` // days and months start at midnight in this time zone; 0 = UTC
}

const (
	DefaultLarkAlertWindowMinutes    = 5
	DefaultLarkAlertFailureThreshold = 3
	DefaultLarkAlertMuteMinutes      = 10

	DefaultUsageAlertMultiplier   = 3.0
	DefaultUsageAlertBaselineDays = 7
	DefaultUsageAlertMinSpendUSD  = 10.0
	DefaultUsageAlertCheckMinutes = 60
)

var larkSetting = LarkSetting{
	AlertWindowMinutes:    DefaultLarkAlertWindowMinutes,
	AlertFailureThreshold: DefaultLarkAlertFailureThreshold,
	AlertMuteMinutes:      DefaultLarkAlertMuteMinutes,

	UsageAlertMultiplier:   DefaultUsageAlertMultiplier,
	UsageAlertBaselineDays: DefaultUsageAlertBaselineDays,
	UsageAlertMinSpendUSD:  DefaultUsageAlertMinSpendUSD,
	UsageAlertCheckMinutes: DefaultUsageAlertCheckMinutes,
}

func init() {
	config.GlobalConfig.Register("lark_setting", &larkSetting)
}

// GetLarkSetting returns a copy with the numbers brought into range, so a
// zero or absurd value saved by mistake cannot silence or flood the group.
func GetLarkSetting() *LarkSetting {
	s := larkSetting
	s.AlertWindowMinutes = clampLark(s.AlertWindowMinutes, 1, 1440, DefaultLarkAlertWindowMinutes)
	s.AlertFailureThreshold = clampLark(s.AlertFailureThreshold, 1, 1000, DefaultLarkAlertFailureThreshold)
	s.AlertMuteMinutes = clampLark(s.AlertMuteMinutes, 1, 10080, DefaultLarkAlertMuteMinutes)
	if s.UsageAlertMultiplier <= 0 {
		s.UsageAlertMultiplier = DefaultUsageAlertMultiplier
	}
	s.UsageAlertBaselineDays = clampLark(s.UsageAlertBaselineDays, 1, 90, DefaultUsageAlertBaselineDays)
	if s.UsageAlertMinSpendUSD <= 0 {
		s.UsageAlertMinSpendUSD = DefaultUsageAlertMinSpendUSD
	}
	s.UsageAlertDailyThresholdUSD = max(s.UsageAlertDailyThresholdUSD, 0)
	s.UsageAlertMonthlyThresholdUSD = max(s.UsageAlertMonthlyThresholdUSD, 0)
	s.UsageAlertCheckMinutes = clampLark(s.UsageAlertCheckMinutes, 5, 1440, DefaultUsageAlertCheckMinutes)
	s.UsageAlertUTCOffsetHours = max(min(s.UsageAlertUTCOffsetHours, 14), -12)
	return &s
}

// clampLark returns def for values below min, and max for values above it.
func clampLark(v, min, max, def int) int {
	switch {
	case v < min:
		return def
	case v > max:
		return max
	}
	return v
}
