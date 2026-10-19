package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/autoroute"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAutoModelTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

// stubAvailability replaces the channel-availability probe; restore on cleanup.
func stubAvailability(t *testing.T, available map[string]bool) {
	t.Helper()
	orig := groupModelAvailable
	groupModelAvailable = func(group, model, requestPath string) bool {
		return available[group+"/"+model]
	}
	t.Cleanup(func() { groupModelAvailable = orig })
}

func setupAutoModels(t *testing.T, jsonStr string) {
	t.Helper()
	require.NoError(t, setting.UpdateAutoModelConfigs(jsonStr))
	t.Cleanup(func() { _ = setting.UpdateAutoModelConfigs("[]") })
}

// Billing safety: resolution must yield a concrete, currently-available model
// or an explicit rejection — the virtual name must never fall through.
func TestResolveAutoModel(t *testing.T) {
	setupAutoModels(t, `[{"name":"auto-cn","models":["m-a","m-b","m-c"]}]`)

	t.Run("non-auto model passes through untouched", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		resolved, isAuto := ResolveAutoModel(c, "default", "gpt-4o", "/v1/chat/completions")
		assert.False(t, isAuto)
		assert.Empty(t, resolved)
	})

	t.Run("skips unavailable candidates in order", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		stubAvailability(t, map[string]bool{"default/m-b": true, "default/m-c": true})
		resolved, isAuto := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
		assert.True(t, isAuto)
		assert.Equal(t, "m-b", resolved)
		assert.Equal(t, "auto-cn", common.GetContextKeyString(c, constant.ContextKeyAutoModelOriginal))
		assert.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyAutoModelIndex))
	})

	t.Run("no candidate available -> isAuto with empty resolution", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		stubAvailability(t, map[string]bool{})
		resolved, isAuto := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
		assert.True(t, isAuto, "caller must know this WAS an auto model and reject")
		assert.Empty(t, resolved)
	})
}

// The failover cursor must be strictly monotonic and terminate: with N
// candidates there can be at most N-1 advances, regardless of errors.
func TestAdvanceAutoModel(t *testing.T) {
	setupAutoModels(t, `[{"name":"auto-cn","models":["m-a","m-b","m-c"]}]`)

	t.Run("advances past unavailable candidates and then exhausts", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		stubAvailability(t, map[string]bool{"default/m-a": true, "default/m-c": true})
		resolved, isAuto := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
		require.True(t, isAuto)
		require.Equal(t, "m-a", resolved)

		next, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
		require.True(t, ok)
		assert.Equal(t, "m-c", next, "m-b is unavailable and must be skipped")

		_, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok, "pool exhausted; cursor must not wrap")
		_, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok, "repeated calls after exhaustion stay exhausted")
	})

	t.Run("no-op for requests that were never auto", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		_, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok)
	})
}

// ---- auto models routed by content ----

const tieredAutoConfig = `[{"name":"auto","tiers":{"simple":["m-small"],"medium":["m-mid"],"complex":["m-big"],"reasoning":["m-top"]}}]`

func newTieredTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func chatBody(text string) string {
	return `{"model":"auto","messages":[{"role":"user","content":"` + text + `"}]}`
}

// withAffinity installs one channel affinity rule keyed on the X-Session
// header for the models the test uses, and restores the settings afterwards.
func withAffinity(t *testing.T, enabled bool) operation_setting.ChannelAffinityRule {
	t.Helper()
	affinity := operation_setting.GetChannelAffinitySetting()
	origEnabled, origRules := affinity.Enabled, affinity.Rules
	rule := operation_setting.ChannelAffinityRule{
		Name:             "auto-test",
		ModelRegex:       []string{"^m-.*$"},
		KeySources:       []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}},
		IncludeRuleName:  true,
		IncludeModelName: true,
	}
	affinity.Enabled = enabled
	affinity.Rules = []operation_setting.ChannelAffinityRule{rule}
	t.Cleanup(func() { affinity.Enabled, affinity.Rules = origEnabled, origRules })
	return rule
}

