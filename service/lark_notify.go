package service

// Lark (Feishu) notifications. The bots and their secrets come from the admin
// settings page (setting/operation_setting/lark_setting.go). The old
// LARK_ENTERPRISE_WEBHOOK_URL environment variable still works for the
// enterprise bot until the page has a value.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/larknotify"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// LarkEnterpriseWebhookEnv is the environment variable the enterprise bot was
// configured with before it moved to the admin page.
const LarkEnterpriseWebhookEnv = "LARK_ENTERPRISE_WEBHOOK_URL"

var larkSender = larknotify.Sender{Marshal: common.Marshal, Unmarshal: common.Unmarshal}

// LarkTranslator renders the text of Lark cards. They are notifications with no
// web console in between, so the backend translates them, in DEFAULT_LANGUAGE.
var LarkTranslator larknotify.Translator = func(key string, params map[string]any) string {
	return i18n.Translate("", key, params)
}

// LarkEnterpriseTarget is the bot for enterprise inquiries and refund requests.
func LarkEnterpriseTarget() larknotify.Target {
	s := operation_setting.GetLarkSetting()
	if hook := strings.TrimSpace(s.EnterpriseWebhookURLSecret); hook != "" {
		return larknotify.Target{WebhookURL: hook, Secret: strings.TrimSpace(s.EnterpriseSignSecret)}
	}
	return larknotify.Target{WebhookURL: strings.TrimSpace(os.Getenv(LarkEnterpriseWebhookEnv))}
}

// LarkAlertTarget is the bot for upstream channel failure alerts.
func LarkAlertTarget() larknotify.Target {
	s := operation_setting.GetLarkSetting()
	return larknotify.Target{
		WebhookURL: strings.TrimSpace(s.AlertWebhookURLSecret),
		Secret:     strings.TrimSpace(s.AlertSignSecret),
	}
}

// SendLarkRaw posts an already built card object.
func SendLarkRaw(ctx context.Context, t larknotify.Target, card map[string]any) error {
	return larkSender.SendRaw(ctx, t, card)
}

// SendLarkCard posts a card.
func SendLarkCard(ctx context.Context, t larknotify.Target, card larknotify.Card) error {
	return larkSender.Send(ctx, t, card)
}

var upstreamFailureTracker = larknotify.NewTracker(func() larknotify.Config {
	s := operation_setting.GetLarkSetting()
	return larknotify.Config{
		Window:    time.Duration(s.AlertWindowMinutes) * time.Minute,
		Threshold: s.AlertFailureThreshold,
		Mute:      time.Duration(s.AlertMuteMinutes) * time.Minute,
	}
})

// ReportUpstreamFailure counts one failed relay attempt for its channel and
// model. When they have failed often enough (by default 3 times within 5
// minutes) it sends one alert to the alert group, and then stays quiet about
// that channel and model for a while (by default 10 minutes).
//
// Only failures that point at the upstream count: timeouts, 5xx, rate limiting,
// rejected credentials. A caller's invalid request or empty account does not.
// It is cheap and never blocks the request: the card goes out on a goroutine.
//
// The counting is per process. With several instances each one counts the
// failures it saw itself.
func ReportUpstreamFailure(c *gin.Context, ch types.ChannelError, err *types.NewAPIError, upstreamURLs []string) {
	if c == nil || err == nil || ch.ChannelId <= 0 {
		return
	}
	if !operation_setting.GetLarkSetting().AlertEnabled {
		return
	}
	target := LarkAlertTarget()
	if !target.Configured() {
		return
	}
	// The caller hung up. That says nothing about the channel.
	if c.Request != nil && c.Request.Context().Err() != nil {
		return
	}
	code := string(err.GetErrorCode())
	if !larknotify.IsUpstreamFailure(err.StatusCode, code, types.IsChannelError(err)) {
		return
	}

	alert, fire := upstreamFailureTracker.Record(larknotify.Sample{
		Channel: larknotify.Channel{
			ID:   ch.ChannelId,
			Name: ch.ChannelName,
			Type: constant.GetChannelTypeName(ch.ChannelType),
		},
		Model: c.GetString("original_model"),
		Caller: larknotify.Caller{
			UserID:    c.GetInt("id"),
			Username:  c.GetString("username"),
			TokenName: c.GetString("token_name"),
			Group:     c.GetString("group"),
		},
		StatusCode: err.StatusCode,
		ErrorCode:  code,
		Message:    err.MaskSensitiveErrorWithStatusCode(upstreamURLs...),
		RequestID:  c.GetString(common.RequestIdKey),
		At:         time.Now(),
	})
	if !fire {
		return
	}
	card := larknotify.AlertCard(alert, LarkTranslator)
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if sendErr := larkSender.Send(ctx, target, card); sendErr != nil {
			logger.LogError(ctx, fmt.Sprintf("lark channel alert was not delivered: %s", sendErr.Error()))
		}
	})
}
