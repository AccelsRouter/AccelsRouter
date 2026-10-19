package service

// Auto virtual model resolution (fork-local; config in setting/auto_model.go).
//
// Flow: the distributor calls ResolveAutoModel BEFORE channel selection and
// billing, so every downstream consumer (pricing, abilities, logs, upstream
// mapping) only ever sees a concrete model. When the relay retry loop
// exhausts the current model's channels, AdvanceAutoModel moves a monotonic
// cursor to the next available candidate; total attempts stay bounded by the
// caller's global retry cap, so failover can never loop.
//
// Two kinds of auto model exist (see setting/auto_model.go):
//
//   - fixed order: the first candidate with an available channel wins.
//   - by content: channel affinity has the highest priority. If the request
//     belongs to a conversation that already has a model and a usable
//     affinity channel, that model is used and nothing is classified.
//     Otherwise the request's text is classified (common/autoroute) and the
//     matching tier's models are tried in order. The model a request ends
//     up on is remembered next to the channel affinity entry (see
//     auto_model_affinity.go), so the next request of the conversation
//     is served from it.

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/autoroute"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// AutoRouteHeader is the response header that says how an auto model request
// was routed, for example "tier=complex; via=classifier; score=0.55".
const AutoRouteHeader = "X-Auto-Router"

const ginKeyAutoRouteInfo = "auto_route_info"

// How an auto model request got its model.
const (
	autoViaClassifier = "classifier" // the request's text was rated
	autoViaAffinity   = "affinity"   // the conversation's model and channel were reused
	autoViaFailover   = "failover"   // the first model's channels were exhausted
	// The conversation's model was reused: see conversationKey.
	autoViaConversation = "conversation"
)

// autoRouteInfo is what the gateway remembers about one auto model request.
// It lives in the gin context and is read again when the log entry is built
// and when the request ends successfully.
type autoRouteInfo struct {
	Tier  autoroute.Tier
	Score float64
	Cause string // why the classifier chose Tier, or "affinity"
	Via   string

	// Set when channel affinity applies to the request: where the
	// conversation's model is remembered.
	HasAffinityKey bool
	AffinityKey    string
	AffinityTTL    int
	// The key is the conversation's own (see conversationKey), not one of a
	// channel affinity rule.
	ByConversation bool
}

func getAutoRouteInfo(c *gin.Context) *autoRouteInfo {
	if c == nil {
		return nil
	}
	value, ok := c.Get(ginKeyAutoRouteInfo)
	if !ok {
		return nil
	}
	info, _ := value.(*autoRouteInfo)
	return info
}

func setAutoRouteHeader(c *gin.Context, info *autoRouteInfo) {
	header := fmt.Sprintf("tier=%s; via=%s", info.Tier, info.Via)
	if info.Via == autoViaClassifier {
		header += fmt.Sprintf("; score=%.2f", info.Score)
	}
	c.Header(AutoRouteHeader, header)
}

