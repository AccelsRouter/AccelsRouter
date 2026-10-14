package setting

import (
	"fmt"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// maxRateLimitDurationSeconds is the largest window the count cap is computed
// against (24h). Token-bucket capacity is count*duration; this keeps that
// product inside int64 when the window is at most a day.
const maxRateLimitDurationSeconds = 24 * 60 * 60

// maxModelRequestRateLimitCount is math.MaxInt64 / maxRateLimitDurationSeconds.
// It is the largest count that cannot overflow int64(count)*duration for a
// window of at most 24 hours.
const maxModelRequestRateLimitCount int64 = math.MaxInt64 / maxRateLimitDurationSeconds

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog(common.LogText("error marshalling model ratio: %s", err.Error()))
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	ModelRequestRateLimitGroup = make(map[string][2]int)
	return common.Unmarshal([]byte(jsonStr), &ModelRequestRateLimitGroup)
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	checkModelRequestRateLimitGroup := make(map[string][2]int)
	err := common.Unmarshal([]byte(jsonStr), &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if int64(limits[0]) > maxModelRequestRateLimitCount || int64(limits[1]) > maxModelRequestRateLimitCount {
			return fmt.Errorf("group %s [%d, %d] exceeds max rate limit %d", group, limits[0], limits[1], maxModelRequestRateLimitCount)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Daily token quota limiting.
//
// Unlike ModelRequestRateLimit* above (which counts REQUESTS per minute
// window), this counts actual TOKENS (prompt+completion) consumed per
// calendar day, reset at 00:00 UTC (see common/limiter.AddDailyTokens /
// PeekDailyTokens):
//
//   - User side: each user has their own daily token budget
//     (model.User.DailyTokenLimit, set by an admin on the user record; 0 =
//     unlimited). Checked by middleware.UserTokenRateLimit BEFORE a request
//     is dispatched; over budget -> reject with 429 immediately.
//   - Channel side: a per-channel daily token budget set on the channel
//     itself (dto.ChannelSettings.DailyTokenLimit). Checked by
//     model.GetRandomSatisfiedChannel while selecting a channel; a channel
//     over budget is skipped in favor of the next channel/priority tier (and,
//     for "auto" group combinations, the next group) instead of erroring.
//
// Both sides record actual usage the same way, after a response completes:
// see service.RecordTokenRateLimitUsage.
// ---------------------------------------------------------------------------

var UserDailyTokenLimitEnabled = false

var ChannelDailyTokenLimitEnabled = false

// UserDailyTokenLimitKey and ChannelDailyTokenLimitKey build the counter
// keys shared by middleware.UserTokenRateLimit, model.GetRandomSatisfiedChannel
// and service.RecordTokenRateLimitUsage, so all three always agree on where
// a given user/channel's daily usage is tracked.
func UserDailyTokenLimitKey(userId int) string {
	return fmt.Sprintf("dailyTokenLimit:v1:user:%d", userId)
}

func ChannelDailyTokenLimitKey(channelId int) string {
	return fmt.Sprintf("dailyTokenLimit:v1:channel:%d", channelId)
}

// ---------------------------------------------------------------------------
// Smart routing.
//
// Replaces channel/ability Priority and Weight as the mechanism for
// choosing among several channels serving the same model, with a
// composite score (success rate, latency, and — in channel-pricing mode
// only — price) computed from rolling per-(channel, model) stats kept in
// Redis (see common/smartroute). Applies to both group-mode selection
// (model.GetRandomSatisfiedChannel / model.GetChannel) and
// channel-pricing-mode selection (model.GetChannelPricingChannel); reseller
// routing (service.SelectResellerChannel) is untouched regardless of this
// setting.
//
// Defaults to true — this is meant to take effect automatically, not as an
// opt-in a user has to request. The switch exists purely as an operator
// escape hatch: turning it off reverts every affected selection path to
// its exact pre-smart-routing behavior — priority tiers + weighted-random
// in group mode, ascending channel_id order in channel-pricing mode, and
// channel affinity's usability check without the consecutive-failure
// circuit breaker (see common/smartroute.IsCircuitOpen) — entirely
// without needing a rollback deploy.
var SmartRoutingEnabled = false
