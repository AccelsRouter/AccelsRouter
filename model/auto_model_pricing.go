// Fork-local helpers for auto virtual models and channel-pricing-mode users
// (see service/auto_model.go and service/auto_model_pricing.go).
package model

import "github.com/QuantumNous/new-api/constant"

// UserModelHasBoundChannel reports whether a channel-pricing-mode user can be
// served modelName right now: some channel the user has a binding for is
// enabled, declares the model, supports the request path and is within its
// daily token budget. It is the channel-pricing counterpart of
// GroupModelHasEnabledChannel, and the test an auto model applies to each
// candidate for such a user, so the auto model only ever picks a model the
// user can actually reach.
func UserModelHasBoundChannel(userId int, modelName string, requestPath string) bool {
	channel, _, err := GetChannelPricingChannelSeeded(userId, modelName, 0, requestPath, "")
	return err == nil && channel != nil
}

// ChannelSupportsRequestPath mirrors the distributor's check: only advanced
// custom channels restrict which paths and models they serve.
func ChannelSupportsRequestPath(channel *Channel, requestPath string, requestModel string) bool {
	if channel == nil {
		return false
	}
	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	return config != nil && config.SupportsPathForModel(requestPath, requestModel)
}

// UserChannelBindingUsable reports whether channel is a legitimate
// channel-pricing target for userId and modelName: the user has a binding for
// exactly this (channel, model) pair, and the channel still declares the
// model. Channel affinity uses it to validate a remembered channel, since the
// entry may predate a binding being removed.
func UserChannelBindingUsable(userId int, channel *Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	if _, bound := GetUserChannelBindingRatio(userId, channel.Id, modelName); !bound {
		return false
	}
	for _, m := range channel.GetModels() {
		if m == modelName {
			return true
		}
	}
	return false
}