func TestResolveTieredAutoModelByContent(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withAffinity(t, false)
	stubAvailability(t, map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-big": true, "default/m-top": true})

	cases := []struct {
		name string
		text string
		want string
		tier string
	}{
		{"a greeting is simple", "你好", "m-small", "simple"},
		{"a code request is medium", "请帮我写一个 Python 函数，实现快速排序，并给出单元测试", "m-mid", "medium"},
		{"design work is complex", "用Go实现一个分布式缓存，需要考虑一致性和高并发，写出核心代码和接口定义", "m-big", "complex"},
		{"explicit reasoning is reasoning", "请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊", "m-top", "reasoning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTieredTestContext(t, chatBody(tc.text))
			resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
			require.True(t, isAuto)
			assert.Equal(t, tc.want, resolved)
			assert.Equal(t, "auto", common.GetContextKeyString(c, constant.ContextKeyAutoModelOriginal))
			assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "tier="+tc.tier)
			assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "via=classifier")
			info := getAutoRouteInfo(c)
			require.NotNil(t, info)
			assert.Equal(t, autoViaClassifier, info.Via)
		})
	}
}

func TestTieredAutoModelFailsOverUpwardOnly(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withAffinity(t, false)

	t.Run("a tier with no available model uses a stronger one", func(t *testing.T) {
		stubAvailability(t, map[string]bool{"default/m-big": true})
		c := newTieredTestContext(t, chatBody("你好"))
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		require.True(t, isAuto)
		assert.Equal(t, "m-big", resolved)
	})

	t.Run("never falls back to a weaker tier", func(t *testing.T) {
		stubAvailability(t, map[string]bool{"default/m-small": true})
		c := newTieredTestContext(t, chatBody("请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊"))
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.True(t, isAuto)
		assert.Empty(t, resolved, "the request must be rejected, not served by a weaker model")
	})

	t.Run("channel exhaustion advances through the plan and then stops", func(t *testing.T) {
		stubAvailability(t, map[string]bool{"default/m-mid": true, "default/m-big": true, "default/m-top": true})
		c := newTieredTestContext(t, chatBody("请帮我写一个 Python 函数，实现快速排序，并给出单元测试"))
		resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		require.Equal(t, "m-mid", resolved)
		next, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
		require.True(t, ok)
		assert.Equal(t, "m-big", next)
		assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "via=failover")
		next, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		require.True(t, ok)
		assert.Equal(t, "m-top", next)
		_, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok)
	})
}

