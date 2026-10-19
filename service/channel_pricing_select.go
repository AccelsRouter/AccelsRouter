package service

import (
	"github.com/QuantumNous/new-api/model"
)

// CacheGetChannelPricingChannel is the channel-pricing-mode counterpart to
// CacheGetRandomSatisfiedChannel: it picks a channel from the user's own
// bound channels (model.UserChannelBinding) instead of a group. Callers
// (controller.getChannel) are expected to have already checked
// model.User.BillingMode == model.BillingModeChannelPricing and be retrying
// the same way they would for a normal group-based selection
// (param.GetRetry(), incrementing on each attempt until common.RetryTimes).
//
// The overBudget return value distinguishes, for a nil-channel result,
// whether a bound channel exists that supports the model but was over its
// daily token budget (true) versus no supporting binding existing at all
// (false) — see model.GetChannelPricingChannel.
// channelPricingSeeded is indirected so tests can stub the ranked selection.
var channelPricingSeeded = model.GetChannelPricingChannelSeeded

func CacheGetChannelPricingChannel(userId int, param *RetryParam) (channel *model.Channel, overBudget bool, err error) {
	seed := smartRouteSeed(param.Ctx)
	used := usedChannelIds(param.Ctx)
	if len(used) == 0 {
		return channelPricingSeeded(userId, param.ModelName, param.GetRetry(), param.RequestPath, seed)
	}
	// Fork: the first attempt may have used a channel that channel affinity
	// picked ahead of the ordering, so "retry N" no longer means "the Nth
	// ranked channel". Take the first ranked channel this request has not
	// tried yet instead. The ranking is seeded, so it is the same on every
	// call, and it ends with nil when the candidates run out.
	for retry := 0; ; retry++ {
		channel, overBudget, err = channelPricingSeeded(userId, param.ModelName, retry, param.RequestPath, seed)
		if err != nil || channel == nil {
			return channel, overBudget, err
		}
		if !used[channel.Id] {
			return channel, false, nil
		}
	}
}
