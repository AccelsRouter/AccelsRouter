// Admin endpoints for managing "channel pricing mode" user-channel
// bindings. See model.UserChannelBinding and model.BillingModeChannelPricing.
// Not upstream — this whole feature is fork-specific.
package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ListUserChannelBindings — GET /api/user/:id/channel-bindings
// Returns every (channel, model) binding for this user, with each row's
// own ratio.
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
	ModelName string  `json:"model_name" binding:"required"`
	Ratio     float64 `json:"ratio"`
}

// UpsertUserChannelBinding — POST /api/user/:id/channel-bindings
// Binds (or updates the ratio of an existing binding to) a channel for a
// specific model, for this user. Each model on a channel is its own row
// with its own ratio — binding gpt-4o at 0.9 doesn't touch a separate
// gpt-3.5-turbo binding on the same channel. No check on the channel's
// current status, or whether it's even configured to serve modelName —
// binding a disabled or mismatched channel/model pair is allowed; see
// model.GetChannelPricingChannel for how that's handled at actual routing
// time.
func UpsertUserChannelBinding(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	var req upsertUserChannelBindingRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.ChannelId <= 0 || strings.TrimSpace(req.ModelName) == "" {
		common.ApiErrorMsg(c, "invalid params")
		return
	}
	ratio := req.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	if err := model.UpsertUserChannelBinding(userId, req.ChannelId, strings.TrimSpace(req.ModelName), ratio); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteUserChannelBinding — DELETE /api/user/:id/channel-bindings/:channelId/:modelName
// Unbinds one (channel, model) pair from this user. Other models still
// bound on the same channel are untouched. Not an error if the binding
// didn't exist.
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
	modelName := strings.TrimSpace(c.Param("modelName"))
	if modelName == "" {
		common.ApiErrorMsg(c, "invalid model name")
		return
	}
	if err := model.DeleteUserChannelBinding(userId, channelId, modelName); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteUserChannelBindingsForChannel — DELETE /api/user/:id/channel-bindings/:channelId
// Unbinds every model this user has bound on channelId in one call — the
// "remove this channel entirely" action, as opposed to
// DeleteUserChannelBinding's "remove just this one model". Not an error if
// no bindings existed.
func DeleteUserChannelBindingsForChannel(c *gin.Context) {
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
	if err := model.DeleteUserChannelBindingsForChannel(userId, channelId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
