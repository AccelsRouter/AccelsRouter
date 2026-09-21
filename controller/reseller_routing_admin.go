// Admin endpoints for reseller upstream routing (fork-only). Admin-scoped by the
// router; the reseller itself never sees channels, so nothing here is reachable
// from the reseller console.
package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type resellerRoutingResponse struct {
	*model.ResellerRouting
	EffectiveModels []string `json:"effective_models"`
	UncoveredModels []string `json:"uncovered_models"`
}

func resellerOrgFromPath(c *gin.Context) *model.Organization {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid organization id")
		return nil
	}
	org, err := model.GetOrganizationById(id)
	if err != nil || org == nil {
		common.ApiErrorMsg(c, "organization not found")
		return nil
	}
	if org.Type != model.OrgTypeReseller {
		common.ApiErrorMsg(c, "upstream routing applies to reseller organizations only")
		return nil
	}
	return org
}

// AdminGetResellerRouting — GET /api/admin/organizations/:id/routing
func AdminGetResellerRouting(c *gin.Context) {
	org := resellerOrgFromPath(c)
	if org == nil {
		return
	}
	cfg, err := model.GetResellerRouting(org.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if cfg == nil {
		cfg = &model.ResellerRouting{ResellerOrgId: org.Id, ChannelIdList: []int{}, RuleList: []model.ResellerRoutingRule{}}
	}
	effective, uncovered, err := model.ResellerEffectiveModels(org, cfg.ChannelIdList)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resellerRoutingResponse{ResellerRouting: cfg, EffectiveModels: effective, UncoveredModels: uncovered})
}

type setResellerRoutingRequest struct {
	ChannelIds  []int                       `json:"channel_ids"`
	Rules       []model.ResellerRoutingRule `json:"rules"`
	Fallback    bool                        `json:"fallback"`
	AffinityOff bool                        `json:"affinity_off"`
}

// AdminSetResellerRouting — PUT /api/admin/organizations/:id/routing
// Replaces the reseller's whole routing config atomically (bound channels,
// per-model matrix, fallback and affinity switches). Validation and caps live
// in model.ValidateResellerRouting.
func AdminSetResellerRouting(c *gin.Context) {
	org := resellerOrgFromPath(c)
	if org == nil {
		return
	}
	var req setResellerRoutingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	cfg := &model.ResellerRouting{
		ResellerOrgId: org.Id,
		ChannelIdList: req.ChannelIds,
		RuleList:      req.Rules,
		Fallback:      req.Fallback,
		AffinityOff:   req.AffinityOff,
	}
	if err := model.SaveResellerRouting(cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	effective, uncovered, err := model.ResellerEffectiveModels(org, cfg.ChannelIdList)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resellerRoutingResponse{ResellerRouting: cfg, EffectiveModels: effective, UncoveredModels: uncovered})
}

// AdminListResellerRoutingChannels — GET /api/admin/organizations/:id/routing/channels
// Candidate channels for the matrix editor in admin-safe summary form (no keys).
func AdminListResellerRoutingChannels(c *gin.Context) {
	if resellerOrgFromPath(c) == nil {
		return
	}
	rows, err := model.ListResellerRoutingChannelSummaries()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
