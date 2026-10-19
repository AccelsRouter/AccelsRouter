package service

// Channel affinity for auto models (fork-local).
//
// Channel affinity remembers which channel served a conversation, keyed by the
// model the request asked for. An auto model request asks for a virtual name,
// so before it is resolved nothing says which model, and so which affinity
// entry, to look at. Two things close that gap:
//
//   - after a successful request, RecordAutoModelAffinity remembers the model
//     the request settled on, under a key built from the same affinity rule
//     and the same key value (header, body field, ...) that the channel
//     affinity entry uses;
//   - at the next request, ResolveAutoModel looks that model up first. If its
//     affinity channel is still usable the request goes straight back to the
//     model, and from there to the channel, with nothing classified. Only a
//     request that does not satisfy channel affinity is classified.
//
// The probe, the peek and the usability check below deliberately repeat the
// matching of GetPreferredChannelByAffinity and of the affinity branch of
// SelectChannelForRequest without their side effects (no request-context
// writes, no cache clearing), because they run before the model is known.
// Keep them in step with those two functions.

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/autoroute"
	"github.com/QuantumNous/new-api/common/smartroute"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// autoAffinityChannelUsable is indirected so tests can stub channel state.
var autoAffinityChannelUsable = affinityChannelUsable

type autoAffinityProbe struct {
	ok           bool
	stickySuffix string // cache key (without namespace) of the conversation's model
	ttlSeconds   int
}

// probeAutoAffinity finds the channel affinity rule that applies to an auto
// model request and builds the key under which the conversation's model is
// remembered. A rule applies when it matches the virtual name or any of the
// models the name can resolve to, because the rule that will later govern the
// chosen model is not known yet. ok is false when affinity is off, no rule
// applies, or the request carries no affinity key.
func probeAutoAffinity(c *gin.Context, autoName, usingGroup string, candidates []string) autoAffinityProbe {
	affinity := operation_setting.GetChannelAffinitySetting()
	if affinity == nil || !affinity.Enabled || c == nil || c.Request == nil || c.Request.URL == nil {
		return autoAffinityProbe{}
	}
	path := c.Request.URL.Path
	userAgent := c.Request.UserAgent()
	for _, rule := range affinity.Rules {
		if !ruleMatchesAnyModel(rule.ModelRegex, autoName, candidates) {
			continue
		}
		if len(rule.PathRegex) > 0 && !matchAnyRegexCached(rule.PathRegex, path) {
			continue
		}
		if len(rule.UserAgentInclude) > 0 && !matchAnyIncludeFold(rule.UserAgentInclude, userAgent) {
			continue
		}
		value := ruleAffinityValue(c, rule)
		if value == "" {
			continue
		}
		if rule.ValueRegex != "" && !matchAnyRegexCached([]string{rule.ValueRegex}, value) {
			continue
		}
		if mode, _ := EffectiveSessionMode(affinity, rule); mode == "off" {
			return autoAffinityProbe{}
		}
		ttl := rule.TTLSeconds
		if ttl <= 0 {
			ttl = affinity.DefaultTTLSeconds
		}
		return autoAffinityProbe{ok: true, stickySuffix: autoStickySuffix(rule, autoName, usingGroup, value), ttlSeconds: ttl}
	}
	return autoAffinityProbe{}
}

func ruleMatchesAnyModel(patterns []string, autoName string, candidates []string) bool {
	if matchAnyRegexCached(patterns, autoName) {
		return true
	}
	for _, candidate := range candidates {
		if matchAnyRegexCached(patterns, candidate) {
			return true
		}
	}
	return false
}

// ruleAffinityValue is the first non-empty value among the rule's key sources.
func ruleAffinityValue(c *gin.Context, rule operation_setting.ChannelAffinityRule) string {
	for _, src := range rule.KeySources {
		if value := extractChannelAffinityValue(c, src); value != "" {
			return value
		}
	}
	return ""
}

// autoStickySuffix is the cache key of a conversation's model. It starts with
// the rule name when the rule has include_rule_name, like the channel
// entries, so clearing a rule's cache clears these too.
func autoStickySuffix(rule operation_setting.ChannelAffinityRule, autoName, usingGroup, value string) string {
	parts := make([]string, 0, 5)
	if rule.IncludeRuleName && rule.Name != "" {
		parts = append(parts, rule.Name)
	}
	parts = append(parts, "auto", autoName)
	if rule.IncludeUsingGroup && usingGroup != "" {
		parts = append(parts, usingGroup)
	}
	parts = append(parts, value)
	return strings.Join(parts, ":")
}

// affinityAutoModel returns the conversation's remembered model when channel
// affinity is satisfied for it: the model is still one of the auto model's
// candidates, still has an enabled channel, and its affinity channel exists
// and is usable. The tier is the one the request was rated at when the model
// was chosen.
func affinityAutoModel(c *gin.Context, am *setting.AutoModel, probe autoAffinityProbe, usingGroup string, available autoAvailability, requestPath string) (string, autoroute.Tier, bool) {
	stored, found, err := getChannelAffinityCache().Get(probe.stickySuffix)
	if err != nil || !found {
		return "", autoroute.TierSimple, false
	}
	tier, hash, ok := autoroute.DecodeSticky(stored)
	if !ok {
		return "", autoroute.TierSimple, false
	}
	remembered, ok := autoroute.FindByHash(am.AllCandidates(), hash)
	if !ok || !available(remembered) {
		return "", autoroute.TierSimple, false
	}
	channelID, found := peekPreferredChannelByAffinity(c, remembered, usingGroup)
	if !found || !autoAffinityChannelUsable(c, channelID, remembered, usingGroup, requestPath) {
		return "", autoroute.TierSimple, false
	}
	return remembered, tier, true
}

