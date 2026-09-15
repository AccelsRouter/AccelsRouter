// Admin endpoints for managing "channel pricing mode" user-channel
// bindings. See model.UserChannelBinding and model.BillingModeChannelPricing.
// Not upstream — this whole feature is fork-specific.
package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ListUserChannelBindings — GET /api/user/:id/channel-bindings
// Returns every channel this user is bound to, with each binding's ratio.
func ListUserChannelBindings(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	bindings, err := model.GetUserChannelBindings(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, bindings)
}

type upsertUserChannelBindingRequest struct {
	ChannelId int     `json:"channel_id" binding:"required"`
	Ratio     float64 `json:"ratio"`
}

// UpsertUserChannelBinding — POST /api/user/:id/channel-bindings
// Binds (or updates the ratio of an existing binding to) a channel for this
// user. No check on the channel's current status — binding a disabled or
// otherwise broken channel is allowed; see model.GetChannelPricingChannel
// for how that's handled at actual routing time.
func UpsertUserChannelBinding(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	var req upsertUserChannelBindingRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.ChannelId <= 0 {
		common.ApiErrorMsg(c, "invalid params")
		return
	}
	ratio := req.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	if err := model.UpsertUserChannelBinding(userId, req.ChannelId, ratio); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteUserChannelBinding — DELETE /api/user/:id/channel-bindings/:channelId
// Unbinds a channel from this user. Not an error if the binding didn't
// exist.
func DeleteUserChannelBinding(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	channelId, err := strconv.Atoi(c.Param("channelId"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	if err := model.DeleteUserChannelBinding(userId, channelId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
