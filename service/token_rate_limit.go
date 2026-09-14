package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/bytedance/gopkg/util/gopool"
)

// tokenRateLimitRecordTimeout bounds the async write to the limiter backend.
const tokenRateLimitRecordTimeout = 3 * time.Second

// RecordTokenRateLimitUsage feeds the actual tokens consumed by a completed
// relay request into the user- and channel-level daily token counters that
// middleware.UserTokenRateLimit and model.GetRandomSatisfiedChannel's
// token-budget filter read from. It's called once billing has already been
// settled (see PostTextConsumeQuota / PostAudioConsumeQuota /
// PostWssConsumeQuota), and is fire-and-forget: a slow or unavailable
// limiter backend must never delay or fail a response that's already been
// sent to the caller.
func RecordTokenRateLimitUsage(relayInfo *relaycommon.RelayInfo, totalTokens int) {
	if relayInfo == nil || totalTokens <= 0 {
		return
	}
	userId := relayInfo.UserId
	channelId := relayInfo.ChannelId

	recordUser := setting.UserDailyTokenLimitEnabled && userId > 0
	recordChannel := setting.ChannelDailyTokenLimitEnabled && channelId > 0
	if !recordUser && !recordChannel {
		return
	}

	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), tokenRateLimitRecordTimeout)
		defer cancel()

		if recordUser {
			key := setting.UserDailyTokenLimitKey(userId)
			if _, err := limiter.AddDailyTokens(ctx, key, int64(totalTokens)); err != nil {
				logger.LogError(ctx, "failed to record user daily token usage for rate limit: "+err.Error())
			}
		}
		if recordChannel {
			key := setting.ChannelDailyTokenLimitKey(channelId)
			if _, err := limiter.AddDailyTokens(ctx, key, int64(totalTokens)); err != nil {
				logger.LogError(ctx, "failed to record channel daily token usage for rate limit: "+err.Error())
			}
		}
	})
}
