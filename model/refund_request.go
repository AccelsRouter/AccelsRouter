// Refund request table. Lives in the fork; upstream syncs never touch
// this file. The only upstream contact points are:
//   - 2 lines in model/main.go to register the table with AutoMigrate
//   - 4-5 lines in controller/token.go to gate create/enable on
//     HasActiveRefundRequest
//   - a few lines in router/api-router.go to register HTTP routes
package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Status values
const (
	RefundStatusPending   = "pending"
	RefundStatusApproved  = "approved"
	RefundStatusRejected  = "rejected"
	RefundStatusRefunded  = "refunded"
	RefundStatusCancelled = "cancelled"
)

// Method values
const (
	RefundMethodBank   = "bank"
	RefundMethodCrypto = "crypto"
)

// RefundRequest is a user-initiated request to refund a portion of
// their wallet balance. Refunds are processed off-platform by an
// operator; this table tracks the request lifecycle and pairs the
// admin action with an on-platform Quota deduction at the
// "refunded" stage.
type RefundRequest struct {
	Id int `json:"id" gorm:"primarykey"`

	// Snapshots taken at submission time
	UserId          int     `json:"user_id" gorm:"index;not null"`
	Username        string  `json:"username" gorm:"type:varchar(64)"`
	Email           string  `json:"email" gorm:"type:varchar(128)"`
	AmountUSD       float64 `json:"amount_usd" gorm:"not null"`
	BalanceSnapshot int     `json:"balance_snapshot"` // user.Quota at submit time

	// User-supplied
	Method            string `json:"method" gorm:"type:varchar(16);not null"` // bank | crypto
	RefundDestination string `json:"refund_destination" gorm:"type:text"`     // bank: 开户行+卡号+姓名 / crypto: 链+地址
	Reason            string `json:"reason" gorm:"type:text;not null"`
	ContactInfo       string `json:"contact_info" gorm:"type:varchar(128)"`

	// Lifecycle
	Status      string     `json:"status" gorm:"type:varchar(16);index;default:'pending'"`
	AdminNote   string     `json:"admin_note" gorm:"type:text"`
	ProcessedBy int        `json:"processed_by"`
	ProcessedAt *time.Time `json:"processed_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (r *RefundRequest) Insert() error {
	return DB.Create(r).Error
}

// GetRefundRequestByID — admin or self use; caller must enforce
// ownership for non-admin callers.
func GetRefundRequestByID(id int) (*RefundRequest, error) {
	var r RefundRequest
	err := DB.Where("id = ?", id).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

// ListRefundRequestsByUser returns the user's own refund history,
// newest first.
func ListRefundRequestsByUser(userId int, limit int) ([]*RefundRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var out []*RefundRequest
	err := DB.Where("user_id = ?", userId).
		Order("id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// ListAllRefundRequests — admin scoped list with optional status filter.
func ListAllRefundRequests(status string, limit, offset int) ([]*RefundRequest, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tx := DB.Model(&RefundRequest{})
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*RefundRequest
	err := tx.Order("id DESC").Limit(limit).Offset(offset).Find(&out).Error
	return out, total, err
}

// HasActiveRefundRequest returns true when the user has a refund
// request in 'pending' or 'approved' state. Used to gate token
// create / enable.
func HasActiveRefundRequest(userId int) (bool, error) {
	var count int64
	err := DB.Model(&RefundRequest{}).
		Where("user_id = ? AND status IN (?, ?)",
			userId, RefundStatusPending, RefundStatusApproved).
		Count(&count).Error
	return count > 0, err
}

// GetActiveRefundRequest returns the user's currently-open refund
// request (pending or approved), or nil if none.
func GetActiveRefundRequest(userId int) (*RefundRequest, error) {
	var r RefundRequest
	err := DB.Where("user_id = ? AND status IN (?, ?)",
		userId, RefundStatusPending, RefundStatusApproved).
		Order("id DESC").First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

// CountUserActiveTokens counts a user's Status=Enabled tokens. Used
// by the refund precheck — submission is blocked when this is > 0.
func CountUserActiveTokens(userId int) (int64, error) {
	var n int64
	err := DB.Model(&Token{}).
		Where("user_id = ? AND status = ?", userId, common.TokenStatusEnabled).
		Count(&n).Error
	return n, err
}

// CancelRefundRequest — user-initiated cancel; only valid on pending.
func (r *RefundRequest) Cancel() error {
	if r.Status != RefundStatusPending {
		return fmt.Errorf("only pending refund requests can be cancelled (current status: %s)", r.Status)
	}
	now := time.Now()
	return r.leavePending(map[string]any{
		"status":       RefundStatusCancelled,
		"processed_at": &now,
	})
}

// leavePending moves the request out of pending with the given fields, and
// fails when it is no longer pending in the database: a user cancelling and an
// admin approving at the same moment must not overwrite one another.
func (r *RefundRequest) leavePending(fields map[string]any) error {
	result := DB.Model(&RefundRequest{}).Where("id = ? AND status = ?", r.Id, RefundStatusPending).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("the refund request is no longer pending")
	}
	return DB.First(r, r.Id).Error
}

// Approve — admin transitions pending → approved.
func (r *RefundRequest) Approve(adminId int, note string) error {
	if r.Status != RefundStatusPending {
		return fmt.Errorf("only pending refund requests can be approved (current status: %s)", r.Status)
	}
	now := time.Now()
	return r.leavePending(map[string]any{
		"status":       RefundStatusApproved,
		"admin_note":   note,
		"processed_by": adminId,
		"processed_at": &now,
	})
}

// Reject — admin transitions pending → rejected. Cannot reject an
// already-approved request; admin must contact user directly for
// post-approval issues.
func (r *RefundRequest) Reject(adminId int, note string) error {
	if r.Status != RefundStatusPending {
		return fmt.Errorf("only pending refund requests can be rejected (current status: %s)", r.Status)
	}
	now := time.Now()
	return r.leavePending(map[string]any{
		"status":       RefundStatusRejected,
		"admin_note":   note,
		"processed_by": adminId,
		"processed_at": &now,
	})
}

// InvalidateUserCache deletes the cached user snapshot so the next read
// repopulates from the database. Fork wrapper over the (rc.25 unexported)
// invalidateUserCache: refund settlement performs a raw in-transaction quota
// deduction that bypasses the atomic cache-delta path, so the cached balance
// must be busted explicitly afterwards.
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}
