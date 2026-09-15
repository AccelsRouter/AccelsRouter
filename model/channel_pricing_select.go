package model

import (
	"github.com/QuantumNous/new-api/common"
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
func GetChannelPricingChannel(userId int, modelName string, retry int, requestPath string) (*Channel, error) {
	channelIds, err := getUserBoundChannelIds(userId)
	if err != nil {
		return nil, err
	}
	if len(channelIds) == 0 {
		return nil, nil
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
