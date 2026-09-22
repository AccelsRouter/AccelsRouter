// Fork-only reseller upstream routing. A platform admin binds a set of upstream
// channels to a reseller and, per model, ranks those channels (priority) and
// shares traffic among equals (weight). The reseller's customers are then routed
// ONLY through those channels via the reseller's private routing group
// (reseller-<id>), which is attached to each bound channel's group list so the
// existing abilities / channel-cache machinery discovers candidates unchanged.
//
// Priority and weight are NOT written into the abilities rows: the memory-cache
// selection path reads channel-level priority/weight and ignores per-row values,
// so the matrix is kept here as the single source of truth and applied by the
// reseller selector (service/reseller_routing.go) at selection time. That also
// means a channel save that regenerates abilities can never clobber it.
//
// Upstream channels are never exposed to the reseller itself: everything in this
// file is admin-scoped, and the reseller-facing APIs return models and prices
// only.
package model

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

const (
	resellerRoutingGroupPrefix = "reseller-"
	// Hard caps on admin input. Generous for any real deployment, tight enough
	// that a mistaken or malicious payload cannot blow up storage or selection.
	resellerRoutingMaxChannels    = 50
	resellerRoutingMaxRules       = 2000
	resellerRoutingMaxModelLen    = 255
	resellerRoutingMaxAbsPriority = 1_000_000
	resellerRoutingMaxWeight      = 10_000
	resellerRoutingCacheTTL       = 30 * time.Second
)

// ResellerRoutingRule ranks one bound channel for one model (exact name) or one
// model family (prefix). Resolution: exact full-model-name key beats any prefix,
// the longest matching prefix beats shorter ones, and a channel with no matching
// rule keeps its channel-level priority/weight.
type ResellerRoutingRule struct {
	Model     string `json:"model"`
	ChannelId int    `json:"channel_id"`
	Priority  int64  `json:"priority"`
	Weight    uint   `json:"weight"`
}

// ResellerRouting is one reseller's upstream routing configuration.
type ResellerRouting struct {
	Id            int    `json:"id" gorm:"primarykey"`
	ResellerOrgId int    `json:"reseller_org_id" gorm:"uniqueIndex;not null"`
	ChannelIds    string `json:"-" gorm:"type:text"` // JSON []int, the bound channel set
	Rules         string `json:"-" gorm:"type:text"` // JSON []ResellerRoutingRule
	// Fallback: when every bound channel is exhausted for a request, continue in
	// the customer's original (platform) group. Zero value = strict isolation.
	Fallback bool `json:"fallback"`
	// AffinityOff disables sticky upstream/key selection. Zero value = affinity
	// ON, the cache-preserving default.
	AffinityOff bool  `json:"affinity_off"`
	UpdatedTime int64 `json:"updated_time"`

	// Parsed, non-persisted views of ChannelIds / Rules.
	ChannelIdList []int                 `json:"channel_ids" gorm:"-"`
	RuleList      []ResellerRoutingRule `json:"rules" gorm:"-"`
}

func (ResellerRouting) TableName() string { return "reseller_routings" }

// ResellerRoutingGroup is the private routing group reserved for one reseller's
// bound channels. Built from an integer only, so it can never carry user input.
func ResellerRoutingGroup(resellerOrgId int) string {
	return resellerRoutingGroupPrefix + strconv.Itoa(resellerOrgId)
}

