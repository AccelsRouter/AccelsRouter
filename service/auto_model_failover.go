package service

// Failover for auto models at the first channel selection (fork-local).
//
// The relay retry loop already moves an auto model on to its next candidate
// when the current model's channels are exhausted (AdvanceAutoModel). The
// first selection, in the distributor, used to just fail the request with a
// 503. WithAutoFailover gives it the same behaviour: if the model cannot be
// served right now, try the next candidate, until one can or none are left.

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

// CanTryNextModel reports whether a selection failure means "no channel can
// serve this model right now", the only kind of failure a different model can
// fix. A pinned channel that is unavailable, a strict session binding or a
// request that filters reject are the caller's constraints, not the model's
// fault, and stay failures.
func (e *ChannelSelectError) CanTryNextModel() bool {
	return e != nil && (e.NoAvailableChannel || e.MessageID == i18n.MsgDistributorGetChannelFailed)
}

// WithAutoFailover runs attempt for modelName. attempt reports its result,
// whether the model was served, and whether a different model could help. For
// an auto model request that was not served, and could be helped, it moves the
// request to the next candidate (AdvanceAutoModel) and runs attempt again,
// until a candidate is served or the candidates run out. For any other
// request it runs attempt once.
//
// The model returned is the one that was served. When nothing could be served
// for an auto model it is the auto model's own name, which is what an error
// message should name, not whichever candidate happened to be tried last.
func WithAutoFailover[R any](c *gin.Context, usingGroup, requestPath, modelName string,
	attempt func(candidate string) (result R, served bool, canTryNext bool)) (R, string) {
	current := modelName
	for {
		result, served, canTryNext := attempt(current)
		if served {
			return result, current
		}
		if canTryNext {
			if next, ok := AdvanceAutoModel(c, usingGroup, requestPath); ok {
				forgetAffinityOfPreviousModel(c)
				current = next
				continue
			}
		}
		if original := common.GetContextKeyString(c, constant.ContextKeyAutoModelOriginal); original != "" {
			return result, original
		}
		return result, current
	}
}

// forgetAffinityOfPreviousModel drops what the last selection recorded about
// channel affinity. The selection of the previous model stored the affinity
// cache key and rule in the request, and RecordChannelAffinity would write the
// channel that finally served the request under that key. If the next model
// matches no affinity rule nothing overwrites it, so the entry would be filed
// under the wrong model.
func forgetAffinityOfPreviousModel(c *gin.Context) {
	c.Set(ginKeyChannelAffinityCacheKey, nil)
	c.Set(ginKeyChannelAffinityTTLSeconds, nil)
	c.Set(ginKeyChannelAffinityMeta, nil)
	state := RequestPolicy(c)
	state.SessionMode, state.SessionModeSource, state.RuleName = "", "", ""
}
