// Fork-local availability probe for auto virtual models (see
// setting/auto_model.go). Lives in package model to reuse the in-memory
// channel cache guarded by channelSyncLock.
package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// GroupModelHasEnabledChannel reports whether at least one enabled channel can
// serve model for group (path-aware, mirroring GetRandomSatisfiedChannel's
// lookup: exact model name first, then the normalized matching name). Used by
// auto-model resolution to skip candidates that cannot be served right now.
//
// A channel that has used up its daily token budget cannot serve the model
// either (see IsChannelOverDailyTokenBudget): counting it would make an auto
// model pick a model whose every channel is exhausted. This is the same test
// channel selection and channel-pricing availability apply.
func GroupModelHasEnabledChannel(group string, model string, requestPath string) bool {
	if group == "" || model == "" {
		return false
	}
	if !common.MemoryCacheEnabled {
		if !setting.ChannelDailyTokenLimitEnabled {
			var count int64
			DB.Table("abilities").
				Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
				Count(&count)
			return count > 0
		}
		// Daily budgets are on: look at the channels themselves, directly
		// (filterChannelsByTokenBudget reads the memory cache, which is empty
		// in this mode).
		var channelIds []int
		if err := DB.Table("abilities").
			Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
			Pluck("channel_id", &channelIds).Error; err != nil || len(channelIds) == 0 {
			return false
		}
		var channels []*Channel
		if err := DB.Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
			return false
		}
		for _, ch := range channels {
			if !IsChannelOverDailyTokenBudget(ch) {
				return true
			}
		}
		return false
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if len(filterChannelsByTokenBudget(filterChannelsByRequestPathAndModel(group2model2channels[group][model], requestPath, model))) > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(model)
	if normalized == model {
		return false
	}
	return len(filterChannelsByTokenBudget(filterChannelsByRequestPathAndModel(group2model2channels[group][normalized], requestPath, model))) > 0
}
