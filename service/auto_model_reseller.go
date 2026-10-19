package service

// Auto models for reseller customers (fork-local).
//
// A reseller customer's request is routed only through the channels the
// platform admin bound to its reseller (see the reseller step of the
// distributor): the distributor switches the request's group to the
// reseller's group and the channel is picked from the reseller's matrix
// (SelectResellerChannel). An auto model has to judge each candidate by that
// same rule, or it would pick a model the customer cannot be served, so the
// test here is the one the selection itself uses, resellerTiers: a bound,
// enabled channel that serves the model, matches the request path and is under
// its daily budget.
//
// Personal BYOK channels are not considered: for an auto model a user's own
// BYOK channel does not make a candidate available.

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// Indirected so tests can stub the reseller matrix and the payer lookup.
var (
	resellerConfigLookup = model.GetResellerRoutingCached
	orgPayerLookup       = model.GetOrgPayerInfo

	resellerModelAvailable = func(c *gin.Context, group string, cfg *model.ResellerRouting, candidate, requestPath string) bool {
		return len(resellerTiers(&RetryParam{Ctx: c, TokenGroup: group, ModelName: candidate, RequestPath: requestPath}, cfg)) > 0
	}
)

// autoResellerTarget reports whether the request is routed through a reseller's
// channels, and which group and matrix. It covers both moments an auto model
// asks: before the distributor has switched the group (usingGroup is the
// customer's own group; the same conditions as the distributor's reseller
// step decide), and during a retry, when the group is already the reseller's.
func autoResellerTarget(c *gin.Context, usingGroup string) (group string, cfg *model.ResellerRouting, ok bool) {
	if resellerId, parsed := model.ParseResellerRoutingGroup(usingGroup); parsed {
		matrix := resellerConfigLookup(resellerId)
		if matrix == nil || len(matrix.ChannelIdList) == 0 {
			return "", nil, false
		}
		return usingGroup, matrix, true
	}
	if c == nil || !setting.ResellerRoutingEnabled || usingGroup == "auto" {
		return "", nil, false
	}
	payer, err := orgPayerLookup(c.GetInt("id"))
	if err != nil || payer == nil || payer.ResellerOrgId <= 0 {
		return "", nil, false
	}
	matrix := resellerConfigLookup(payer.ResellerOrgId)
	if matrix == nil || len(matrix.ChannelIdList) == 0 {
		return "", nil, false
	}
	return model.ResellerRoutingGroup(payer.ResellerOrgId), matrix, true
}

// autoRouting is what an auto model needs to know about how the requesting
// user is served.
type autoRouting struct {
	available autoAvailability
	// The group the request is ultimately routed in: the reseller's group for
	// a reseller customer. Channel affinity entries are keyed by it.
	group string
	// Names what was searched, for log lines.
	scope string
}

// autoRoutingFor mirrors, for the request in c, how it will be routed: through
// the user's bound channels (channel pricing), through a reseller's channels,
// or through the user's group(s).
func autoRoutingFor(c *gin.Context, usingGroup string, requestPath string) autoRouting {
	if userId := autoPricingUserID(c); userId > 0 {
		return autoRouting{
			available: userBindingAvailability(userId, requestPath),
			group:     usingGroup,
			scope:     "the channels bound to the user",
		}
	}
	if group, matrix, ok := autoResellerTarget(c, usingGroup); ok {
		return autoRouting{
			available: func(candidate string) bool {
				return resellerModelAvailable(c, group, matrix, candidate, requestPath)
			},
			group: group,
			scope: "the channels of reseller routing group " + group,
		}
	}
	groups := autoModelGroups(c, usingGroup)
	return autoRouting{
		available: groupAvailability(groups, requestPath),
		group:     usingGroup,
		scope:     "groups [" + strings.Join(groups, ",") + "]",
	}
}
