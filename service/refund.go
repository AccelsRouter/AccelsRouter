// Refund business logic. Lives in the fork; upstream syncs never
// touch this file. The only upstream contact points are a handful of
// route registrations and one token-controller check.
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The Lark bot is shared with the enterprise inquiry flow and is set on the
// admin settings page (see LarkEnterpriseTarget).
const larkRefundTimeout = 8 * time.Second

// minRefundUSD guards against accidental tiny submissions; users can
// always re-submit with a larger amount.
const minRefundUSD = 1.0

// maxReasonLen / maxDestinationLen / maxContactLen — defensive bounds.
const (
	maxReasonLen      = 4000
	maxDestinationLen = 2000
	maxContactLen     = 128
)

// SubmitRefundRequestParams is the input for SubmitRefundRequest.
type SubmitRefundRequestParams struct {
	UserId            int
	AmountUSD         float64
	Method            string // "bank" | "crypto"
	RefundDestination string
	Reason            string
	ContactInfo       string
}

// SubmitRefundRequest validates input + enforces invariants (0 active
// tokens, no other pending request) + inserts the row + best-effort
// fires a Lark notification.
func SubmitRefundRequest(c *gin.Context, p SubmitRefundRequestParams) (*model.RefundRequest, error) {
	if p.UserId <= 0 {
		return nil, errors.New("invalid user")
	}

	// Field validation
	p.Method = strings.ToLower(strings.TrimSpace(p.Method))
	switch p.Method {
	case model.RefundMethodBank, model.RefundMethodCrypto:
		// ok
	default:
		return nil, fmt.Errorf("refund method must be 'bank' or 'crypto', got %q", p.Method)
	}

	p.Reason = strings.TrimSpace(p.Reason)
	if len(p.Reason) < 5 {
		return nil, errors.New("refund reason is too short (>= 5 chars)")
	}
	if len(p.Reason) > maxReasonLen {
		return nil, errors.New("refund reason is too long")
	}

	p.RefundDestination = strings.TrimSpace(p.RefundDestination)
	if len(p.RefundDestination) < 4 {
		return nil, errors.New("refund destination is too short")
	}
	if len(p.RefundDestination) > maxDestinationLen {
		return nil, errors.New("refund destination is too long")
	}

	p.ContactInfo = strings.TrimSpace(p.ContactInfo)
	if len(p.ContactInfo) > maxContactLen {
		return nil, errors.New("contact info is too long")
	}

	if math.IsNaN(p.AmountUSD) || math.IsInf(p.AmountUSD, 0) || p.AmountUSD < minRefundUSD {
		return nil, fmt.Errorf("refund amount must be >= $%.2f", minRefundUSD)
	}

	// Invariant 1: no other open refund request.
	if active, err := model.GetActiveRefundRequest(p.UserId); err != nil {
		return nil, fmt.Errorf("failed to check existing refund requests: %w", err)
	} else if active != nil {
		return nil, fmt.Errorf("you already have an open refund request (#%d) in status %q", active.Id, active.Status)
	}

	// Invariant 2: user must have 0 enabled tokens. Front-end pre-checks
	// this and instructs the user to disable their tokens first; server
	// re-checks to defend against direct API calls.
	if n, err := model.CountUserActiveTokens(p.UserId); err != nil {
		return nil, fmt.Errorf("failed to count active tokens: %w", err)
	} else if n > 0 {
		return nil, fmt.Errorf("you have %d active API token(s); disable all of them before requesting a refund", n)
	}

	// Invariant 3: amount must fit within current balance (in USD).
	user, err := model.GetUserById(p.UserId, false)
	if err != nil {
		return nil, fmt.Errorf("failed to load user: %w", err)
	}
	balanceUSD := float64(user.Quota) / common.QuotaPerUnit
	if p.AmountUSD > balanceUSD+1e-9 {
		return nil, fmt.Errorf("refund amount $%.2f exceeds current balance $%.2f", p.AmountUSD, balanceUSD)
	}

	r := &model.RefundRequest{
		UserId:            user.Id,
		Username:          user.Username,
		Email:             user.Email,
		AmountUSD:         p.AmountUSD,
		BalanceSnapshot:   user.Quota,
		Method:            p.Method,
		RefundDestination: p.RefundDestination,
		Reason:            p.Reason,
		ContactInfo:       p.ContactInfo,
		Status:            model.RefundStatusPending,
	}
	if err := r.Insert(); err != nil {
		return nil, fmt.Errorf("failed to persist refund request: %w", err)
	}

	// Best-effort Lark notification — do not fail the request if it errors.
	go notifyLarkRefundSubmitted(c, r)

	return r, nil
}

// CancelRefundRequest — user-initiated cancel.
func CancelRefundRequest(userId, refundId int) error {
	r, err := model.GetRefundRequestByID(refundId)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.New("refund request not found")
	}
	if r.UserId != userId {
		return errors.New("refund request does not belong to this user")
	}
	return r.Cancel()
}

// ApproveRefundRequest — admin marks pending → approved. Does NOT
// modify the user's quota (that happens at MarkRefunded).
func ApproveRefundRequest(adminId, refundId int, note string) error {
	r, err := model.GetRefundRequestByID(refundId)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.New("refund request not found")
	}
	return r.Approve(adminId, note)
}

// RejectRefundRequest — admin marks pending → rejected.
func RejectRefundRequest(adminId, refundId int, note string) error {
	r, err := model.GetRefundRequestByID(refundId)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.New("refund request not found")
	}
	if strings.TrimSpace(note) == "" {
		return errors.New("rejection requires a note explaining the reason")
	}
	return r.Reject(adminId, note)
}

