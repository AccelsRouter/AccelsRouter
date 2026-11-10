package controller

import (
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const customRemoveChannelModelsMaxItems = 500

type customRemoveChannelModelsItem struct {
	ChannelID int      `json:"channel_id"`
	Models    []string `json:"models"`
}

type customRemoveChannelModelsRequest struct {
	Items []customRemoveChannelModelsItem `json:"items"`
}

// CustomRemoveChannelModels takes the given models out of each channel's model
// list, e.g. to stop routing loss-making models to an expensive upstream.
func CustomRemoveChannelModels(c *gin.Context) {
	var req customRemoveChannelModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Items) == 0 || len(req.Items) > customRemoveChannelModelsMaxItems {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid items"})
		return
	}

	removed := make(map[int][]string)
	for _, item := range req.Items {
		if item.ChannelID <= 0 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid channel id"})
			return
		}
		channel, err := model.GetChannelById(item.ChannelID, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		origin := normalizeModelNames(channel.GetModels())
		next := subtractModelNames(origin, item.Models)
		if slices.Equal(origin, next) {
			continue
		}
		channel.Models = strings.Join(next, ",")
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", channel.Models).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		if err := channel.UpdateAbilities(nil); err != nil {
			common.ApiError(c, err)
			return
		}
		removed[channel.Id] = subtractModelNames(origin, next)
	}
	if len(removed) > 0 {
		refreshChannelRuntimeCache()
		recordManageAudit(c, "channel.remove_models", map[string]any{"channels": len(removed)})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"removed": removed}})
}
