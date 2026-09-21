package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// GetChannelPricingChannel picks a channel for a "channel pricing mode"
// user (see User.BillingMode == BillingModeChannelPricing) from the
// channels they have an explicit binding for modelName (UserChannelBinding),
// instead of the normal group-based selection in
// GetRandomSatisfiedChannel — channel-pricing-mode users have no
// functioning group, by design (the Group field is ignored entirely).
//
// The binding is per (user, channel, model): a channel bound only for a
// different model than the one requested is not a candidate here, even if
// the channel itself is otherwise capable of serving modelName — the
// per-model ratio row is what actually authorizes routing that model to
// that channel for this user (see GetUserChannelBindingRatio).
//
// Candidates are tried in a stable (ascending channel_id) order; retry
// walks that list one at a time. retry >= len(candidates) means "no more
// channels to try", mirroring how GetRandomSatisfiedChannel signals
// exhaustion by returning (nil, nil) — the caller's existing retry-until-
// RetryTimes loop handles this the same way either mode.
//
// A bound channel is only a live candidate if it's currently enabled,
// still declares support for modelName in its own model list (a binding
// doesn't force a channel to keep serving a model it was reconfigured to
// drop), and (when configured) isn't already over its daily token budget —
// binding a channel doesn't require it to be healthy, but actually routing
// live traffic to it does.
//
// The overBudget return value distinguishes, for a nil-channel result, why
// nothing was selected: true means at least one bound channel supports
// modelName but was filtered out solely for being over its daily token
// budget (a transient, "try again later" condition); false means there was
// never a supporting binding to begin with (a configuration gap). Callers
// use this to give a more specific error than a single generic message.
//
// Mirrors GetRandomSatisfiedChannel's own memory-cache split: channelsIDM
// (and the request-path filter that reads it) is only populated when
// common.MemoryCacheEnabled is true, so a disabled memory cache falls
// through to a direct DB query instead of silently seeing every candidate
// as "not found".
func GetChannelPricingChannel(userId int, modelName string, retry int, requestPath string) (channel *Channel, overBudget bool, err error) {
	channelIds, err := getUserBoundChannelIdsForModel(userId, modelName)
	if err != nil {
		return nil, false, err
	}
	if len(channelIds) == 0 {
		return nil, false, nil
	}

	if !common.MemoryCacheEnabled {
		return getChannelPricingChannelFromDB(channelIds, modelName, retry, requestPath)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	candidates := make([]int, 0, len(channelIds))
	for _, id := range channelIds {
		ch, ok := channelsIDM[id]
		if !ok || ch.Status != common.ChannelStatusEnabled {
			continue
		}
		supported := false
		for _, m := range ch.GetModels() {
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

	// Reuse the same request-path/advanced-custom filter group-based
	// selection already applies, so a channel-pricing user's bound
	// channels are held to the same bar as any other channel.
	candidates = filterChannelsByRequestPathAndModel(candidates, requestPath, modelName)
	hadCandidates := len(candidates) > 0
	candidates = filterChannelsByTokenBudget(candidates)
	overBudget = hadCandidates && len(candidates) == 0

	if retry < 0 || retry >= len(candidates) {
		return nil, overBudget, nil
	}
	return channelsIDM[candidates[retry]], false, nil
}

// GetUserBoundEnabledModels returns every model userId has an explicit
// binding for on a currently-enabled channel (channel pricing mode) — the
// model-list counterpart to GetChannelPricingChannel's channel selection,
// used so Playground/the model picker shows what this user can actually
// reach instead of anything group-based. Unlike a channel's own full model
// list, this reflects exactly the (channel, model) pairs that have a
// configured ratio — a channel bound only for gpt-4o won't also surface
// every other model that channel happens to support.
func GetUserBoundEnabledModels(userId int) ([]string, error) {
	bindings, err := GetUserChannelBindings(userId)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return []string{}, nil
	}

	channelIds := make([]int, 0, len(bindings))
	seenChannel := make(map[int]struct{}, len(bindings))
	for _, b := range bindings {
		if _, ok := seenChannel[b.ChannelId]; ok {
			continue
		}
		seenChannel[b.ChannelId] = struct{}{}
		channelIds = append(channelIds, b.ChannelId)
	}

	enabled := make(map[int]struct{}, len(channelIds))
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		for _, id := range channelIds {
			if ch, ok := channelsIDM[id]; ok && ch.Status == common.ChannelStatusEnabled {
				enabled[id] = struct{}{}
			}
		}
		channelSyncLock.RUnlock()
	} else {
		var channels []*Channel
		if err := DB.Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
			return nil, err
		}
		for _, ch := range channels {
			enabled[ch.Id] = struct{}{}
		}
	}

	seen := make(map[string]struct{}, len(bindings))
	modelsList := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if _, ok := enabled[b.ChannelId]; !ok {
			continue
		}
		if _, ok := seen[b.ModelName]; ok {
			continue
		}
		seen[b.ModelName] = struct{}{}
		modelsList = append(modelsList, b.ModelName)
	}
	return modelsList, nil
}

// getChannelPricingChannelFromDB is GetChannelPricingChannel's fallback for
// when the in-memory channel cache is disabled (common.MemoryCacheEnabled
// == false, the project's default) — queries the bound channels directly
// instead of relying on channelsIDM, which is never populated in that mode.
func getChannelPricingChannelFromDB(channelIds []int, modelName string, retry int, requestPath string) (channel *Channel, overBudget bool, err error) {
	var channels []*Channel
	if err := DB.Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, false, err
	}

	byId := make(map[int]*Channel, len(channels))
	for _, ch := range channels {
		byId[ch.Id] = ch
	}

	candidates := make([]int, 0, len(channelIds))
	for _, id := range channelIds {
		ch, ok := byId[id]
		if !ok {
			continue
		}
		supported := false
		for _, m := range ch.GetModels() {
			if m == modelName {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		if requestPath != "" && ch.Type == constant.ChannelTypeAdvancedCustom {
			config := ch.GetOtherSettings().AdvancedCustom
			if config == nil || !config.SupportsPathForModel(requestPath, modelName) {
				continue
			}
		}
		candidates = append(candidates, id)
	}
	hadCandidates := len(candidates) > 0

	// Fork: filterChannelsByTokenBudget itself reads channelsIDM (the
	// memory-cache map), which is never populated when
	// common.MemoryCacheEnabled is false — the exact case this DB-fallback
	// function exists for. Using it here would silently no-op (every
	// lookup misses, so every channel is kept regardless of its actual
	// usage). Filter directly against the *Channel objects already loaded
	// from the DB above instead.
	filtered := make([]int, 0, len(candidates))
	for _, id := range candidates {
		if ch, ok := byId[id]; ok && IsChannelOverDailyTokenBudget(ch) {
			common.SysLog(fmt.Sprintf("[DEBUG] getChannelPricingChannelFromDB: dropping channel %d, over daily token budget", id))
			continue
		}
		filtered = append(filtered, id)
	}
	candidates = filtered
	overBudget = hadCandidates && len(candidates) == 0

	if retry < 0 || retry >= len(candidates) {
		return nil, overBudget, nil
	}
	return byId[candidates[retry]], false, nil
}