// MarkRefundedParams — input for MarkRefunded.
type MarkRefundedParams struct {
	AdminId  int
	RefundId int
	Note     string
}

// MarkRefunded is called by an admin AFTER the actual refund has
// been paid out off-platform. It atomically:
//
//	(a) verifies the request is in 'approved' state,
//	(b) verifies the user's current balance >= refund amount,
//	(c) deducts the corresponding quota,
//	(d) writes a Log entry of type LogTypeRefund,
//	(e) flips the request to 'refunded'.
//
// If any step fails, the whole transaction rolls back and admin can
// retry / contact user.
func MarkRefunded(p MarkRefundedParams) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var r model.RefundRequest
		if err := tx.Where("id = ?", p.RefundId).First(&r).Error; err != nil {
			return fmt.Errorf("load refund request: %w", err)
		}
		if r.Status != model.RefundStatusApproved {
			return fmt.Errorf("only approved requests can be marked refunded (current: %s)", r.Status)
		}

		var user model.User
		if err := tx.Where("id = ?", r.UserId).First(&user).Error; err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		// Convert USD to quota units. Round to avoid floating drift.
		quotaToDeduct := int(math.Round(r.AmountUSD * common.QuotaPerUnit))
		if quotaToDeduct <= 0 {
			return errors.New("computed quota deduction is non-positive; refund amount is invalid")
		}
		if user.Quota < quotaToDeduct {
			return fmt.Errorf("user balance %.4f USD is insufficient for refund of %.4f USD; please contact the user",
				float64(user.Quota)/common.QuotaPerUnit, r.AmountUSD)
		}

		// (c) deduct quota atomically with row-level UPDATE
		if err := tx.Model(&model.User{}).
			Where("id = ? AND quota >= ?", user.Id, quotaToDeduct).
			Update("quota", gorm.Expr("quota - ?", quotaToDeduct)).Error; err != nil {
			return fmt.Errorf("deduct user quota: %w", err)
		}

		// (d) audit log — use the dedicated refund log type.
		logContent := fmt.Sprintf("退款扣除 $%.2f (申请 #%d)", r.AmountUSD, r.Id)
		if err := tx.Create(&model.Log{
			UserId:    user.Id,
			Username:  user.Username,
			CreatedAt: common.GetTimestamp(),
			Type:      model.LogTypeRefund,
			Quota:     -quotaToDeduct,
			Content:   logContent,
		}).Error; err != nil {
			return fmt.Errorf("write refund log: %w", err)
		}

		// (e) flip status
		now := time.Now()
		if err := tx.Model(&r).Updates(map[string]any{
			"status":       model.RefundStatusRefunded,
			"admin_note":   p.Note,
			"processed_by": p.AdminId,
			"processed_at": &now,
		}).Error; err != nil {
			return fmt.Errorf("update refund request status: %w", err)
		}

		// Invalidate Redis user-quota cache so the new balance is visible
		// immediately. The helper handles the case where caching is off.
		model.InvalidateUserCache(user.Id)
		return nil
	})
}

// DisableAllEnabledTokens — convenience helper for the "one-click
// disable all tokens" button in the refund blocker dialog. Sets
// every Status=Enabled token to Status=Disabled.
func DisableAllEnabledTokens(userId int) (int64, error) {
	res := model.DB.Model(&model.Token{}).
		Where("user_id = ? AND status = ?", userId, common.TokenStatusEnabled).
		Update("status", common.TokenStatusDisabled)
	return res.RowsAffected, res.Error
}

// ----- Lark notification helpers ---------------------------------

func notifyLarkRefundSubmitted(c *gin.Context, r *model.RefundRequest) {
	target := LarkEnterpriseTarget()
	if !target.Configured() {
		return // silently no-op when not configured
	}

	methodLabel := r.Method
	if methodLabel == model.RefundMethodBank {
		methodLabel = "Bank transfer"
	} else if methodLabel == model.RefundMethodCrypto {
		methodLabel = "Crypto (USDC/USDT)"
	}

	balanceUSD := float64(r.BalanceSnapshot) / common.QuotaPerUnit
	fields := strings.Join([]string{
		fmt.Sprintf("**User**\n%s (id=%d) · %s", r.Username, r.UserId, r.Email),
		fmt.Sprintf("**Amount**\n$%.2f USD", r.AmountUSD),
		fmt.Sprintf("**Balance at submit**\n$%.2f USD", balanceUSD),
		fmt.Sprintf("**Method**\n%s", methodLabel),
		fmt.Sprintf("**Refund destination**\n%s", r.RefundDestination),
		fmt.Sprintf("**Contact**\n%s", r.ContactInfo),
		fmt.Sprintf("**Reason**\n%s", r.Reason),
	}, "\n\n")

	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "orange",
			"title": map[string]any{
				"tag":     "plain_text",
				"content": fmt.Sprintf("💸 Refund Request #%d submitted", r.Id),
			},
		},
		"elements": []any{
			map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "lark_md",
					"content": fields,
				},
			},
			map[string]any{"tag": "hr"},
			map[string]any{
				"tag": "note",
				"elements": []any{
					map[string]any{
						"tag":     "plain_text",
						"content": fmt.Sprintf("submitted %s · ip %s", time.Now().UTC().Format(time.RFC3339), c.ClientIP()),
					},
				},
			},
		},
	}

	// The request context ends with the response, and this runs after it.
	ctx, cancel := context.WithTimeout(context.Background(), larkRefundTimeout)
	defer cancel()
	if err := SendLarkRaw(ctx, target, card); err != nil {
		logger.LogError(ctx, "refund lark notification failed: "+err.Error())
	}
}
