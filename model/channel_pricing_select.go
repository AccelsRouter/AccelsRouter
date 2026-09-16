package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// GetChannelPricingChannel picks a channel for a "channel pricing mode"
// user (see User.BillingMode == BillingModeChannelPricing) from their bound
// channels (UserChannelBinding), instead of the normal group-based
// selection in GetRandomSatisfiedChannel — channel-pricing-mode users have
// no functioning group, by design (the Group field is ignored entirely).
//
// Bound channels are tried in a stable (ascending channel_id) order; retry
// walks that list one at a time. retry >= len(candidates) means "no more
// channels to try", mirroring how GetRandomSatisfiedChannel signals
// exhaustion by returning (nil, nil) — the caller's existing retry-until-
// RetryTimes loop handles this the same way either mode.
//
// A bound channel is only a live candidate if it's currently enabled,
// declares support for modelName, and (when configured) isn't already over
// its daily token budget — binding a channel doesn't require it to be
// healthy, but actually routing live traffic to it does.
//
// Mirrors GetRandomSatisfiedChannel's own memory-cache split: channelsIDM
// (and the request-path filter that reads it) is only populated when
// common.MemoryCacheEnabled is true, so a disabled memory cache falls
// through to a direct DB query instead of silently seeing every candidate
// as "not found".
func GetChannelPricingChannel(userId int, modelName string, retry int, requestPath string) (*Channel, error) {
	channelIds, err := getUserBoundChannelIds(userId)
	if err != nil {
		return nil, err
	}
	if len(channelIds) == 0 {
		return nil, nil
	}

	if !common.MemoryCacheEnabled {
		return getChannelPricingChannelFromDB(channelIds, modelName, retry, requestPath)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	candidates := make([]int, 0, len(channelIds))
	for _, id := range channelIds {
		channel, ok := channelsIDM[id]
		if !ok || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		supported := false
		for _, m := range channel.GetModels() {
			if m == modelName {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		candidates = append(candidates, id)
	}

	// Reuse the same request-path/advanced-custom and daily-token-budget
	// filters group-based selection already applies, so a channel-pricing
	// user's bound channels are held to the same bar as any other channel.
	candidates = filterChannelsByRequestPathAndModel(candidates, requestPath, modelName)
	candidates = filterChannelsByTokenBudget(candidates)

	if retry < 0 || retry >= len(candidates) {
		return nil, nil
	}
	return channelsIDM[candidates[retry]], nil
}

// getChannelPricingChannelFromDB is GetChannelPricingChannel's fallback for
// when the in-memory channel cache is disabled (common.MemoryCacheEnabled
// == false, the project's default) — queries the bound channels directly
// instead of relying on channelsIDM, which is never populated in that mode.
func getChannelPricingChannelFromDB(channelIds []int, modelName string, retry int, requestPath string) (*Channel, error) {
	var channels []*Channel
	if err := DB.Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}

	byId := make(map[int]*Channel, len(channels))
	for _, ch := range channels {
		byId[ch.Id] = ch
	}

	candidates := make([]int, 0, len(channelIds))
	for _, id := range channelIds {
		channel, ok := byId[id]
		if !ok {
			continue
		}
		supported := false
		for _, m := range channel.GetModels() {
			if m == modelName {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
			config := channel.GetOtherSettings().AdvancedCustom
			if config == nil || !config.SupportsPathForModel(requestPath, modelName) {
				continue
			}
		}
		candidates = append(candidates, id)
	}

	candidates = filterChannelsByTokenBudget(candidates)

	if retry < 0 || retry >= len(candidates) {
		return nil, nil
	}
	return byId[candidates[retry]], nil
}
