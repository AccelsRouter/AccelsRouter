package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// tokenRateLimitCheckTimeout bounds how long the pre-flight Redis peek may
// take. If the limiter backend is slow/unreachable we fail OPEN (let the
// request through) rather than turn a limiter outage into a full outage.
const tokenRateLimitCheckTimeout = 2 * time.Second

// UserTokenRateLimit rejects a request with 429 BEFORE it is dispatched to a
// channel if the requesting user has already exhausted their own configured
// daily token budget (resets at 00:00 UTC). The budget is a per-user value
// set by an admin on the user record (model.User.DailyTokenLimit), not a
// group-wide setting.
//
// This is deliberately separate from ModelRequestRateLimit, which counts
// REQUESTS. This one counts actual TOKENS (prompt+completion), recorded
// after each response completes by service.RecordTokenRateLimitUsage — the
// token count for the request in flight isn't known until then, so this
// middleware only peeks the already-accumulated total for today.
//
// Must run after TokenAuth/UserAuth so the user context keys are populated.
func UserTokenRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !setting.UserDailyTokenLimitEnabled {
			logger.LogInfo(c.Request.Context(), "[DEBUG] UserTokenRateLimit: UserDailyTokenLimitEnabled=false, skipping check entirely")
			c.Next()
			return
		}

		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}

		limitTokens, _ := common.GetContextKeyType[int64](c, constant.ContextKeyUserDailyTokenLimit)
		if limitTokens <= 0 {
			// 0/unset on the user record means unlimited.
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("[DEBUG] UserTokenRateLimit: userId=%d has no daily_token_limit set (value=%d), treating as unlimited", userId, limitTokens))
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), tokenRateLimitCheckTimeout)
		defer cancel()
		count, err := limiter.PeekDailyTokens(ctx, setting.UserDailyTokenLimitKey(userId))
		if err != nil {
			// Fail open: a limiter-backend hiccup must not take every user's
			// traffic down.
			logger.LogError(c.Request.Context(), "user daily token limit check failed: "+err.Error())
			c.Next()
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("[DEBUG] UserTokenRateLimit: userId=%d today's usage=%d limit=%d key=%s", userId, count, limitTokens, setting.UserDailyTokenLimitKey(userId)))
		if count >= limitTokens {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitDailyTokenReached, map[string]any{
				"Max": limitTokens,
			}))
			return
		}
		c.Next()
	}
}
