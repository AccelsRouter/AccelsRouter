package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/larknotify"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type larkTestRequest struct {
	Target string `json:"target"` // "enterprise" or "alert"
}

// TestLarkNotify sends a test card with the settings that are saved, so the
// administrator can see at once whether the address and the signing secret work.
// The error text comes from Lark and never contains the webhook address.
func TestLarkNotify(c *gin.Context) {
	var req larkTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid request body.")
		return
	}
	var (
		target     larknotify.Target
		purposeKey string
	)
	switch req.Target {
	case "enterprise":
		target, purposeKey = service.LarkEnterpriseTarget(), larknotify.MsgTestPurposeEnterprise
	case "alert":
		target, purposeKey = service.LarkAlertTarget(), larknotify.MsgTestPurposeAlert
	default:
		common.ApiErrorT(c, "Unknown notification target")
		return
	}
	if !target.Configured() {
		common.ApiErrorT(c, "This bot has no webhook address yet. Save one first.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := service.SendLarkCard(ctx, target, larknotify.TestCard(purposeKey, service.LarkTranslator)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sent": true})
}