// A claude agent loop ends with a user message that only holds tool results.
// The request must keep the tier of what the person asked.
func TestTieredAutoModelAgentLoop(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withAffinity(t, false)
	stubAvailability(t, map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-big": true, "default/m-top": true})

	body := `{"model":"auto","messages":[` +
		`{"role":"user","content":"请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"read_file","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"def f(): pass"}]}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/messages")
	require.True(t, isAuto)
	assert.Equal(t, "m-top", resolved)
}

// Channel affinity has the highest priority: a conversation that already has
// a model and a usable affinity channel keeps both, whatever its latest
// message looks like; one that does not is classified.
func TestTieredAutoModelAffinityComesFirst(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	rule := withAffinity(t, true)
	stubAvailability(t, map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-big": true, "default/m-top": true})
	usable := true
	orig := autoAffinityChannelUsable
	autoAffinityChannelUsable = func(_ *gin.Context, _ int, _, _, _ string) bool { return usable }
	t.Cleanup(func() { autoAffinityChannelUsable = orig })

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	newContext := func(text string) *gin.Context {
		c := newTieredTestContext(t, chatBody(text))
		c.Request.Header.Set("X-Session", session)
		return c
	}

	// The conversation settled on m-big (rated complex) and used channel 7.
	cache := getChannelAffinityCache()
	stickyKey := autoStickySuffix(rule, "auto", "default", session)
	channelKey := buildChannelAffinityCacheKeySuffix(rule, "m-big", "default", session)
	require.NoError(t, cache.SetWithTTL(stickyKey, autoroute.EncodeSticky(autoroute.TierComplex, "m-big"), time.Minute))
	require.NoError(t, cache.SetWithTTL(channelKey, 7, time.Minute))
	t.Cleanup(func() { _, _ = cache.DeleteMany([]string{stickyKey, channelKey}) })

	t.Run("a short follow-up keeps the model instead of being rated simple", func(t *testing.T) {
		c := newContext("继续")
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		require.True(t, isAuto)
		assert.Equal(t, "m-big", resolved)
		assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "via=affinity")
	})

	t.Run("when the affinity channel is no longer usable the request is classified", func(t *testing.T) {
		usable = false
		defer func() { usable = true }()
		c := newContext("你好")
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		require.True(t, isAuto)
		assert.Equal(t, "m-small", resolved)
		assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "via=classifier")
	})

	t.Run("a remembered model that is gone from the config is ignored", func(t *testing.T) {
		require.NoError(t, cache.SetWithTTL(stickyKey, autoroute.EncodeSticky(autoroute.TierComplex, "removed-model"), time.Minute))
		defer func() {
			_ = cache.SetWithTTL(stickyKey, autoroute.EncodeSticky(autoroute.TierComplex, "m-big"), time.Minute)
		}()
		c := newContext("你好")
		resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.Equal(t, "m-small", resolved)
	})

	t.Run("a request with another session key is classified", func(t *testing.T) {
		c := newTieredTestContext(t, chatBody("你好"))
		c.Request.Header.Set("X-Session", session+"-other")
		resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.Equal(t, "m-small", resolved)
	})
}

// After a successful request the model it settled on is remembered, failover
// included, so the next request of the conversation finds it.
func TestRecordAutoModelAffinityRoundTrip(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	rule := withAffinity(t, true)
	stubAvailability(t, map[string]bool{"default/m-mid": true, "default/m-big": true})

	session := fmt.Sprintf("round-trip-%d", time.Now().UnixNano())
	c := newTieredTestContext(t, chatBody("请帮我写一个 Python 函数，实现快速排序，并给出单元测试"))
	c.Request.Header.Set("X-Session", session)
	resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
	require.Equal(t, "m-mid", resolved)
	next, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
	require.True(t, ok)
	require.Equal(t, "m-big", next)

	RecordAutoModelAffinity(c)

	stickyKey := autoStickySuffix(rule, "auto", "default", session)
	cache := getChannelAffinityCache()
	t.Cleanup(func() { _, _ = cache.DeleteMany([]string{stickyKey}) })
	stored, found, err := cache.Get(stickyKey)
	require.NoError(t, err)
	require.True(t, found, "the model the request ended on must be remembered")
	tier, hash, ok := autoroute.DecodeSticky(stored)
	require.True(t, ok)
	assert.Equal(t, autoroute.TierMedium, tier, "the tier the request was rated at")
	assert.Equal(t, autoroute.ModelHash("m-big"), hash, "the model after failover, not the first one")
}

func TestRecordAutoModelAffinityIgnoresOtherRequests(t *testing.T) {
	setupAutoModels(t, `[{"name":"auto-cn","models":["m-a"]}]`)
	withAffinity(t, true)
	stubAvailability(t, map[string]bool{"default/m-a": true})
	c := newAutoModelTestContext(t)
	resolved, _ := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
	require.Equal(t, "m-a", resolved)
	assert.Nil(t, getAutoRouteInfo(c), "a fixed-order model keeps no content routing state")
	RecordAutoModelAffinity(c) // must be a no-op, not a panic
	RecordAutoModelAffinity(newAutoModelTestContext(t))
}

// ---- channel-pricing users: an auto model picks among the user's bound channels ----

func stubUserBound(t *testing.T, bound map[string]bool) {
	t.Helper()
	orig := userModelBound
	userModelBound = func(userId int, candidate, requestPath string) bool {
		return bound[fmt.Sprintf("%d/%s", userId, candidate)]
	}
	t.Cleanup(func() { userModelBound = orig })
}

func newPricingTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c := newTieredTestContext(t, body)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	return c
}

func TestResolveAutoModelForUser(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withAffinity(t, false)
	// Groups offer nothing: a channel-pricing user must never be judged by them.
	stubAvailability(t, map[string]bool{})
	stubUserBound(t, map[string]bool{"7/m-mid": true, "7/m-big": true})

	t.Run("picks among the models the user is bound to, upward only", func(t *testing.T) {
		c := newPricingTestContext(t, chatBody("你好"))
		resolved, isAuto := ResolveAutoModelForUser(c, 7, "auto", "/v1/chat/completions")
		require.True(t, isAuto)
		assert.Equal(t, "m-mid", resolved, "the simple tier has no bound model, so the next stronger one is used")
		assert.Equal(t, 7, autoPricingUserID(c))

		next, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
		require.True(t, ok)
		assert.Equal(t, "m-big", next, "failover follows the bindings too")
		_, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok, "m-top is not bound to this user")
	})

	t.Run("a user with no binding for any candidate is rejected", func(t *testing.T) {
		c := newPricingTestContext(t, chatBody("你好"))
		resolved, isAuto := ResolveAutoModelForUser(c, 8, "auto", "/v1/chat/completions")
		assert.True(t, isAuto)
		assert.Empty(t, resolved)
	})

	t.Run("a model that is not an auto model is left alone", func(t *testing.T) {
		c := newPricingTestContext(t, chatBody("你好"))
		resolved, isAuto := ResolveAutoModelForUser(c, 7, "gpt-4o", "/v1/chat/completions")
		assert.False(t, isAuto)
		assert.Empty(t, resolved)
		assert.Equal(t, 0, autoPricingUserID(c), "a request that is not auto must not be marked")
	})

	t.Run("a group-routed request is not affected by the marker", func(t *testing.T) {
		stubAvailability(t, map[string]bool{"default/m-small": true})
		c := newTieredTestContext(t, chatBody("你好"))
		resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.Equal(t, "m-small", resolved)
		assert.Equal(t, 0, autoPricingUserID(c))
	})

	t.Run("a fixed-order pool resolves by bindings as well", func(t *testing.T) {
		setupAutoModels(t, `[{"name":"auto-cn","models":["m-a","m-b"]}]`)
		stubUserBound(t, map[string]bool{"7/m-b": true})
		c := newPricingTestContext(t, chatBody("你好"))
		resolved, isAuto := ResolveAutoModelForUser(c, 7, "auto-cn", "/v1/chat/completions")
		require.True(t, isAuto)
		assert.Equal(t, "m-b", resolved)
	})
}

func TestAutoModelForUserAffinityComesFirst(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	rule := withAffinity(t, true)
	stubAvailability(t, map[string]bool{})
	bound := map[string]bool{"7/m-small": true, "7/m-mid": true, "7/m-big": true, "7/m-top": true}
	stubUserBound(t, bound)
	usable := true
	orig := autoAffinityChannelUsable
	autoAffinityChannelUsable = func(_ *gin.Context, _ int, _, _, _ string) bool { return usable }
	t.Cleanup(func() { autoAffinityChannelUsable = orig })

	session := fmt.Sprintf("pricing-session-%d", time.Now().UnixNano())
	newContext := func(text string) *gin.Context {
		c := newPricingTestContext(t, chatBody(text))
		c.Request.Header.Set("X-Session", session)
		return c
	}
	cache := getChannelAffinityCache()
	stickyKey := autoStickySuffix(rule, "auto", "default", session)
	channelKey := buildChannelAffinityCacheKeySuffix(rule, "m-big", "default", session)
	require.NoError(t, cache.SetWithTTL(stickyKey, autoroute.EncodeSticky(autoroute.TierComplex, "m-big"), time.Minute))
	require.NoError(t, cache.SetWithTTL(channelKey, 7, time.Minute))
	t.Cleanup(func() { _, _ = cache.DeleteMany([]string{stickyKey, channelKey}) })

	resolved, isAuto := ResolveAutoModelForUser(newContext("继续"), 7, "auto", "/v1/chat/completions")
	require.True(t, isAuto)
	assert.Equal(t, "m-big", resolved, "the conversation keeps its model and, through affinity, its channel")

	usable = false
	resolved, _ = ResolveAutoModelForUser(newContext("你好"), 7, "auto", "/v1/chat/completions")
	assert.Equal(t, "m-small", resolved, "affinity not satisfied: the request is classified")
	usable = true

	bound["7/m-big"] = false // the binding was removed since
	resolved, _ = ResolveAutoModelForUser(newContext("你好"), 7, "auto", "/v1/chat/completions")
	assert.Equal(t, "m-small", resolved, "a remembered model the user can no longer reach is not reused")
}

func TestUsedChannelIds(t *testing.T) {
	assert.Empty(t, usedChannelIds(nil))
	c := newAutoModelTestContext(t)
	assert.Empty(t, usedChannelIds(c))
	c.Set("use_channel", []string{"3", "x", "5"})
	assert.Equal(t, map[int]bool{3: true, 5: true}, usedChannelIds(c))
}

// A retry must never land on a channel the request has already tried, such as
// one that channel affinity picked ahead of the ranking.
func TestCacheGetChannelPricingChannelSkipsUsedChannels(t *testing.T) {
	orig := channelPricingSeeded
	channelPricingSeeded = func(userId int, modelName string, retry int, requestPath, seed string) (*model.Channel, bool, error) {
		ranked := []int{4, 5, 6}
		if retry < 0 || retry >= len(ranked) {
			return nil, false, nil
		}
		return &model.Channel{Id: ranked[retry]}, false, nil
	}
	t.Cleanup(func() { channelPricingSeeded = orig })

	c := newAutoModelTestContext(t)
	param := &RetryParam{Ctx: c, ModelName: "m", RequestPath: "/v1/chat/completions", Retry: common.GetPointer(0)}
	pick := func() int {
		ch, _, err := CacheGetChannelPricingChannel(7, param)
		require.NoError(t, err)
		if ch == nil {
			return 0
		}
		return ch.Id
	}

	assert.Equal(t, 4, pick(), "nothing tried yet: the top of the ranking")
	c.Set("use_channel", []string{"5"}) // the first attempt used channel 5, chosen by affinity
	assert.Equal(t, 4, pick())
	c.Set("use_channel", []string{"5", "4"})
	assert.Equal(t, 6, pick())
	c.Set("use_channel", []string{"5", "4", "6"})
	assert.Equal(t, 0, pick(), "every candidate tried: no channel left")
}

// ---- reseller customers: an auto model picks among the reseller's channels ----

// stubReseller makes user 9 a customer of reseller 3 whose matrix has
// channels, and answers the matrix's availability from the given map
// ("group/model"). Group availability is left empty: it must not be consulted.
func stubReseller(t *testing.T, available map[string]bool) string {
	t.Helper()
	stubAvailability(t, map[string]bool{})
	origPayer, origCfg, origAvail := orgPayerLookup, resellerConfigLookup, resellerModelAvailable
	orgPayerLookup = func(userId int) (*model.OrgPayerInfo, error) {
		if userId == 9 {
			return &model.OrgPayerInfo{ResellerOrgId: 3}, nil
		}
		return &model.OrgPayerInfo{}, nil
	}
	resellerConfigLookup = func(resellerOrgId int) *model.ResellerRouting {
		return &model.ResellerRouting{ChannelIdList: []int{1, 2}}
	}
	group := model.ResellerRoutingGroup(3)
	resellerModelAvailable = func(_ *gin.Context, g string, _ *model.ResellerRouting, candidate, _ string) bool {
		return available[g+"/"+candidate]
	}
	t.Cleanup(func() {
		orgPayerLookup, resellerConfigLookup, resellerModelAvailable = origPayer, origCfg, origAvail
	})
	return group
}

func newResellerTestContext(t *testing.T, userId int, body string) *gin.Context {
	t.Helper()
	c := newTieredTestContext(t, body)
	c.Set("id", userId)
	return c
}

func TestResolveAutoModelForResellerCustomer(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withAffinity(t, false)
	group := stubReseller(t, map[string]bool{})
	// the reseller's channels serve only m-big and m-top
	stubReseller(t, map[string]bool{group + "/m-big": true, group + "/m-top": true})

	t.Run("picks among the models the reseller's channels serve, upward only", func(t *testing.T) {
		c := newResellerTestContext(t, 9, chatBody("你好"))
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		require.True(t, isAuto)
		assert.Equal(t, "m-big", resolved)

		// The distributor has switched the group to the reseller's by the
		// time the retry loop asks again.
		next, ok := AdvanceAutoModel(c, group, "/v1/chat/completions")
		require.True(t, ok)
		assert.Equal(t, "m-top", next)
		_, ok = AdvanceAutoModel(c, group, "/v1/chat/completions")
		assert.False(t, ok)
	})

	t.Run("a customer of no reseller is judged by the group", func(t *testing.T) {
		stubAvailability(t, map[string]bool{"default/m-small": true})
		c := newResellerTestContext(t, 10, chatBody("你好"))
		resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.Equal(t, "m-small", resolved)
	})

	t.Run("the kill switch puts the customer back on the group", func(t *testing.T) {
		orig := setting.ResellerRoutingEnabled
		setting.ResellerRoutingEnabled = false
		t.Cleanup(func() { setting.ResellerRoutingEnabled = orig })
		stubAvailability(t, map[string]bool{"default/m-small": true})
		c := newResellerTestContext(t, 9, chatBody("你好"))
		resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.Equal(t, "m-small", resolved)
	})

	t.Run("the token auto group is not reseller-routed", func(t *testing.T) {
		stubAvailability(t, map[string]bool{"default/m-small": true})
		c := newResellerTestContext(t, 9, chatBody("你好"))
		resolved, _ := ResolveAutoModel(c, "auto", "auto", "/v1/chat/completions")
		assert.Equal(t, "m-small", resolved)
	})

	t.Run("nothing the reseller serves: rejected", func(t *testing.T) {
		stubReseller(t, map[string]bool{})
		c := newResellerTestContext(t, 9, chatBody("你好"))
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
		assert.True(t, isAuto)
		assert.Empty(t, resolved)
	})
}

// Channel affinity entries of a reseller customer are keyed by the reseller's
// group, because that is the group the request is routed in; the affinity
// check of an auto model has to look under it, not under the customer's own.
func TestAutoModelAffinityUsesTheResellerGroup(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	rule := withAffinity(t, true)
	affinity := operation_setting.GetChannelAffinitySetting()
	affinity.Rules[0].IncludeUsingGroup = true
	rule.IncludeUsingGroup = true
	group := stubReseller(t, map[string]bool{})
	stubReseller(t, map[string]bool{group + "/m-small": true, group + "/m-mid": true, group + "/m-big": true})
	orig := autoAffinityChannelUsable
	autoAffinityChannelUsable = func(_ *gin.Context, _ int, _, _, _ string) bool { return true }
	t.Cleanup(func() { autoAffinityChannelUsable = orig })

	session := fmt.Sprintf("reseller-session-%d", time.Now().UnixNano())
	cache := getChannelAffinityCache()
	stickyKey := autoStickySuffix(rule, "auto", group, session)
	channelKey := buildChannelAffinityCacheKeySuffix(rule, "m-big", group, session)
	require.NoError(t, cache.SetWithTTL(stickyKey, autoroute.EncodeSticky(autoroute.TierComplex, "m-big"), time.Minute))
	require.NoError(t, cache.SetWithTTL(channelKey, 7, time.Minute))
	t.Cleanup(func() { _, _ = cache.DeleteMany([]string{stickyKey, channelKey}) })

	c := newResellerTestContext(t, 9, chatBody("继续"))
	c.Request.Header.Set("X-Session", session)
	resolved, isAuto := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
	require.True(t, isAuto)
	assert.Equal(t, "m-big", resolved, "found under the reseller's group, not the customer's")
	assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "via=affinity")
}

// ---- failover at the first channel selection ----

func TestChannelSelectErrorCanTryNextModel(t *testing.T) {
	assert.True(t, (&ChannelSelectError{NoAvailableChannel: true}).CanTryNextModel())
	assert.True(t, (&ChannelSelectError{MessageID: i18n.MsgDistributorGetChannelFailed}).CanTryNextModel())
	// the caller's own constraints are not the model's fault
	assert.False(t, (&ChannelSelectError{StatusCode: 503, Message: "strict_session_binding_unavailable"}).CanTryNextModel())
	assert.False(t, (&ChannelSelectError{StatusCode: 400}).CanTryNextModel())
	assert.False(t, (*ChannelSelectError)(nil).CanTryNextModel())
}

func TestWithAutoFailover(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withAffinity(t, false)
	path := "/v1/chat/completions"

	// run resolves a greeting (m-small first) and then attempts each candidate:
	// served names the ones that can be served, fixable says whether a failure
	// is one another model could fix.
	run := func(t *testing.T, available map[string]bool, served map[string]bool, fixable bool) (tried []string, result string, c *gin.Context) {
		t.Helper()
		stubAvailability(t, available)
		c = newTieredTestContext(t, chatBody("你好"))
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", path)
		require.True(t, isAuto)
		result, model := WithAutoFailover(c, "default", path, resolved, func(candidate string) (string, bool, bool) {
			tried = append(tried, candidate)
			return candidate, served[candidate], fixable
		})
		_ = result
		return tried, model, c
	}
	all := map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-big": true, "default/m-top": true}

	t.Run("a model that fails moves on until one is served", func(t *testing.T) {
		tried, model, c := run(t, all, map[string]bool{"m-big": true}, true)
		assert.Equal(t, []string{"m-small", "m-mid", "m-big"}, tried)
		assert.Equal(t, "m-big", model)
		assert.Equal(t, "m-big", finalAutoModel(c), "the cursor follows, so the model that served the request is the one remembered")
		assert.Contains(t, c.Writer.Header().Get(AutoRouteHeader), "via=failover")
	})

	t.Run("candidates that are not available are skipped", func(t *testing.T) {
		onlySmallAndBig := map[string]bool{"default/m-small": true, "default/m-big": true}
		tried, model, _ := run(t, onlySmallAndBig, map[string]bool{"m-big": true}, true)
		assert.Equal(t, []string{"m-small", "m-big"}, tried, "m-mid has no available channel, so it is never attempted")
		assert.Equal(t, "m-big", model)
	})

	t.Run("nothing else is available: the failure stands", func(t *testing.T) {
		onlySmall := map[string]bool{"default/m-small": true}
		tried, model, _ := run(t, onlySmall, map[string]bool{}, true)
		assert.Equal(t, []string{"m-small"}, tried)
		assert.Equal(t, "auto", model)
	})

	t.Run("a failure another model cannot fix stops at once", func(t *testing.T) {
		tried, model, _ := run(t, all, map[string]bool{"m-big": true}, false)
		assert.Equal(t, []string{"m-small"}, tried)
		assert.Equal(t, "auto", model)
	})

	t.Run("when every candidate fails the error names the auto model", func(t *testing.T) {
		tried, model, _ := run(t, all, map[string]bool{}, true)
		assert.Equal(t, []string{"m-small", "m-mid", "m-big", "m-top"}, tried)
		assert.Equal(t, "auto", model)
	})

	t.Run("a request that is not an auto model is attempted once", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		calls := 0
		_, model := WithAutoFailover(c, "default", path, "gpt-4o", func(candidate string) (int, bool, bool) {
			calls++
			return 0, false, true
		})
		assert.Equal(t, 1, calls)
		assert.Equal(t, "gpt-4o", model)
	})

	t.Run("the affinity recorded for the previous model is forgotten", func(t *testing.T) {
		stubAvailability(t, all)
		c := newTieredTestContext(t, chatBody("你好"))
		resolved, _ := ResolveAutoModel(c, "default", "auto", path)
		c.Set(ginKeyChannelAffinityCacheKey, "stale-key-of-m-small")
		c.Set(ginKeyChannelAffinityTTLSeconds, 60)
		c.Set(ginKeyChannelAffinityMeta, channelAffinityMeta{CacheKey: "stale-key-of-m-small", RuleName: "r"})
		RequestPolicy(c).SessionMode = "strict"

		_, _ = WithAutoFailover(c, "default", path, resolved, func(candidate string) (int, bool, bool) {
			return 0, candidate == "m-mid", true
		})
		_, _, found := getChannelAffinityContext(c)
		assert.False(t, found, "RecordChannelAffinity must not file the new model's channel under the old model's key")
		_, metaFound := getChannelAffinityMeta(c)
		assert.False(t, metaFound)
		assert.Empty(t, RequestPolicy(c).SessionMode)
	})
}

// ---- conversation stickiness: requests no channel affinity rule applies to ----

// withUnmatchedAffinity turns the affinity feature on with a rule that matches
// none of the test models, so that no request has an affinity key.
func withUnmatchedAffinity(t *testing.T, enabled bool) {
	t.Helper()
	affinity := operation_setting.GetChannelAffinitySetting()
	origEnabled, origRules := affinity.Enabled, affinity.Rules
	affinity.Enabled = enabled
	affinity.Rules = []operation_setting.ChannelAffinityRule{{
		Name:       "matches-nothing",
		ModelRegex: []string{"^no-such-model$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}},
	}}
	t.Cleanup(func() { affinity.Enabled, affinity.Rules = origEnabled, origRules })
}

// turnsBody is a chat request carrying a whole conversation: even entries are
// the user's, odd ones the assistant's.
func turnsBody(turns ...string) string {
	messages := make([]string, 0, len(turns))
	for i, text := range turns {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, `{"role":"`+role+`","content":"`+text+`"}`)
	}
	return `{"model":"auto","messages":[` + strings.Join(messages, ",") + `]}`
}

const (
	hardQuestion    = "请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊"
	complexQuestion = "用Go实现一个分布式缓存，需要考虑一致性和高并发，写出核心代码和接口定义"
)

func TestConversationStickiness(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	withUnmatchedAffinity(t, true)
	all := map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-big": true, "default/m-top": true}
	stubAvailability(t, all)
	path := "/v1/chat/completions"

	// serve resolves a request of the conversation and, as the distributor does
	// after a success, remembers the model it ended on.
	serve := func(userId int, turns ...string) (string, *gin.Context) {
		c := newTieredTestContext(t, turnsBody(turns...))
		c.Set("id", userId)
		resolved, isAuto := ResolveAutoModel(c, "default", "auto", path)
		require.True(t, isAuto)
		if resolved != "" {
			RecordAutoModelAffinity(c)
		}
		return resolved, c
	}
	header := func(c *gin.Context) string { return c.Writer.Header().Get(AutoRouteHeader) }

	t.Run("a short follow-up keeps the model of the conversation", func(t *testing.T) {
		first, c1 := serve(1, hardQuestion)
		assert.Equal(t, "m-top", first)
		assert.Contains(t, header(c1), "via=classifier")
		second, c2 := serve(1, hardQuestion, "reply", "继续")
		assert.Equal(t, "m-top", second, "without this the follow-up would be rated simple and answered by a weaker model")
		assert.Contains(t, header(c2), "via=conversation")
		assert.Contains(t, header(c2), "tier=reasoning")
	})

	t.Run("a harder question upgrades the conversation and an easier one never downgrades it", func(t *testing.T) {
		first, _ := serve(2, "你好")
		assert.Equal(t, "m-small", first)
		upgraded, c2 := serve(2, "你好", "hi", hardQuestion)
		assert.Equal(t, "m-top", upgraded, "the question is harder than anything asked so far")
		assert.Contains(t, header(c2), "via=classifier")
		followUp, c3 := serve(2, "你好", "hi", hardQuestion, "reply", "继续")
		assert.Equal(t, "m-top", followUp)
		assert.Contains(t, header(c3), "via=conversation")
	})

	t.Run("another user opening the same way is a separate conversation", func(t *testing.T) {
		serve(3, "你好")
		other, c := serve(4, "你好")
		assert.Equal(t, "m-small", other)
		assert.Contains(t, header(c), "via=classifier")
	})

	t.Run("a conversation with another opening is a new conversation", func(t *testing.T) {
		serve(5, hardQuestion)
		fresh, c := serve(5, "你好", "hi", "继续")
		assert.Equal(t, "m-small", fresh)
		assert.Contains(t, header(c), "via=classifier")
	})

	t.Run("a remembered model that is unavailable moves to a stronger one, never a weaker one", func(t *testing.T) {
		first, _ := serve(6, complexQuestion)
		require.Equal(t, "m-big", first)
		unavailable := map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-top": true}
		stubAvailability(t, unavailable) // restored to the parent's by the subtest's own cleanup
		next, c := serve(6, complexQuestion, "reply", "继续")
		assert.Equal(t, "m-top", next)
		assert.Contains(t, header(c), "via=classifier")
		assert.Contains(t, header(c), "tier=complex", "the conversation had reached complex, so a simple follow-up is not served by m-small")
	})

	t.Run("a model that left the configuration is forgotten", func(t *testing.T) {
		serve(7, complexQuestion)
		setupAutoModels(t, `[{"name":"auto","tiers":{"simple":["m-small"],"medium":["m-mid"],"reasoning":["m-top"]}}]`)
		next, _ := serve(7, complexQuestion, "reply", "继续")
		assert.NotEqual(t, "m-big", next)
	})

	t.Run("it is off with the affinity feature", func(t *testing.T) {
		setupAutoModels(t, tieredAutoConfig) // the previous subtest replaced the configuration
		withUnmatchedAffinity(t, false)
		serve(8, hardQuestion)
		next, c := serve(8, hardQuestion, "reply", "继续")
		assert.Equal(t, "m-small", next)
		assert.Contains(t, header(c), "via=classifier")
	})
}

// A request that a channel affinity rule applies to keeps using channel
// affinity, as before: the conversation fingerprint is not consulted.
func TestConversationStickinessLeavesRuleKeyedRequestsAlone(t *testing.T) {
	setupAutoModels(t, tieredAutoConfig)
	rule := withAffinity(t, true)
	stubAvailability(t, map[string]bool{"default/m-small": true, "default/m-mid": true, "default/m-big": true, "default/m-top": true})
	c := newTieredTestContext(t, turnsBody(hardQuestion))
	c.Request.Header.Set("X-Session", "keyed-session")
	c.Set("id", 1)
	resolved, _ := ResolveAutoModel(c, "default", "auto", "/v1/chat/completions")
	require.Equal(t, "m-top", resolved)
	info := getAutoRouteInfo(c)
	require.NotNil(t, info)
	assert.False(t, info.ByConversation)
	assert.Equal(t, autoStickySuffix(rule, "auto", "default", "keyed-session"), info.AffinityKey, "the rule's key, not the conversation's")
}
