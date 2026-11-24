package controller

import (
	"maps"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// ListMyNotifications — GET /api/user/notifications
// The caller's newest notifications and how many are unread.
func ListMyNotifications(c *gin.Context) {
	items, unread, err := model.ListCustomNotifications(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "unread": unread})
}

// ReadMyNotifications — POST /api/user/notifications/read {up_to}
// Marks the caller's notifications as read, up to the newest one it was shown.
func ReadMyNotifications(c *gin.Context) {
	var req struct {
		UpTo int `json:"up_to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.MarkCustomNotificationsRead(c.GetInt("id"), req.UpTo); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// orgNotice words a notification about an organization: sentence may name the
// organization as {{org}} and the user it is about as {{user}} (0: nobody),
// and take further values.
func orgNotice(sentence string, org *model.Organization, userId int, values ...map[string]any) *common.Message {
	params := map[string]any{"org": org.Name}
	if userId > 0 {
		params["user"] = model.UserDisplayLabelsByIds([]int{userId})[userId]
	}
	for _, more := range values {
		maps.Copy(params, more)
	}
	return common.NewMessage(sentence, params)
}
