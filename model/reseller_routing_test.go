package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The routing group is the isolation boundary between resellers: a customer of
// reseller 1 must never be routed (or billed) as reseller 12. Parsing must be
// strict and exact-token.
func TestParseResellerRoutingGroupIsStrict(t *testing.T) {
	id, ok := ParseResellerRoutingGroup("reseller-12")
	require.True(t, ok)
	assert.Equal(t, 12, id)
	assert.Equal(t, "reseller-7", ResellerRoutingGroup(7))

	for _, g := range []string{
		"", "reseller-", "reseller-0", "reseller-1x", "reseller-1 ", " reseller-1",
		"Reseller-1", "reseller--1", "reseller-1234567890", "default", "org-1",
		"user-1", "reseller-1,default",
	} {
		_, ok := ParseResellerRoutingGroup(g)
		assert.False(t, ok, "%q must not parse", g)
	}
}

func TestChannelGroupMembershipIsExactToken(t *testing.T) {
	assert.True(t, channelHasGroup("default,reseller-1", "reseller-1"))
	assert.False(t, channelHasGroup("default,reseller-12", "reseller-1"), "prefix must not match")
	assert.False(t, channelHasGroup("reseller-1x", "reseller-1"))

	assert.Equal(t, "default,vip,reseller-1", setChannelGroupMembership("default, vip", "reseller-1", true))
	assert.Equal(t, "default,vip", setChannelGroupMembership("default,reseller-1,vip", "reseller-1", false))
	assert.Equal(t, "default,reseller-1", setChannelGroupMembership("default,default,reseller-1", "reseller-1", true),
		"dedupes and never doubles the group")
	assert.Equal(t, "", setChannelGroupMembership("reseller-1", "reseller-1", false))
}

func TestResolveResellerRuleExactBeatsLongestPrefix(t *testing.T) {
	rules := []ResellerRoutingRule{
		{Model: "claude-", ChannelId: 1, Priority: 10, Weight: 1},
		{Model: "claude-opus-", ChannelId: 1, Priority: 20, Weight: 2},
		{Model: "claude-opus-4-8", ChannelId: 1, Priority: 30, Weight: 3},
		{Model: "claude-", ChannelId: 2, Priority: 5, Weight: 5},
	}

	p, w, ok := ResolveResellerRule(rules, "claude-opus-4-8", 1)
	require.True(t, ok)
	assert.EqualValues(t, 30, p, "exact name wins")
	assert.EqualValues(t, 3, w)

	p, w, ok = ResolveResellerRule(rules, "claude-opus-4-9", 1)
	require.True(t, ok)
	assert.EqualValues(t, 20, p, "longest prefix wins")
	assert.EqualValues(t, 2, w)

	p, w, ok = ResolveResellerRule(rules, "claude-sonnet-5", 1)
	require.True(t, ok)
	assert.EqualValues(t, 10, p)
	assert.EqualValues(t, 1, w)

	p, w, ok = ResolveResellerRule(rules, "CLAUDE-SONNET-5", 2)
	require.True(t, ok, "matching is case-insensitive")
	assert.EqualValues(t, 5, p)
	assert.EqualValues(t, 5, w)

	_, _, ok = ResolveResellerRule(rules, "deepseek-v4", 1)
	assert.False(t, ok, "no rule => caller keeps the channel's own priority/weight")
	_, _, ok = ResolveResellerRule(rules, "claude-opus-4-8", 3)
	assert.False(t, ok, "rules are per channel")
}

func TestValidateResellerRoutingNormalisesAndBounds(t *testing.T) {
	r := &ResellerRouting{
		ChannelIdList: []int{3, 1, 3},
		RuleList: []ResellerRoutingRule{
			{Model: "  Claude-Opus-4-8 ", ChannelId: 3, Priority: 1, Weight: 2},
			{Model: "deepseek-", ChannelId: 1, Priority: -1},
		},
	}
	require.NoError(t, ValidateResellerRouting(r))
	assert.Equal(t, []int{1, 3}, r.ChannelIdList, "deduped and sorted")
	require.Len(t, r.RuleList, 2)
	assert.Equal(t, "claude-opus-4-8", r.RuleList[0].Model, "lowercased, trimmed, sorted by model")
	assert.Equal(t, "deepseek-", r.RuleList[1].Model)

	bad := []struct {
		name string
		cfg  ResellerRouting
	}{
		{"channel id <= 0", ResellerRouting{ChannelIdList: []int{0}}},
		{"rule on unbound channel", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "m", ChannelId: 2}}}},
		{"empty model", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "  ", ChannelId: 1}}}},
		{"comma in model", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "a,b", ChannelId: 1}}}},
		{"control char in model", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "a\nb", ChannelId: 1}}}},
		{"priority out of range", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "m", ChannelId: 1, Priority: resellerRoutingMaxAbsPriority + 1}}}},
		{"weight out of range", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "m", ChannelId: 1, Weight: resellerRoutingMaxWeight + 1}}}},
		{"duplicate rule (case-folded)", ResellerRouting{ChannelIdList: []int{1}, RuleList: []ResellerRoutingRule{{Model: "m", ChannelId: 1}, {Model: "M", ChannelId: 1}}}},
	}
	for _, tc := range bad {
		cfg := tc.cfg
		assert.Error(t, ValidateResellerRouting(&cfg), tc.name)
	}

	tooMany := ResellerRouting{ChannelIdList: make([]int, resellerRoutingMaxChannels+1)}
	for i := range tooMany.ChannelIdList {
		tooMany.ChannelIdList[i] = i + 1
	}
	assert.Error(t, ValidateResellerRouting(&tooMany), "channel cap")
	assert.Error(t, ValidateResellerRouting(nil))
}
