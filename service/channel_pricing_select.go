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
func CacheGetChannelPricingChannel(userId int, param *RetryParam) (*model.Channel, error) {
	return model.GetChannelPricingChannel(userId, param.ModelName, param.GetRetry(), param.RequestPath)
}
