package setting

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := json.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	ModelRequestRateLimitGroup = make(map[string][2]int)
	return json.Unmarshal([]byte(jsonStr), &ModelRequestRateLimitGroup)
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
	err := json.Unmarshal([]byte(jsonStr), &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
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