// autoModelGroups expands the effective group into the concrete group list
// used for availability checks ("auto" fans out to the token's auto groups).
func autoModelGroups(c *gin.Context, usingGroup string) []string {
	if usingGroup != "auto" {
		return []string{usingGroup}
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	return GetRequestAutoGroups(c, userGroup)
}

// groupModelAvailable is indirected so tests can stub channel availability
// without a database or channel cache.
var groupModelAvailable = model.GroupModelHasEnabledChannel

func autoModelCandidateAvailable(groups []string, candidate string, requestPath string) bool {
	for _, g := range groups {
		if groupModelAvailable(g, candidate, requestPath) {
			return true
		}
	}
	return false
}

// autoAvailability reports whether a candidate model can be served to the
// requesting user right now. An auto model only ever picks among models the
// user can reach, and "reach" depends on how the user is routed: through the
// channels of their group(s), or, in channel pricing mode, through the
// channels they have a binding for.
type autoAvailability func(candidate string) bool

func groupAvailability(groups []string, requestPath string) autoAvailability {
	return func(candidate string) bool {
		return autoModelCandidateAvailable(groups, candidate, requestPath)
	}
}

func userBindingAvailability(userId int, requestPath string) autoAvailability {
	return func(candidate string) bool {
		return userModelBound(userId, candidate, requestPath)
	}
}

// firstAvailable returns the index of the first plan entry that is available,
// or -1.
func firstAvailable(available autoAvailability, plan []string) int {
	for i, candidate := range plan {
		if available(candidate) {
			return i
		}
	}
	return -1
}

// ResolveAutoModel resolves an auto virtual model to a concrete model.
// Returns (concrete, true) on success. When requested is not an auto model
// it returns ("", false) and the caller proceeds unchanged. When it IS an
// auto model but nothing is currently available, it returns ("", true) with
// concrete == "" — the caller must reject the request rather than fall
// through with the virtual name.
func ResolveAutoModel(c *gin.Context, usingGroup string, requested string, requestPath string) (string, bool) {
	routing := autoRoutingFor(c, usingGroup, requestPath)
	return resolveAuto(c, routing.group, requested, requestPath, routing.available, routing.scope)
}

// ResolveAutoModelForUser is ResolveAutoModel for a channel-pricing-mode user,
// who has no usable group: a candidate is available when the user has a
// bound, enabled channel that serves it. The request is marked so that
// failover (AdvanceAutoModel) and the affinity checks use the same rule.
func ResolveAutoModelForUser(c *gin.Context, userId int, requested string, requestPath string) (string, bool) {
	if userId <= 0 || !setting.IsAutoModel(requested) {
		return "", false
	}
	c.Set(ginKeyAutoPricingUser, userId)
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	routing := autoRoutingFor(c, usingGroup, requestPath)
	return resolveAuto(c, routing.group, requested, requestPath, routing.available, routing.scope)
}

// resolveAuto resolves requested with the given availability test. scope names
// what was searched, for the log line when nothing is available.
func resolveAuto(c *gin.Context, usingGroup string, requested string, requestPath string, available autoAvailability, scope string) (string, bool) {
	am, ok := setting.GetAutoModel(requested)
	if !ok {
		return "", false
	}
	if am.IsTiered() {
		return resolveTieredAutoModel(c, am, usingGroup, available, requestPath, scope)
	}

	candidates := am.FixedCandidates()
	index := firstAvailable(available, candidates)
	if index < 0 {
		logger.LogWarn(c.Request.Context(),
			"auto model "+requested+" has no available candidate for "+scope)
		return "", true
	}
	return finishAutoResolve(c, requested, candidates, index, nil)
}

// resolveTieredAutoModel is resolveAuto for a model routed by content.
func resolveTieredAutoModel(c *gin.Context, am *setting.AutoModel, usingGroup string, available autoAvailability, requestPath string, scope string) (string, bool) {
	info := &autoRouteInfo{}

	// 1. Channel affinity has the highest priority. Only a request that
	// does not satisfy it goes on to be classified.
	probe := probeAutoAffinity(c, am.Name(), usingGroup, am.AllCandidates())
	if probe.ok {
		info.HasAffinityKey, info.AffinityKey, info.AffinityTTL = true, probe.stickySuffix, probe.ttlSeconds
		if remembered, tier, ok := affinityAutoModel(c, am, probe, usingGroup, available, requestPath); ok {
			info.Tier, info.Cause, info.Via = tier, autoViaAffinity, autoViaAffinity
			return finishAutoResolve(c, am.Name(), am.PlanFrom(remembered, tier), 0, info)
		}
	}

	// 2. Rate the request and use the models of its tier.
	extracted := autoroute.ExtractRequest(requestPath, autoRequestBody(c))
	rated := am.Classifier().Classify(extracted.Input)
	tier := rated.Tier

	// No channel affinity rule applies to this request, so nothing else keeps a
	// conversation on one model: remember it on our own, by the conversation's
	// fingerprint. The conversation keeps its model unless the request is
	// harder than anything the conversation has asked so far, and the tier
	// never goes down within it.
	if !probe.ok {
		if key, ttl, ok := conversationKey(c, am.Name(), usingGroup, extracted.Fingerprint); ok {
			info.HasAffinityKey, info.AffinityKey, info.AffinityTTL, info.ByConversation = true, key, ttl, true
			if remembered, storedTier, found := rememberedConversationModel(am, key); found {
				if storedTier >= tier {
					if available(remembered) {
						info.Tier, info.Score, info.Cause, info.Via = storedTier, rated.Score, autoViaConversation, autoViaConversation
						return finishAutoResolve(c, am.Name(), am.PlanFrom(remembered, storedTier), 0, info)
					}
					logger.LogInfo(c.Request.Context(), fmt.Sprintf(
						"auto model %s: the conversation's model %s (tier %s) is not available now, using the models of the tier instead",
						am.Name(), remembered, storedTier))
				}
				if storedTier > tier {
					tier = storedTier
				}
			}
		}
	}

	plan := am.FailoverPlan(tier)
	index := firstAvailable(available, plan)
	if index < 0 {
		logger.LogWarn(c.Request.Context(),
			"auto model "+am.Name()+" has no available candidate for tier "+tier.String()+
				" for "+scope)
		return "", true
	}
	info.Tier, info.Score, info.Cause, info.Via = tier, rated.Score, rated.Cause, autoViaClassifier
	return finishAutoResolve(c, am.Name(), plan, index, info)
}

// finishAutoResolve records the resolution in the request context. plan is
// the failover order, index the entry in use. info is nil for a fixed-order
// model.
func finishAutoResolve(c *gin.Context, requested string, plan []string, index int, info *autoRouteInfo) (string, bool) {
	resolved := plan[index]
	common.SetContextKey(c, constant.ContextKeyAutoModelOriginal, requested)
	common.SetContextKey(c, constant.ContextKeyAutoModelCandidates, plan)
	common.SetContextKey(c, constant.ContextKeyAutoModelIndex, index)
	message := "auto model " + requested + " resolved to " + resolved
	if info != nil {
		c.Set(ginKeyAutoRouteInfo, info)
		setAutoRouteHeader(c, info)
		message += fmt.Sprintf(" (tier=%s via=%s cause=%s score=%.2f)", info.Tier, info.Via, info.Cause, info.Score)
	}
	logger.LogInfo(c.Request.Context(), message)
	return resolved, true
}

// autoRequestBody returns the raw request body, or nil when it cannot be read.
// A body large enough to have been spooled to disk is not read back into
// memory just to be rated; it classifies as the default tier.
func autoRequestBody(c *gin.Context) []byte {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage.IsDisk() {
		return nil
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil
	}
	return body
}

// AdvanceAutoModel moves the failover cursor to the next available candidate
// after the current model's channels are exhausted. The cursor only moves
// forward, so with at most len(candidates) entries this terminates
// unconditionally. Returns ("", false) when the pool is exhausted or the
// request never was an auto model.
func AdvanceAutoModel(c *gin.Context, usingGroup string, requestPath string) (string, bool) {
	rawCandidates, ok := common.GetContextKey(c, constant.ContextKeyAutoModelCandidates)
	if !ok {
		return "", false
	}
	candidates, ok := rawCandidates.([]string)
	if !ok || len(candidates) == 0 {
		return "", false
	}
	index := common.GetContextKeyInt(c, constant.ContextKeyAutoModelIndex)
	if index < 0 {
		return "", false
	}

	available := autoRoutingFor(c, usingGroup, requestPath).available
	for next := index + 1; next < len(candidates); next++ {
		if !available(candidates[next]) {
			continue
		}
		common.SetContextKey(c, constant.ContextKeyAutoModelIndex, next)
		if info := getAutoRouteInfo(c); info != nil {
			info.Via = autoViaFailover
			setAutoRouteHeader(c, info)
		}
		logger.LogInfo(c.Request.Context(),
			"auto model "+common.GetContextKeyString(c, constant.ContextKeyAutoModelOriginal)+
				" failing over to "+candidates[next])
		return candidates[next], true
	}
	return "", false
}
