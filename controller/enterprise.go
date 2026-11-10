// Enterprise inquiry proxy to Lark webhook.
//
// This file is intentionally isolated so upstream merges never touch it.
// The only upstream integration point is one route registration in
// router/api-router.go.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const larkRequestTimeout = 8 * time.Second

type enterpriseInquiryRequest struct {
	Name           string   `json:"name"`
	Company        string   `json:"company"`
	WorkEmail      string   `json:"work_email"`
	Country        string   `json:"country"`
	MonthlyVolume  string   `json:"monthly_volume"`
	ModelsInterest []string `json:"models_interest"`
	UseCase        string   `json:"use_case"`
	Source         string   `json:"source"`
}

// PostEnterpriseInquiry receives the form payload from aurora's /enterprise
// page and forwards a Lark interactive card to the configured webhook.
//
// The bot (webhook address and optional signing secret) is read from the admin
// settings page at request time, so it can be changed without a restart. The old
// LARK_ENTERPRISE_WEBHOOK_URL variable is used until the page has a value.
func PostEnterpriseInquiry(c *gin.Context) {
	target := service.LarkEnterpriseTarget()
	if !target.Configured() {
		logger.LogError(c.Request.Context(), "enterprise inquiry rejected: the Lark webhook is not configured (admin settings, or "+service.LarkEnterpriseWebhookEnv+")")
		common.ApiError(c, fmt.Errorf("enterprise contact is not configured"))
		return
	}

	var req enterpriseInquiryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, fmt.Errorf("invalid payload: %w", err))
		return
	}

	// Minimal server-side validation. Frontend already validates with zod,
	// but never trust the client.
	req.Name = strings.TrimSpace(req.Name)
	req.Company = strings.TrimSpace(req.Company)
	req.WorkEmail = strings.TrimSpace(req.WorkEmail)
	req.Country = strings.TrimSpace(req.Country)
	req.UseCase = strings.TrimSpace(req.UseCase)
	if req.Name == "" || req.Company == "" || req.WorkEmail == "" ||
		req.Country == "" || req.UseCase == "" {
		common.ApiError(c, fmt.Errorf("missing required fields"))
		return
	}
	if !strings.Contains(req.WorkEmail, "@") {
		common.ApiError(c, fmt.Errorf("invalid work email"))
		return
	}
	if len(req.UseCase) > 4000 {
		common.ApiError(c, fmt.Errorf("use case too long"))
		return
	}

	card := buildLarkCard(&req, c.ClientIP(), c.Request.UserAgent())
	ctx, cancel := context.WithTimeout(c.Request.Context(), larkRequestTimeout)
	defer cancel()
	if err := service.SendLarkRaw(ctx, target, card); err != nil {
		logger.LogError(c.Request.Context(), "enterprise inquiry webhook failed: "+err.Error())
		common.ApiError(c, fmt.Errorf("failed to reach contact service, please try again later"))
		return
	}

	common.ApiSuccess(c, gin.H{"submitted": true})
}

// buildLarkCard returns a Lark "interactive" card body. Lark webhook expects a
// "card" object at the top level when msg_type=interactive.
func buildLarkCard(req *enterpriseInquiryRequest, clientIP, userAgent string) map[string]any {
	models := strings.Join(req.ModelsInterest, ", ")
	if models == "" {
		models = "-"
	}
	volume := req.MonthlyVolume
	if strings.TrimSpace(volume) == "" {
		volume = "-"
	}
	source := req.Source
	if strings.TrimSpace(source) == "" {
		source = "-"
	}

	fieldsMarkdown := strings.Join([]string{
		fmt.Sprintf("**Name**\n%s", escapeLark(req.Name)),
		fmt.Sprintf("**Company**\n%s", escapeLark(req.Company)),
		fmt.Sprintf("**Work Email**\n%s", escapeLark(req.WorkEmail)),
		fmt.Sprintf("**Country / Region**\n%s", escapeLark(req.Country)),
		fmt.Sprintf("**Expected Monthly Volume (USD)**\n%s", escapeLark(volume)),
		fmt.Sprintf("**Models of Interest**\n%s", escapeLark(models)),
		fmt.Sprintf("**Source**\n%s", escapeLark(source)),
		fmt.Sprintf("**Use Case**\n%s", escapeLark(req.UseCase)),
	}, "\n\n")

	footer := fmt.Sprintf("Submitted %s · IP %s · UA %s",
		time.Now().UTC().Format(time.RFC3339), clientIP, truncate(userAgent, 80))

	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": "blue",
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "🏢 New Enterprise Inquiry",
			},
		},
		"elements": []any{
			map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "lark_md",
					"content": fieldsMarkdown,
				},
			},
			map[string]any{"tag": "hr"},
			map[string]any{
				"tag": "note",
				"elements": []any{
					map[string]any{
						"tag":     "plain_text",
						"content": footer,
					},
				},
			},
		},
	}
}

// escapeLark replaces characters that would break Lark markdown rendering.
func escapeLark(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	// Lark lark_md treats bare newlines literally, no escape needed.
	// Just avoid raw control chars.
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
