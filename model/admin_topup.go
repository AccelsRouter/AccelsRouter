// Fork-only: admin top-up order listing joined with the user's
// username, with keyword search over both username and trade number.
// Upstream's GetAllTopUps/SearchAllTopUps return raw TopUp rows
// (user_id only) and only search trade_no — this adds the username
// join the aurora admin page needs.
package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TopUpOrderWithUser is a TopUp row enriched with the owner's username.
type TopUpOrderWithUser struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id"`
	Username        string  `json:"username"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no"`
	PaymentMethod   string  `json:"payment_method"`
	PaymentProvider string  `json:"payment_provider"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
	Remark          string  `json:"remark,omitempty"`
}

// ListTopUpOrdersWithUsername returns paginated top-up orders joined
// with users.username. keyword (optional) matches trade_no OR username
// (case-insensitive LIKE). Newest first.
func ListTopUpOrdersWithUsername(
	keyword string,
	page *common.PageInfo,
) ([]*TopUpOrderWithUser, int64, error) {
	// Build the WHERE clause once, apply to both count and page query.
	applyWhere := func(tx *gorm.DB) *gorm.DB {
		t := tx.Table("top_ups").
			Joins("LEFT JOIN users ON users.id = top_ups.user_id")
		if keyword != "" {
			like := "%" + keyword + "%"
			t = t.Where(
				"top_ups.trade_no LIKE ? OR users.username LIKE ?",
				like, like,
			)
		}
		return t
	}

	var total int64
	if err := applyWhere(DB).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []*TopUpOrderWithUser
	err := applyWhere(DB).
		Select(
			"top_ups.id, top_ups.user_id, users.username, " +
				"top_ups.amount, top_ups.money, top_ups.trade_no, " +
				"top_ups.payment_method, top_ups.payment_provider, " +
				"top_ups.create_time, top_ups.complete_time, top_ups.status, " +
				"top_ups.remark",
		).
		Order("top_ups.id DESC").
		Limit(page.GetPageSize()).
		Offset(page.GetStartIdx()).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
