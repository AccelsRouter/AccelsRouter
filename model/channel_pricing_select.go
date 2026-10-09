package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/smartroute"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
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
// Candidates are ordered by smart routing (see common/smartroute): healthy
// channels first, then a random draw weighted toward the cheaper binding
// ratio. retry walks that order (0 = first); retry >= len(ranked) means
// "no more channels to try", mirroring how GetRandomSatisfiedChannel
// signals exhaustion.
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
	return GetChannelPricingChannelSeeded(userId, modelName, retry, requestPath, "")
}

// GetChannelPricingChannelSeeded is GetChannelPricingChannel with a
// per-request seed (the request ID); see GetChannelSeeded. The initial
// attempt and every retry must pass the same seed.
func GetChannelPricingChannelSeeded(userId int, modelName string, retry int, requestPath string, seed string) (channel *Channel, overBudget bool, err error) {
	channelIds, err := getUserBoundChannelIdsForModel(userId, modelName)
	if err != nil {
		return nil, false, err
	}
	if len(channelIds) == 0 {
		return nil, false, nil
	}

	if !common.MemoryCacheEnabled {
		return getChannelPricingChannelFromDB(userId, channelIds, modelName, retry, requestPath, seed)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	candidateIds := make([]int, 0, len(channelIds))
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
		candidateIds = append(candidateIds, id)
	}

	candidateIds = filterChannelsByRequestPathAndModel(candidateIds, requestPath, modelName)
	hadCandidates := len(candidateIds) > 0
	candidateIds = filterChannelsByTokenBudget(candidateIds)
	overBudget = hadCandidates && len(candidateIds) == 0
	if len(candidateIds) == 0 {
		return nil, overBudget, nil
	}

	var ranked []int
	if setting.SmartRoutingEnabled {
		candidates := make([]smartroute.Candidate, 0, len(candidateIds))
		for _, id := range candidateIds {
			ratio, _ := GetUserChannelBindingRatio(userId, id, modelName)
			candidates = append(candidates, smartroute.Candidate{ChannelId: id, Price: ratio})
		}
		ranked = smartroute.RankChannels(candidates, modelName, seed)
	} else {
		// Legacy (setting.SmartRoutingEnabled off): candidateIds is
		// already in ascending channel_id order (see
		// getUserBoundChannelIdsForModel), so it doubles as the "ranked"
		// list unchanged — the exact pre-smart-routing ordering.
		ranked = candidateIds
	}

	if retry < 0 || retry >= len(ranked) {
		return nil, overBudget, nil
	}
	return channelsIDM[ranked[retry]], false, nil
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
func getChannelPricingChannelFromDB(userId int, channelIds []int, modelName string, retry int, requestPath string, seed string) (channel *Channel, overBudget bool, err error) {
	var channels []*Channel
	if err := DB.Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, false, err
	}

	byId := make(map[int]*Channel, len(channels))
	for _, ch := range channels {
		byId[ch.Id] = ch
	}

	candidateIds := make([]int, 0, len(channelIds))
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
		candidateIds = append(candidateIds, id)
	}
	hadCandidates := len(candidateIds) > 0

	// Fork: filterChannelsByTokenBudget itself reads channelsIDM (the
	// memory-cache map), which is never populated when
	// common.MemoryCacheEnabled is false — the exact case this DB-fallback
	// function exists for. Using it here would silently no-op (every
	// lookup misses, so every channel is kept regardless of its actual
	// usage). Filter directly against the *Channel objects already loaded
	// from the DB above instead.
	filtered := make([]int, 0, len(candidateIds))
	for _, id := range candidateIds {
		if ch, ok := byId[id]; ok && IsChannelOverDailyTokenBudget(ch) {
			common.SysLog(fmt.Sprintf("[DEBUG] getChannelPricingChannelFromDB: dropping channel %d, over daily token budget", id))
			continue
		}
		filtered = append(filtered, id)
	}
	candidateIds = filtered
	overBudget = hadCandidates && len(candidateIds) == 0
	if len(candidateIds) == 0 {
		return nil, overBudget, nil
	}

	var ranked []int
	if setting.SmartRoutingEnabled {
		candidates := make([]smartroute.Candidate, 0, len(candidateIds))
		for _, id := range candidateIds {
			ratio, _ := GetUserChannelBindingRatio(userId, id, modelName)
			candidates = append(candidates, smartroute.Candidate{ChannelId: id, Price: ratio})
		}
		ranked = smartroute.RankChannels(candidates, modelName, seed)
	} else {
		// Legacy (setting.SmartRoutingEnabled off): candidateIds is
		// already in ascending channel_id order (see
		// getUserBoundChannelIdsForModel), so it doubles as the "ranked"
		// list unchanged — the exact pre-smart-routing ordering.
		ranked = candidateIds
	}

	if retry < 0 || retry >= len(ranked) {
		return nil, overBudget, nil
	}
	return byId[ranked[retry]], false, nil
}
