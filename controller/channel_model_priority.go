// Admin endpoints for managing per-(channel, model) priority overrides.
// See model.ChannelModel. Not upstream — fork-specific.
package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ListChannelModelPriorities — GET /api/channel/:id/model-priorities
// Returns every model priority override configured for this channel.
func ListChannelModelPriorities(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	rows, err := model.ListChannelModelPriorities(channelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

type upsertChannelModelPriorityRequest struct {
	ModelName string `json:"model_name" binding:"required"`
	Priority  int64  `json:"priority"`
}

// UpsertChannelModelPriority — POST /api/channel/:id/model-priorities
// Sets (or updates) this channel's priority override for one model. Other
// models on the same channel are untouched. Takes effect the next time
// this channel's abilities are (re)generated — i.e. the next save of the
// channel itself (AddAbilities/UpdateAbilities read the override table
// fresh each time), not instantaneously against already-generated
// Ability rows.
func UpsertChannelModelPriority(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	var req upsertChannelModelPriorityRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || strings.TrimSpace(req.ModelName) == "" {
		common.ApiErrorMsg(c, "invalid params")
		return
	}
	if err := model.UpsertChannelModelPriority(channelId, strings.TrimSpace(req.ModelName), req.Priority); err != nil {
		common.ApiError(c, err)
		return
	}
	ch, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := ch.UpdateAbilities(nil); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteChannelModelPriority — DELETE /api/channel/:id/model-priorities/:modelName
// Removes this channel's priority override for one model, reverting it
// back to the channel's own base Priority. Not an error if no override
// existed.
func DeleteChannelModelPriority(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	modelName := strings.TrimSpace(c.Param("modelName"))
	if modelName == "" {
		common.ApiErrorMsg(c, "invalid model name")
		return
	}
	if err := model.DeleteChannelModelPriority(channelId, modelName); err != nil {
		common.ApiError(c, err)
		return
	}
	ch, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := ch.UpdateAbilities(nil); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
