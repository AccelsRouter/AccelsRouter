package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrgTopUp says which organization a payment order was placed for. The order
// itself stays a row of top_ups, written and completed by the payment code
// unchanged; this names the wallet it was meant for, so the organization, its
// reseller and finance can list an organization's payments.
type OrgTopUp struct {
	Id          int    `json:"id" gorm:"primarykey"`
	TradeNo     string `json:"trade_no" gorm:"type:varchar(255);uniqueIndex;not null"`
	OrgId       int    `json:"org_id" gorm:"index;not null"`
	UserId      int    `json:"user_id" gorm:"not null"`
	CreatedTime int64  `json:"created_time"`
}

// directFundedOrgOfUser returns the organization the user belongs to when that
// organization pays for itself (FundingMode "direct"), with the user's role in
// it; 0 when the user has no organization or it is funded another way.
func directFundedOrgOfUser(tx *gorm.DB, userId int) (orgId int, role string, err error) {
	var acc OrgAccount
	err = tx.Where("user_id = ?", userId).First(&acc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	var org Organization
	err = tx.Select("id", "funding_mode").Where("id = ?", acc.OrgId).First(&org).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	if org.FundingMode != OrgFundingDirect {
		return 0, "", nil
	}
	return org.Id, acc.Role, nil
}

// DirectFundedOrgOfUser is directFundedOrgOfUser outside a transaction.
func DirectFundedOrgOfUser(userId int) (orgId int, role string, err error) {
	return directFundedOrgOfUser(DB, userId)
}

// creditOrgTopUp adds a completed payment to the organization's wallet in the
// payment's own transaction, in place of the payer's personal balance. The
// wallet ceiling is part of the UPDATE, as it is for a personal top-up, and
// userUpdates are the payer's own fields the payment code wanted saved.
func creditOrgTopUp(tx *gorm.DB, orgId, userId, quota int, userUpdates map[string]any) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(quota)
	if err != nil {
		return err
	}
	credited := tx.Model(&Organization{}).
		Where("id = ? AND wallet_quota <= ?", orgId, maxCurrentQuota).
		Updates(map[string]any{
			"wallet_quota": gorm.Expr("wallet_quota + ?", quota),
			"updated_time": common.GetTimestamp(),
		})
	if credited.Error != nil {
		return credited.Error
	}
	if credited.RowsAffected != 1 {
		return ErrTopUpQuotaLimitExceeded
	}
	if len(userUpdates) > 0 {
		if err := tx.Model(&User{}).Where("id = ?", userId).Updates(userUpdates).Error; err != nil {
			return err
		}
	}
	tradeNo, err := orgOrderBeingCredited(tx, orgId, userId)
	if err != nil {
		return err
	}
	return insertLedger(tx, 0, orgId, quota, userId, LedgerTypeTopUp, tradeNo, "")
}

// orgOrderBeingCredited names the payment order a credit to the organization's
// wallet belongs to, so the ledger row can carry its number. The payment code
// marks an order complete before it credits, inside the same transaction, so
// the order is the payer's completed one, placed for this organization, that
// has no ledger row yet. Empty when there is none: a payment whose order was
// never noted as the organization's still reaches the wallet.
func orgOrderBeingCredited(tx *gorm.DB, orgId, userId int) (string, error) {
	placedForOrg := tx.Model(&OrgTopUp{}).Select("trade_no").Where("org_id = ?", orgId)
	alreadyCredited := tx.Model(&CreditLedger{}).Select("trade_no").
		Where("to_org_id = ? AND type = ? AND trade_no <> ?", orgId, LedgerTypeTopUp, "")
	var tradeNos []string
	err := tx.Model(&TopUp{}).
		Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
		Where("trade_no IN (?)", placedForOrg).
		Where("trade_no NOT IN (?)", alreadyCredited).
		Order("id DESC").Limit(1).
		Pluck("trade_no", &tradeNos).Error
	if err != nil || len(tradeNos) == 0 {
		return "", err
	}
	return tradeNos[0], nil
}

// reverseOrgTopUp takes a reversed payment back out of the organization's
// wallet when that is where it went, and reports whether it did. The wallet
// gives back what it still holds of the amount and never goes below zero; the
// part already spent is returned as shortfall for the caller to account for.
func reverseOrgTopUp(tx *gorm.DB, tradeNo string, quota int) (reversed bool, shortfall int, err error) {
	var credit CreditLedger
	if err := tx.Where("trade_no = ? AND type = ?", tradeNo, LedgerTypeTopUp).Limit(1).Find(&credit).Error; err != nil {
		return false, 0, err
	}
	if credit.ToOrgId == 0 {
		return false, 0, nil
	}
	var org Organization
	if err := lockForUpdate(tx).Where("id = ?", credit.ToOrgId).First(&org).Error; err != nil {
		return false, 0, err
	}
	recovered := min(quota, max(org.WalletQuota, 0))
	if recovered > 0 {
		taken := tx.Model(&Organization{}).
			Where("id = ? AND wallet_quota >= ?", org.Id, recovered).
			Updates(map[string]any{
				"wallet_quota": gorm.Expr("wallet_quota - ?", recovered),
				"updated_time": common.GetTimestamp(),
			})
		if taken.Error != nil {
			return false, 0, taken.Error
		}
		if taken.RowsAffected != 1 {
			return false, 0, errors.New("the organization wallet changed while the payment was being reversed; try again")
		}
		if err := insertLedger(tx, org.Id, 0, recovered, credit.OperatorId, LedgerTypeChargeback, tradeNo, ""); err != nil {
			return false, 0, err
		}
	}
	return true, quota - recovered, nil
}

// RecordOrgTopUpOrders notes every payment order the user placed since the
// given time as the organization's. It runs right after a payment request, so
// the orders are the ones that request created; an order already noted is
// left as it is.
func RecordOrgTopUpOrders(orgId, userId int, since int64) error {
	var tradeNos []string
	if err := DB.Model(&TopUp{}).
		Where("user_id = ? AND create_time >= ?", userId, since).
		Pluck("trade_no", &tradeNos).Error; err != nil {
		return err
	}
	now := common.GetTimestamp()
	for _, tradeNo := range tradeNos {
		if tradeNo == "" {
			continue
		}
		row := &OrgTopUp{TradeNo: tradeNo, OrgId: orgId, UserId: userId, CreatedTime: now}
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}

// ListOrgTopUps returns the organization's payment orders, newest first.
func ListOrgTopUps(orgId, startIdx, num int) (orders []*TopUp, total int64, err error) {
	placedForOrg := DB.Model(&OrgTopUp{}).Select("trade_no").Where("org_id = ?", orgId)
	query := DB.Model(&TopUp{}).Where("trade_no IN (?)", placedForOrg)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	orders = []*TopUp{}
	err = query.Order("id DESC").Limit(num).Offset(startIdx).Find(&orders).Error
	return orders, total, err
}
