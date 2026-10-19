package service

// Auto models and channel affinity for channel-pricing-mode users (fork-local).
//
// A channel-pricing user has no usable group: requests go to the channels the
// admin bound to the user (model.UserChannelBinding). Two things follow:
//
//   - an auto model picks among the models the user has a bound, enabled
//     channel for (see ResolveAutoModelForUser);
//   - channel affinity applies to the user's bound channels, and outranks
//     smart routing's ordering: a conversation keeps its channel while it is
//     still usable (see PricingAffinityChannel).

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/smartroute"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

const ginKeyAutoPricingUser = "auto_pricing_user"

// userModelBound is indirected so tests can stub the bindings.
var userModelBound = model.UserModelHasBoundChannel

// autoPricingUserID is the channel-pricing user an auto model was resolved
// for, or 0 for a group-routed request.
func autoPricingUserID(c *gin.Context) int {
	if c == nil {
		return 0
	}
	return c.GetInt(ginKeyAutoPricingUser)
}

// pricingAffinityUsable reports whether a remembered channel may be used for a
// channel-pricing user: enabled, within its daily token budget, circuit closed
// (with smart routing on), serving the request path, and still bound to the
// user for this model.
func pricingAffinityUsable(userId int, preferred *model.Channel, modelName, requestPath string) bool {
	if preferred == nil || preferred.Status != common.ChannelStatusEnabled ||
		model.IsChannelOverDailyTokenBudget(preferred) {
		return false
	}
	if setting.SmartRoutingEnabled && smartroute.IsCircuitOpen(preferred.Id, modelName) {
		return false
	}
	return model.ChannelSupportsRequestPath(preferred, requestPath, modelName) &&
		model.UserChannelBindingUsable(userId, preferred, modelName)
}

// PricingAffinityChannel applies channel affinity for a channel-pricing user:
// it returns the conversation's remembered channel when that channel is still
// usable, or nil, in which case the caller selects a channel as usual. An
// affinity entry that is no longer usable is cleared, as in
// SelectChannelForRequest.
func PricingAffinityChannel(c *gin.Context, userId int, modelName, requestPath string) *model.Channel {
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	channelID, found := GetPreferredChannelByAffinity(c, modelName, usingGroup)
	if !found {
		return nil
	}
	preferred, err := model.CacheGetChannel(channelID)
	if err == nil && pricingAffinityUsable(userId, preferred, modelName, requestPath) {
		MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
		return preferred
	}
	if !ShouldKeepChannelAffinityOnChannelDisabled() {
		ClearCurrentChannelAffinityCache(c)
	}
	return nil
}

// usedChannelIds is the set of channels this request has already been sent
// to (the "use_channel" trail the relay loop appends to on every attempt, the
// first one included). A channel-pricing retry skips them, so a retry never
// lands on the channel that just failed, including a channel that affinity
// picked ahead of the ordering.
func usedChannelIds(c *gin.Context) map[int]bool {
	if c == nil {
		return nil
	}
	used := make(map[int]bool)
	for _, raw := range c.GetStringSlice("use_channel") {
		if id, err := strconv.Atoi(raw); err == nil {
			used[id] = true
		}
	}
	return used
}