// peekPreferredChannelByAffinity is GetPreferredChannelByAffinity without its
// side effects: the cached affinity channel of modelName, if any.
func peekPreferredChannelByAffinity(c *gin.Context, modelName, usingGroup string) (int, bool) {
	affinity := operation_setting.GetChannelAffinitySetting()
	if affinity == nil || !affinity.Enabled || c == nil || c.Request == nil || c.Request.URL == nil {
		return 0, false
	}
	path := c.Request.URL.Path
	userAgent := c.Request.UserAgent()
	for _, rule := range affinity.Rules {
		if !matchAnyRegexCached(rule.ModelRegex, modelName) {
			continue
		}
		if len(rule.PathRegex) > 0 && !matchAnyRegexCached(rule.PathRegex, path) {
			continue
		}
		if len(rule.UserAgentInclude) > 0 && !matchAnyIncludeFold(rule.UserAgentInclude, userAgent) {
			continue
		}
		value := ruleAffinityValue(c, rule)
		if value == "" {
			continue
		}
		if rule.ValueRegex != "" && !matchAnyRegexCached([]string{rule.ValueRegex}, value) {
			continue
		}
		if mode, _ := EffectiveSessionMode(affinity, rule); mode == "off" {
			return 0, false
		}
		channelID, found, err := getChannelAffinityCache().Get(buildChannelAffinityCacheKeySuffix(rule, modelName, usingGroup, value))
		if err != nil {
			return 0, false
		}
		return channelID, found
	}
	return 0, false
}

// affinityChannelUsable reports whether the cached affinity channel may be
// used for modelName, with the conditions SelectChannelForRequest applies:
// enabled, within its daily token budget, circuit closed (with smart routing
// on), allowed by the request's channel filters, and enabled for the group.
func affinityChannelUsable(c *gin.Context, channelID int, modelName, usingGroup, requestPath string) bool {
	preferred, err := model.CacheGetChannel(channelID)
	if err != nil || preferred == nil {
		return false
	}
	// A channel-pricing user is routed through bound channels, not a group.
	if userId := autoPricingUserID(c); userId > 0 {
		return pricingAffinityUsable(userId, preferred, modelName, requestPath)
	}
	if preferred.Status != common.ChannelStatusEnabled || model.IsChannelOverDailyTokenBudget(preferred) {
		return false
	}
	if setting.SmartRoutingEnabled && smartroute.IsCircuitOpen(preferred.Id, modelName) {
		return false
	}
	if ok, _ := model.ChannelSatisfiesFilters(preferred, modelName, GetChannelConstraints(c).Filters); !ok {
		return false
	}
	if usingGroup == "auto" {
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		for _, g := range GetRequestAutoGroups(c, userGroup) {
			if model.IsChannelEnabledForGroupModel(g, modelName, preferred.Id) {
				return true
			}
		}
		return false
	}
	return model.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) &&
		ResellerAffinityPreferredAllowed(c, usingGroup, modelName, requestPath, preferred.Id)
}

// RecordAutoModelAffinity remembers the model an auto model request settled
// on, after the request succeeded. The distributor calls it next to
// RecordChannelAffinity; it does nothing for a request that was not routed by
// content or that has no affinity key.
func RecordAutoModelAffinity(c *gin.Context) {
	info := getAutoRouteInfo(c)
	if info == nil || !info.HasAffinityKey {
		return
	}
	affinity := operation_setting.GetChannelAffinitySetting()
	if affinity == nil || !affinity.Enabled || RequestPolicy(c).SessionMode == "off" {
		return
	}
	finalModel := finalAutoModel(c)
	if finalModel == "" {
		return
	}
	ttlSeconds := info.AffinityTTL
	if ttlSeconds <= 0 {
		ttlSeconds = affinity.DefaultTTLSeconds
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	// Written under the namespaced key, as RecordChannelAffinity does.
	key := channelAffinityCacheNamespace + ":" + info.AffinityKey
	record := autoroute.EncodeSticky(info.Tier, finalModel)
	if err := getChannelAffinityCache().SetWithTTL(key, record, time.Duration(ttlSeconds)*time.Second); err != nil {
		common.SysError(common.LogText("auto model affinity cache set failed: key=%s, err=%v", key, err))
	}
}

// finalAutoModel is the model the request ended up on, after any failover.
func finalAutoModel(c *gin.Context) string {
	raw, ok := common.GetContextKey(c, constant.ContextKeyAutoModelCandidates)
	if !ok {
		return ""
	}
	plan, ok := raw.([]string)
	index := common.GetContextKeyInt(c, constant.ContextKeyAutoModelIndex)
	if !ok || index < 0 || index >= len(plan) {
		return ""
	}
	return plan[index]
}
