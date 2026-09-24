package service

// Auto virtual model resolution (fork-local; config in setting/auto_model.go).
//
// Flow: the distributor calls ResolveAutoModel BEFORE affinity, channel
// selection, and billing, so every downstream consumer (pricing, abilities,
// logs, upstream mapping) only ever sees a concrete model. When the relay
// retry loop exhausts the current model's channels, AdvanceAutoModel moves a
// monotonic cursor to the next available candidate; total attempts stay
// bounded by the caller's global retry cap, so failover can never loop.

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

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

// ResolveAutoModel resolves an auto virtual model to the first candidate with
// an available channel. Returns (concrete, true) on success. When requested
// is not an auto model it returns ("", false) and the caller proceeds
// unchanged. When it IS an auto model but no candidate is currently
// available, it returns ("", true) with concrete == "" — the caller must
// reject the request rather than fall through with the virtual name.
func ResolveAutoModel(c *gin.Context, usingGroup string, requested string, requestPath string) (string, bool) {
	candidates, ok := setting.GetAutoModelCandidates(requested)
	if !ok {
		return "", false
	}

	groups := autoModelGroups(c, usingGroup)
	resolvedIndex := -1
	resolved := ""
	for i, candidate := range candidates {
		if autoModelCandidateAvailable(groups, candidate, requestPath) {
			resolvedIndex = i
			resolved = candidate
			break
		}
	}
	if resolvedIndex < 0 {
		logger.LogWarn(c.Request.Context(),
			"auto model "+requested+" has no available candidate for groups ["+strings.Join(groups, ",")+"]")
		return "", true
	}

	common.SetContextKey(c, constant.ContextKeyAutoModelOriginal, requested)
	common.SetContextKey(c, constant.ContextKeyAutoModelCandidates, candidates)
	common.SetContextKey(c, constant.ContextKeyAutoModelIndex, resolvedIndex)
	logger.LogInfo(c.Request.Context(),
		"auto model "+requested+" resolved to "+resolved)
	return resolved, true
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

	groups := autoModelGroups(c, usingGroup)
	for next := index + 1; next < len(candidates); next++ {
		if !autoModelCandidateAvailable(groups, candidates[next], requestPath) {
			continue
		}
		common.SetContextKey(c, constant.ContextKeyAutoModelIndex, next)
		logger.LogInfo(c.Request.Context(),
			"auto model "+common.GetContextKeyString(c, constant.ContextKeyAutoModelOriginal)+
				" failing over to "+candidates[next])
		return candidates[next], true
	}
	return "", false
}