// ParseResellerRoutingGroup recognises a reseller routing group and returns its
// reseller org id. Strict: the prefix followed by decimal digits only, id > 0.
// "reseller-1" must never match "reseller-12" or "reseller-1x".
func ParseResellerRoutingGroup(group string) (int, bool) {
	if !strings.HasPrefix(group, resellerRoutingGroupPrefix) {
		return 0, false
	}
	digits := group[len(resellerRoutingGroupPrefix):]
	if digits == "" || len(digits) > 9 {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.Atoi(digits)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (r *ResellerRouting) parseColumns() {
	r.ChannelIdList = []int{}
	r.RuleList = []ResellerRoutingRule{}
	if s := strings.TrimSpace(r.ChannelIds); s != "" {
		var ids []int
		if err := common.Unmarshal([]byte(s), &ids); err == nil {
			r.ChannelIdList = ids
		}
	}
	if s := strings.TrimSpace(r.Rules); s != "" {
		var rules []ResellerRoutingRule
		if err := common.Unmarshal([]byte(s), &rules); err == nil {
			r.RuleList = rules
		}
	}
}

// GetResellerRouting loads a reseller's routing config; (nil, nil) when none.
func GetResellerRouting(resellerOrgId int) (*ResellerRouting, error) {
	if resellerOrgId <= 0 {
		return nil, nil
	}
	var row ResellerRouting
	err := DB.Where("reseller_org_id = ?", resellerOrgId).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	row.parseColumns()
	return &row, nil
}

type resellerRoutingCacheEntry struct {
	cfg       *ResellerRouting // nil = reseller has no routing config
	expiresAt time.Time
}

var resellerRoutingCache sync.Map // resellerOrgId -> resellerRoutingCacheEntry

// GetResellerRoutingCached is the hot-path lookup used by the distributor and
// the selector: TTL-cached, never returns an error (a DB failure reads as "no
// config" for that window, i.e. the request takes the unchanged platform path).
func GetResellerRoutingCached(resellerOrgId int) *ResellerRouting {
	if resellerOrgId <= 0 {
		return nil
	}
	if v, ok := resellerRoutingCache.Load(resellerOrgId); ok {
		entry := v.(resellerRoutingCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.cfg
		}
	}
	cfg, err := GetResellerRouting(resellerOrgId)
	if err != nil {
		common.SysError("reseller routing lookup failed: " + err.Error())
		cfg = nil
	}
	resellerRoutingCache.Store(resellerOrgId, resellerRoutingCacheEntry{cfg: cfg, expiresAt: time.Now().Add(resellerRoutingCacheTTL)})
	return cfg
}

// InvalidateResellerRoutingCache must follow every admin save so the hot path
// converges within one request instead of the TTL.
func InvalidateResellerRoutingCache(resellerOrgId int) {
	resellerRoutingCache.Delete(resellerOrgId)
}

// ValidateResellerRouting normalises and bounds admin input in place. It is the
// only gate between the admin API and storage, so every cap lives here.
func ValidateResellerRouting(r *ResellerRouting) error {
	if r == nil {
		return errors.New("routing config is required")
	}
	if len(r.ChannelIdList) > resellerRoutingMaxChannels {
		return errors.New("too many channels (max " + strconv.Itoa(resellerRoutingMaxChannels) + ")")
	}
	if len(r.RuleList) > resellerRoutingMaxRules {
		return errors.New("too many rules (max " + strconv.Itoa(resellerRoutingMaxRules) + ")")
	}
	seenCh := map[int]struct{}{}
	channels := make([]int, 0, len(r.ChannelIdList))
	for _, id := range r.ChannelIdList {
		if id <= 0 {
			return errors.New("invalid channel id")
		}
		if _, dup := seenCh[id]; dup {
			continue
		}
		seenCh[id] = struct{}{}
		channels = append(channels, id)
	}
	sort.Ints(channels)
	r.ChannelIdList = channels

	type ruleKey struct {
		model     string
		channelId int
	}
	seenRule := map[ruleKey]struct{}{}
	rules := make([]ResellerRoutingRule, 0, len(r.RuleList))
	for _, rule := range r.RuleList {
		m := strings.ToLower(strings.TrimSpace(rule.Model))
		if m == "" {
			return errors.New("rule model must not be empty")
		}
		if len(m) > resellerRoutingMaxModelLen {
			return errors.New("rule model too long")
		}
		for _, ch := range m {
			if ch == ',' || unicode.IsControl(ch) || unicode.IsSpace(ch) {
				return errors.New("rule model contains an invalid character")
			}
		}
		if _, bound := seenCh[rule.ChannelId]; !bound {
			return errors.New("rule refers to a channel that is not bound to this reseller")
		}
		if rule.Priority > resellerRoutingMaxAbsPriority || rule.Priority < -resellerRoutingMaxAbsPriority {
			return errors.New("rule priority out of range")
		}
		if rule.Weight > resellerRoutingMaxWeight {
			return errors.New("rule weight out of range")
		}
		k := ruleKey{model: m, channelId: rule.ChannelId}
		if _, dup := seenRule[k]; dup {
			return errors.New("duplicate rule for model " + m)
		}
		seenRule[k] = struct{}{}
		rules = append(rules, ResellerRoutingRule{Model: m, ChannelId: rule.ChannelId, Priority: rule.Priority, Weight: rule.Weight})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Model != rules[j].Model {
			return rules[i].Model < rules[j].Model
		}
		return rules[i].ChannelId < rules[j].ChannelId
	})
	r.RuleList = rules
	return nil
}

// SaveResellerRouting validates, persists, and applies a reseller's routing
// config: the private group is attached to every bound channel and detached
// from any channel no longer bound (abilities regenerated, cache refreshed).
// The org must be an existing reseller; every bound channel must exist.
func SaveResellerRouting(r *ResellerRouting) error {
	if err := ValidateResellerRouting(r); err != nil {
		return err
	}
	org, err := GetOrganizationById(r.ResellerOrgId)
	if err != nil || org == nil {
		return errors.New("reseller organization not found")
	}
	if org.Type != OrgTypeReseller {
		return errors.New("upstream routing can only be configured for a reseller organization")
	}
	if len(r.ChannelIdList) > 0 {
		var count int64
		if err := DB.Model(&Channel{}).Where("id IN ?", r.ChannelIdList).Count(&count).Error; err != nil {
			return err
		}
		if int(count) != len(r.ChannelIdList) {
			return errors.New("one or more bound channels do not exist")
		}
	}

	idsJSON, err := common.Marshal(r.ChannelIdList)
	if err != nil {
		return err
	}
	rulesJSON, err := common.Marshal(r.RuleList)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	existing, err := GetResellerRouting(r.ResellerOrgId)
	if err != nil {
		return err
	}
	if existing == nil {
		row := ResellerRouting{
			ResellerOrgId: r.ResellerOrgId,
			ChannelIds:    string(idsJSON),
			Rules:         string(rulesJSON),
			Fallback:      r.Fallback,
			AffinityOff:   r.AffinityOff,
			UpdatedTime:   now,
		}
		if err := DB.Create(&row).Error; err != nil {
			return err
		}
		r.Id = row.Id
	} else {
		if err := DB.Model(&ResellerRouting{}).Where("reseller_org_id = ?", r.ResellerOrgId).
			Updates(map[string]interface{}{
				"channel_ids":  string(idsJSON),
				"rules":        string(rulesJSON),
				"fallback":     r.Fallback,
				"affinity_off": r.AffinityOff,
				"updated_time": now,
			}).Error; err != nil {
			return err
		}
		r.Id = existing.Id
	}
	r.ChannelIds = string(idsJSON)
	r.Rules = string(rulesJSON)
	r.UpdatedTime = now
	InvalidateResellerRoutingCache(r.ResellerOrgId)
	return syncResellerChannelGroups(r.ResellerOrgId, r.ChannelIdList)
}

// syncResellerChannelGroups makes "channels whose group list contains
// reseller-<id>" equal to the desired bound set. Only the group list of the
// affected channels changes; their own groups, models, keys and priorities are
// untouched, and their abilities are regenerated through the same call every
// channel edit uses.
func syncResellerChannelGroups(resellerOrgId int, desired []int) error {
	group := ResellerRoutingGroup(resellerOrgId)
	want := make(map[int]struct{}, len(desired))
	for _, id := range desired {
		want[id] = struct{}{}
	}
	var channels []Channel
	if err := DB.Select("id", "group", "models", "status", "priority", "weight", "tag").Find(&channels).Error; err != nil {
		return err
	}
	changed := false
	for i := range channels {
		ch := &channels[i]
		has := channelHasGroup(ch.Group, group)
		_, shouldHave := want[ch.Id]
		if has == shouldHave {
			continue
		}
		newGroup := setChannelGroupMembership(ch.Group, group, shouldHave)
		if err := DB.Model(&Channel{}).Where("id = ?", ch.Id).Update("group", newGroup).Error; err != nil {
			return err
		}
		ch.Group = newGroup
		if err := ch.UpdateAbilities(nil); err != nil {
			return err
		}
		changed = true
	}
	if changed && common.MemoryCacheEnabled {
		InitChannelCache()
	}
	return nil
}

// channelHasGroup is an exact-token test over the comma-separated group list
// ("reseller-1" is not a member because "reseller-12" is).
func channelHasGroup(groupList, group string) bool {
	for _, g := range strings.Split(groupList, ",") {
		if strings.TrimSpace(g) == group {
			return true
		}
	}
	return false
}

func setChannelGroupMembership(groupList, group string, member bool) string {
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, g := range strings.Split(groupList, ",") {
		g = strings.TrimSpace(g)
		if g == "" || g == group {
			continue
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	if member {
		out = append(out, group)
	}
	return strings.Join(out, ",")
}

// ResolveResellerRule returns the priority/weight the reseller configured for a
// channel on a model: exact full-model-name rule beats any prefix rule, the
// longest matching prefix beats shorter ones. matched=false when no rule
// applies (the caller falls back to the channel's own priority/weight).
func ResolveResellerRule(rules []ResellerRoutingRule, modelName string, channelId int) (priority int64, weight uint, matched bool) {
	name := strings.ToLower(modelName)
	bestLen := -1
	for _, rule := range rules {
		if rule.ChannelId != channelId {
			continue
		}
		if rule.Model == name {
			return rule.Priority, rule.Weight, true
		}
		if strings.HasPrefix(name, rule.Model) && len(rule.Model) > bestLen {
			priority, weight, matched = rule.Priority, rule.Weight, true
			bestLen = len(rule.Model)
		}
	}
	return priority, weight, matched
}

// FilterChannelIdsByRequestPathAndModel applies the same request-path filter
// the platform selector applies (Advanced Custom channels serve only matching
// routes). Memory-cache path only; with the cache disabled the ids pass
// through unchanged, matching the DB selector's own handling.
func FilterChannelIdsByRequestPathAndModel(ids []int, requestPath, modelName string) []int {
	if !common.MemoryCacheEnabled || requestPath == "" || len(ids) == 0 {
		return ids
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	return filterChannelsByRequestPathAndModel(ids, requestPath, modelName)
}

// ResellerRoutingChannelSummary is the admin-only view of a candidate channel:
// identity, models and status — never keys or upstream credentials.
type ResellerRoutingChannelSummary struct {
	Id     int      `json:"id"`
	Name   string   `json:"name"`
	Type   int      `json:"type"`
	Status int      `json:"status"`
	Models []string `json:"models"`
	// The channel's own platform priority/weight: what a blank matrix cell
	// falls back to, and the starting values the editor pre-fills.
	Priority int64 `json:"priority"`
	Weight   uint  `json:"weight"`
}

// ListResellerRoutingChannelSummaries returns every enabled channel in the
// admin-safe summary form, for the routing matrix editor.
func ListResellerRoutingChannelSummaries() ([]ResellerRoutingChannelSummary, error) {
	var rows []Channel
	if err := DB.Select("id", "name", "type", "status", "models", "priority", "weight").
		Where("status = ?", common.ChannelStatusEnabled).
		Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ResellerRoutingChannelSummary, 0, len(rows))
	for _, ch := range rows {
		out = append(out, ResellerRoutingChannelSummary{
			Id: ch.Id, Name: ch.Name, Type: ch.Type, Status: ch.Status, Models: splitModelList(ch.Models),
			Priority: ch.GetPriority(), Weight: uint(ch.GetWeight()),
		})
	}
	return out, nil
}

// ResellerEffectiveModels is what a reseller's customers can actually reach:
// the union of the bound channels' models, capped by the reseller's offerable
// allow-list when one is set. uncovered lists offerable models no bound channel
// serves — the admin warning that prevents "offered but unroutable" models.
func ResellerEffectiveModels(reseller *Organization, boundChannelIds []int) (effective []string, uncovered []string, err error) {
	served := map[string]struct{}{}
	if len(boundChannelIds) > 0 {
		var rows []Channel
		if err := DB.Select("id", "models").Where("id IN ?", boundChannelIds).Find(&rows).Error; err != nil {
			return nil, nil, err
		}
		for _, ch := range rows {
			for _, m := range splitModelList(ch.Models) {
				served[m] = struct{}{}
			}
		}
	}
	allowed := parseAllowedModels(reseller.AllowedModels)
	if allowed == nil {
		effective = make([]string, 0, len(served))
		for m := range served {
			effective = append(effective, m)
		}
		sort.Strings(effective)
		return effective, []string{}, nil
	}
	effective = []string{}
	uncovered = []string{}
	for m := range allowed {
		if _, ok := served[m]; ok {
			effective = append(effective, m)
		} else {
			uncovered = append(uncovered, m)
		}
	}
	sort.Strings(effective)
	sort.Strings(uncovered)
	return effective, uncovered, nil
}

func splitModelList(models string) []string {
	out := make([]string, 0, 8)
	for _, m := range strings.Split(models, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}
