package model

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Settlement of what the platform owes a reseller for its customers that pay
// the platform themselves. The commission accrues call by call in the usage
// rollup (org_usage_daily.commission_quota). Once a month it is fixed per
// customer in a settlement item; anything that moves afterwards is an
// adjustment; a payout pays every due item and adjustment of a reseller.

const (
	SettlementAdjustChargeback = "chargeback"
	SettlementAdjustLateUsage  = "late_usage"
	SettlementAdjustManual     = "manual"

	PayoutMethodBank        = "bank"
	PayoutMethodCrypto      = "crypto"
	PayoutMethodWalletDebit = "wallet_debit"
)

// LedgerTypeClawback takes a negative settlement balance out of the
// reseller's wallet (org → platform).
const LedgerTypeClawback = "clawback"

// ResellerSettlementItem is one customer's commission for one month, fixed
// when the month is closed. It is paid by the payout named in PayoutId, and
// may be paid from ReleaseAt on (a negative one nets at once).
type ResellerSettlementItem struct {
	Id              int    `json:"id" gorm:"primarykey"`
	Period          string `json:"period" gorm:"type:varchar(7);uniqueIndex:idx_rsi_key,priority:1;not null"`
	ResellerOrgId   int    `json:"reseller_org_id" gorm:"uniqueIndex:idx_rsi_key,priority:2;index;not null"`
	CustomerOrgId   int    `json:"customer_org_id" gorm:"uniqueIndex:idx_rsi_key,priority:3;not null"`
	StandardQuota   int64  `json:"standard_quota"`
	ChargedQuota    int64  `json:"charged_quota"`
	CostQuota       int64  `json:"cost_quota"`
	CommissionQuota int64  `json:"commission_quota"`
	Requests        int64  `json:"requests"`
	ReleaseAt       int64  `json:"release_at"`
	PayoutId        int    `json:"payout_id" gorm:"index"`
	CreatedTime     int64  `json:"created_time"`
	// Computed for display.
	ResellerName string `json:"reseller_name,omitempty" gorm:"-"`
	CustomerName string `json:"customer_name,omitempty" gorm:"-"`
	Status       string `json:"status" gorm:"-"` // held | payable | paid
}

// ResellerSettlementAdjustment moves what a reseller is owed outside the
// monthly items: a reversed payment whose money was already spent, usage that
// changed after its month was closed, or an administrator's correction.
// (Type, Ref) is unique, so the same event is never counted twice.
type ResellerSettlementAdjustment struct {
	Id            int    `json:"id" gorm:"primarykey"`
	ResellerOrgId int    `json:"reseller_org_id" gorm:"index;not null"`
	CustomerOrgId int    `json:"customer_org_id"`
	Type          string `json:"type" gorm:"type:varchar(16);uniqueIndex:idx_rsa_ref,priority:1;not null"`
	Ref           string `json:"ref" gorm:"type:varchar(96);uniqueIndex:idx_rsa_ref,priority:2;not null"`
	Period        string `json:"period" gorm:"type:varchar(7)"`
	Quota         int64  `json:"quota"`
	Remark        string `json:"remark" gorm:"type:varchar(255)"`
	OperatorId    int    `json:"operator_id"`
	PayoutId      int    `json:"payout_id" gorm:"index"`
	CreatedTime   int64  `json:"created_time"`
	ResellerName  string `json:"reseller_name,omitempty" gorm:"-"`
	CustomerName  string `json:"customer_name,omitempty" gorm:"-"`
}

// ResellerPayout records that the platform paid a reseller: Quota is what it
// settled, Money what was actually sent. A wallet debit carries a negative
// Quota: a balance the reseller owed, taken from its wallet.
type ResellerPayout struct {
	Id            int     `json:"id" gorm:"primarykey"`
	ResellerOrgId int     `json:"reseller_org_id" gorm:"index;not null"`
	Quota         int64   `json:"quota"`
	Money         float64 `json:"money"`
	Currency      string  `json:"currency" gorm:"type:varchar(8)"`
	Method        string  `json:"method" gorm:"type:varchar(16)"`
	Reference     string  `json:"reference" gorm:"type:varchar(128)"`
	InvoiceNo     string  `json:"invoice_no" gorm:"type:varchar(64)"`
	Remark        string  `json:"remark" gorm:"type:varchar(255)"`
	OperatorId    int     `json:"operator_id"`
	PaidTime      int64   `json:"paid_time"`
	CreatedTime   int64   `json:"created_time"`
	ResellerName  string  `json:"reseller_name,omitempty" gorm:"-"`
}

// Months are cut at UTC+8, the calendar the business keeps its books in.
var settlementZone = time.FixedZone("UTC+8", 8*3600)

// SettlementPeriodOf names the month a moment falls in, as "2006-01".
func SettlementPeriodOf(ts int64) string {
	return time.Unix(ts, 0).In(settlementZone).Format("2006-01")
}

// SettlementPeriodBounds returns the period's first second and the first
// second after it.
func SettlementPeriodBounds(period string) (start, end int64, err error) {
	first, err := time.ParseInLocation("2006-01", period, settlementZone)
	if err != nil {
		return 0, 0, common.NewMessage("The month must look like 2026-11")
	}
	return first.Unix(), first.AddDate(0, 1, 0).Unix(), nil
}

// commissionHold is how long a customer's commission is kept back after its
// month when the customer paid by a method that can be charged back within
// that time, and which methods those are. RESELLER_COMMISSION_HOLD_DAYS
// (default 120) and RESELLER_COMMISSION_HOLD_PROVIDERS (comma separated,
// default the card processors) override them; an empty list holds nothing.
func commissionHold() (seconds int64, providers []string) {
	days := common.GetEnvOrDefault("RESELLER_COMMISSION_HOLD_DAYS", 120)
	list, set := os.LookupEnv("RESELLER_COMMISSION_HOLD_PROVIDERS")
	if !set {
		list = strings.Join([]string{PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake}, ",")
	}
	for name := range strings.SplitSeq(list, ",") {
		if name = strings.TrimSpace(name); name != "" {
			providers = append(providers, name)
		}
	}
	return int64(max(days, 0)) * 86400, providers
}

// commissionReleaseAt is when a customer's commission for a month ending at
// periodEnd may be paid: at once, unless the customer completed a payment by a
// chargeback-prone method within the hold before the month's end.
func commissionReleaseAt(customerOrgId int, periodEnd int64) (int64, error) {
	hold, providers := commissionHold()
	if hold == 0 || len(providers) == 0 {
		return periodEnd, nil
	}
	var risky int64
	err := DB.Model(&TopUp{}).
		Where("trade_no IN (?)", DB.Model(&OrgTopUp{}).Select("trade_no").Where("org_id = ?", customerOrgId)).
		Where("status = ? AND payment_provider IN ?", common.TopUpStatusSuccess, providers).
		Where("complete_time > ? AND complete_time <= ?", periodEnd-hold, periodEnd).
		Count(&risky).Error
	if err != nil {
		return 0, err
	}
	if risky > 0 {
		return periodEnd + hold, nil
	}
	return periodEnd, nil
}

// CommissionSums is the usage of one self-funded customer under one reseller.
type CommissionSums struct {
	ResellerOrgId int
	OrgId         int
	Standard      int64
	Charged       int64
	Cost          int64
	Commission    int64
	Requests      int64
}

// commissionSums totals the usage rollup of self-funded customers over
// [start, end), per reseller and customer. A zero bound is open.
func commissionSums(start, end int64, resellerOrgId int) ([]CommissionSums, error) {
	query := DB.Model(&OrgUsageDaily{}).
		Select("reseller_org_id, org_id, COALESCE(SUM(standard_quota), 0) AS standard, COALESCE(SUM(charged_quota), 0) AS charged, COALESCE(SUM(cost_quota), 0) AS cost, COALESCE(SUM(commission_quota), 0) AS commission, COALESCE(SUM(requests), 0) AS requests").
		Where("reseller_org_id > 0 AND org_id <> reseller_org_id").
		Where("org_id IN (?)", DB.Model(&Organization{}).Select("id").Where("funding_mode = ?", OrgFundingDirect)).
		Group("reseller_org_id, org_id")
	if start > 0 {
		query = query.Where("day_bucket >= ?", start)
	}
	if end > 0 {
		query = query.Where("day_bucket < ?", end)
	}
	if resellerOrgId > 0 {
		query = query.Where("reseller_org_id = ?", resellerOrgId)
	}
	var sums []CommissionSums
	err := query.Scan(&sums).Error
	return sums, err
}

// CloseSettlementPeriod fixes every self-funded customer's commission for a
// month that has ended, as one item per reseller and customer. Closing again
// is safe: an item is never rewritten, and usage that moved since (a task
// settled or refunded late) becomes a late-usage adjustment for the
// difference. Returns how many items and adjustments it wrote.
func CloseSettlementPeriod(period string, now int64) (items, adjustments int, err error) {
	start, end, err := SettlementPeriodBounds(period)
	if err != nil {
		return 0, 0, err
	}
	if end > now {
		return 0, 0, common.NewMessage("This month has not ended yet, so it cannot be closed")
	}
	sums, err := commissionSums(start, end, 0)
	if err != nil {
		return 0, 0, err
	}
	for _, sum := range sums {
		var item ResellerSettlementItem
		found := DB.Where("period = ? AND reseller_org_id = ? AND customer_org_id = ?", period, sum.ResellerOrgId, sum.OrgId).
			Limit(1).Find(&item)
		if found.Error != nil {
			return items, adjustments, found.Error
		}
		if found.RowsAffected == 0 {
			releaseAt, err := commissionReleaseAt(sum.OrgId, end)
			if err != nil {
				return items, adjustments, err
			}
			created := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&ResellerSettlementItem{
				Period: period, ResellerOrgId: sum.ResellerOrgId, CustomerOrgId: sum.OrgId,
				StandardQuota: sum.Standard, ChargedQuota: sum.Charged, CostQuota: sum.Cost,
				CommissionQuota: sum.Commission, Requests: sum.Requests,
				ReleaseAt: releaseAt, CreatedTime: now,
			})
			if created.Error != nil {
				return items, adjustments, created.Error
			}
			items += int(created.RowsAffected)
			continue
		}
		var adjusted struct{ Total int64 }
		if err := DB.Model(&ResellerSettlementAdjustment{}).
			Select("COALESCE(SUM(quota), 0) AS total").
			Where("type = ? AND period = ? AND reseller_org_id = ? AND customer_org_id = ?", SettlementAdjustLateUsage, period, sum.ResellerOrgId, sum.OrgId).
			Scan(&adjusted).Error; err != nil {
			return items, adjustments, err
		}
		drift := sum.Commission - item.CommissionQuota - adjusted.Total
		if drift == 0 {
			continue
		}
		if err := DB.Create(&ResellerSettlementAdjustment{
			ResellerOrgId: sum.ResellerOrgId, CustomerOrgId: sum.OrgId,
			Type: SettlementAdjustLateUsage, Ref: fmt.Sprintf("%s:%d:%d", period, sum.OrgId, now),
			Period: period, Quota: drift, CreatedTime: now,
		}).Error; err != nil {
			return items, adjustments, err
		}
		adjustments++
	}
	return items, adjustments, nil
}

// CloseDueSettlementPeriods is the daily job: from the third day of a month
// it closes the month before, and it looks again at the three months before
// that for usage that moved after they were closed.
func CloseDueSettlementPeriods(now int64) error {
	today := time.Unix(now, 0).In(settlementZone)
	first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, settlementZone)
	newest := 1
	if today.Day() < 3 {
		newest = 2
	}
	for back := newest; back < newest+4; back++ {
		period := first.AddDate(0, -back, 0).Format("2006-01")
		if _, _, err := CloseSettlementPeriod(period, now); err != nil {
			return fmt.Errorf("close %s: %w", period, err)
		}
	}
	return nil
}

// settlementItemStatus says where an item stands at a moment.
func settlementItemStatus(item *ResellerSettlementItem, now int64) string {
	switch {
	case item.PayoutId != 0:
		return "paid"
	case item.CommissionQuota <= 0 || item.ReleaseAt <= now:
		return "payable"
	default:
		return "held"
	}
}

// ResellerSettlementSummary is where a reseller's commission stands.
type ResellerSettlementSummary struct {
	ResellerOrgId int    `json:"reseller_org_id"`
	ResellerName  string `json:"reseller_name,omitempty"`
	// Accruing is the commission of months not closed yet.
	Accruing int64 `json:"accruing"`
	// Held and Payable split the closed, unpaid items by whether they may be
	// paid now; Adjustments are the unapplied ones.
	Held        int64 `json:"held"`
	Payable     int64 `json:"payable"`
	Adjustments int64 `json:"adjustments"`
	// Due is what a payout would settle now: Payable plus Adjustments.
	Due  int64 `json:"due"`
	Paid int64 `json:"paid"`
	// AcquiringFees is the fee schedule in force, for the reseller's own view.
	AcquiringFees *AcquiringFeeSchedule `json:"acquiring_fees,omitempty"`
}

// GetResellerSettlementSummary totals a reseller's commission as of now.
func GetResellerSettlementSummary(resellerOrgId int, now int64) (*ResellerSettlementSummary, error) {
	summary := &ResellerSettlementSummary{ResellerOrgId: resellerOrgId}
	var items []ResellerSettlementItem
	if err := DB.Where("reseller_org_id = ?", resellerOrgId).Find(&items).Error; err != nil {
		return nil, err
	}
	closedUntil := int64(0)
	for i := range items {
		_, end, err := SettlementPeriodBounds(items[i].Period)
		if err == nil && end > closedUntil {
			closedUntil = end
		}
		switch settlementItemStatus(&items[i], now) {
		case "held":
			summary.Held += items[i].CommissionQuota
		case "payable":
			summary.Payable += items[i].CommissionQuota
		}
	}
	accruing, err := commissionSums(closedUntil, 0, resellerOrgId)
	if err != nil {
		return nil, err
	}
	for _, sum := range accruing {
		summary.Accruing += sum.Commission
	}
	var pending struct{ Total int64 }
	if err := DB.Model(&ResellerSettlementAdjustment{}).Select("COALESCE(SUM(quota), 0) AS total").
		Where("reseller_org_id = ? AND payout_id = 0", resellerOrgId).Scan(&pending).Error; err != nil {
		return nil, err
	}
	summary.Adjustments = pending.Total
	summary.Due = summary.Payable + summary.Adjustments
	var paid struct{ Total int64 }
	if err := DB.Model(&ResellerPayout{}).Select("COALESCE(SUM(quota), 0) AS total").
		Where("reseller_org_id = ?", resellerOrgId).Scan(&paid).Error; err != nil {
		return nil, err
	}
	summary.Paid = paid.Total
	return summary, nil
}

// ErrNothingToSettle refuses a payout when the reseller is owed nothing now.
var ErrNothingToSettle = common.NewMessage("Nothing is due to this distributor right now")

// ErrSettlementChanged refuses a payout or debit raced by another one.
var ErrSettlementChanged = common.NewMessage("The settlement changed while it was being saved; reload and try again")

// dueSettlementRows returns the reseller's unpaid items that may be paid now
// and its unapplied adjustments, with their total.
func dueSettlementRows(tx *gorm.DB, resellerOrgId int, now int64) (itemIds, adjustmentIds []int, total int64, err error) {
	var items []ResellerSettlementItem
	if err = tx.Where("reseller_org_id = ? AND payout_id = 0", resellerOrgId).Find(&items).Error; err != nil {
		return nil, nil, 0, err
	}
	for i := range items {
		if settlementItemStatus(&items[i], now) == "payable" {
			itemIds = append(itemIds, items[i].Id)
			total += items[i].CommissionQuota
		}
	}
	var adjustments []ResellerSettlementAdjustment
	if err = tx.Where("reseller_org_id = ? AND payout_id = 0", resellerOrgId).Find(&adjustments).Error; err != nil {
		return nil, nil, 0, err
	}
	for i := range adjustments {
		adjustmentIds = append(adjustmentIds, adjustments[i].Id)
		total += adjustments[i].Quota
	}
	return itemIds, adjustmentIds, total, nil
}

// markSettled ties the due rows to the payout that settles them. Each update
// only takes rows nobody settled meanwhile, and a short count undoes it all.
func markSettled(tx *gorm.DB, payoutId int, itemIds, adjustmentIds []int) error {
	if len(itemIds) > 0 {
		res := tx.Model(&ResellerSettlementItem{}).Where("id IN ? AND payout_id = 0", itemIds).Update("payout_id", payoutId)
		if res.Error != nil {
			return res.Error
		}
		if int(res.RowsAffected) != len(itemIds) {
			return ErrSettlementChanged
		}
	}
	if len(adjustmentIds) > 0 {
		res := tx.Model(&ResellerSettlementAdjustment{}).Where("id IN ? AND payout_id = 0", adjustmentIds).Update("payout_id", payoutId)
		if res.Error != nil {
			return res.Error
		}
		if int(res.RowsAffected) != len(adjustmentIds) {
			return ErrSettlementChanged
		}
	}
	return nil
}

// RegisterResellerPayout records a payment the platform made to a reseller
// and settles everything due to it: the payable items and every unapplied
// adjustment, netted. The amount settled is computed here, never taken from
// the caller; it must be positive.
func RegisterResellerPayout(payout *ResellerPayout, now int64) error {
	if payout.Method != PayoutMethodBank && payout.Method != PayoutMethodCrypto {
		return errors.New("invalid payout method")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var reseller Organization
		if err := lockForUpdate(tx).Where("id = ? AND type = ?", payout.ResellerOrgId, OrgTypeReseller).First(&reseller).Error; err != nil {
			return errors.New("distributor not found")
		}
		itemIds, adjustmentIds, total, err := dueSettlementRows(tx, payout.ResellerOrgId, now)
		if err != nil {
			return err
		}
		if total <= 0 {
			return ErrNothingToSettle
		}
		payout.Id = 0
		payout.Quota = total
		payout.CreatedTime = now
		if payout.PaidTime <= 0 {
			payout.PaidTime = now
		}
		if err := tx.Create(payout).Error; err != nil {
			return err
		}
		return markSettled(tx, payout.Id, itemIds, adjustmentIds)
	})
}

// ErrNoDebtToClawBack refuses a wallet debit when the reseller owes nothing.
var ErrNoDebtToClawBack = common.NewMessage("This distributor does not owe anything right now")

// ErrWalletCannotCoverDebt refuses a wallet debit the wallet cannot cover in
// full, so a debt is never left half settled.
var ErrWalletCannotCoverDebt = common.NewMessage("The distributor's wallet does not hold enough to cover what it owes")

// ClawbackResellerDebt settles a negative balance (reversed payments and
// negative commission exceeding what is payable) out of the reseller's
// wallet: the wallet is debited by the whole amount and the due rows are
// settled by a wallet-debit payout of that negative amount.
func ClawbackResellerDebt(resellerOrgId, operatorId int, now int64) (*ResellerPayout, error) {
	var payout *ResellerPayout
	err := DB.Transaction(func(tx *gorm.DB) error {
		var reseller Organization
		if err := lockForUpdate(tx).Where("id = ? AND type = ?", resellerOrgId, OrgTypeReseller).First(&reseller).Error; err != nil {
			return errors.New("distributor not found")
		}
		itemIds, adjustmentIds, total, err := dueSettlementRows(tx, resellerOrgId, now)
		if err != nil {
			return err
		}
		if total >= 0 {
			return ErrNoDebtToClawBack
		}
		debt := -total
		if debt > int64(common.MaxWalletQuota) {
			return ErrWalletCannotCoverDebt
		}
		taken := tx.Model(&Organization{}).
			Where("id = ? AND wallet_quota >= ?", resellerOrgId, debt).
			Updates(map[string]any{
				"wallet_quota": gorm.Expr("wallet_quota - ?", debt),
				"updated_time": now,
			})
		if taken.Error != nil {
			return taken.Error
		}
		if taken.RowsAffected != 1 {
			return ErrWalletCannotCoverDebt
		}
		if err := insertLedger(tx, resellerOrgId, 0, int(debt), operatorId, LedgerTypeClawback, "", "settlement debt"); err != nil {
			return err
		}
		payout = &ResellerPayout{
			ResellerOrgId: resellerOrgId, Quota: total, Method: PayoutMethodWalletDebit,
			OperatorId: operatorId, PaidTime: now, CreatedTime: now,
		}
		if err := tx.Create(payout).Error; err != nil {
			return err
		}
		return markSettled(tx, payout.Id, itemIds, adjustmentIds)
	})
	if err != nil {
		return nil, err
	}
	return payout, nil
}

// AddManualSettlementAdjustment records an administrator's correction to what
// a reseller is owed (positive: owed more).
func AddManualSettlementAdjustment(resellerOrgId, customerOrgId int, quota int64, remark string, operatorId int, now int64) error {
	if quota == 0 {
		return common.NewMessage("The adjustment cannot be zero")
	}
	limit := int64(common.MaxWalletQuota)
	if quota > limit || quota < -limit {
		return common.NewMessage("The adjustment is out of range")
	}
	var reseller Organization
	if err := DB.Select("id").Where("id = ? AND type = ?", resellerOrgId, OrgTypeReseller).First(&reseller).Error; err != nil {
		return errors.New("distributor not found")
	}
	return DB.Create(&ResellerSettlementAdjustment{
		ResellerOrgId: resellerOrgId, CustomerOrgId: customerOrgId,
		Type: SettlementAdjustManual, Ref: fmt.Sprintf("%d:%d:%s", operatorId, now, common.GetRandomString(8)),
		Period: SettlementPeriodOf(now), Quota: quota, Remark: remark, OperatorId: operatorId, CreatedTime: now,
	}).Error
}

// RecordChargebackAdjustment takes back the commission resellers earned on
// money a reversed payment had brought in and the customer had already spent
// (shortfall, in quota). Only the commission is taken back, at the rate the
// customer has earned each reseller so far; the wholesale part of the loss is
// the platform's. The same payment is never counted twice.
//
// The resellers are the ones the customer's usage earned commission for, not
// the one it is linked to today: a customer that has since left, or been
// released, does not take the commission on a reversed payment with it. A
// customer that moved from one reseller to another earned both a commission,
// and each gives back its own share. What the customer spent under no
// reseller earned no commission and lowers every rate accordingly.
func RecordChargebackAdjustment(customerOrgId int, tradeNo string, shortfall int) error {
	if shortfall <= 0 || tradeNo == "" {
		return nil
	}
	var counted int64
	if err := DB.Model(&ResellerSettlementAdjustment{}).
		Where("type = ? AND (ref = ? OR ref LIKE ?)", SettlementAdjustChargeback, tradeNo, tradeNo+"#r%").
		Count(&counted).Error; err != nil {
		return err
	}
	if counted > 0 {
		return nil
	}
	var earners []struct {
		ResellerOrgId int
		Commission    int64
	}
	if err := DB.Model(&OrgUsageDaily{}).
		Select("reseller_org_id, COALESCE(SUM(commission_quota), 0) AS commission").
		Where("org_id = ? AND reseller_org_id > 0", customerOrgId).
		Group("reseller_org_id").Order("commission DESC, reseller_org_id ASC").
		Scan(&earners).Error; err != nil {
		return err
	}
	if len(earners) == 0 || earners[0].Commission <= 0 {
		return nil
	}
	var spent struct{ Charged int64 }
	if err := DB.Model(&OrgUsageDaily{}).
		Select("COALESCE(SUM(charged_quota), 0) AS charged").
		Where("org_id = ?", customerOrgId).
		Scan(&spent).Error; err != nil {
		return err
	}
	if spent.Charged <= 0 {
		return nil
	}
	now := common.GetTimestamp()
	remaining := int64(shortfall)
	for i, earned := range earners {
		if earned.Commission <= 0 || remaining <= 0 {
			break
		}
		// A payment can be far larger than one request, so this is wallet-sized
		// arithmetic: shortfall × commission ÷ charged, never more than shortfall.
		share := decimal.NewFromInt(int64(shortfall)).Mul(decimal.NewFromInt(earned.Commission)).Div(decimal.NewFromInt(spent.Charged))
		rounded, err := common.WalletQuotaFromDecimalStrict(share)
		if err != nil {
			return err
		}
		// Several reversed payments together never take back more than the
		// customer has earned the reseller.
		var taken struct{ Total int64 }
		if err := DB.Model(&ResellerSettlementAdjustment{}).
			Select("COALESCE(SUM(quota), 0) AS total").
			Where("type = ? AND reseller_org_id = ? AND customer_org_id = ?", SettlementAdjustChargeback, earned.ResellerOrgId, customerOrgId).
			Scan(&taken).Error; err != nil {
			return err
		}
		takenBack := min(int64(rounded), remaining, earned.Commission+taken.Total)
		if takenBack <= 0 {
			continue
		}
		// One adjustment per payment and reseller: the reseller that earned the
		// most carries the trade number itself, as every adjustment did when a
		// customer could have one reseller only.
		ref := tradeNo
		if i > 0 {
			ref = fmt.Sprintf("%s#r%d", tradeNo, earned.ResellerOrgId)
		}
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&ResellerSettlementAdjustment{
			ResellerOrgId: earned.ResellerOrgId, CustomerOrgId: customerOrgId,
			Type: SettlementAdjustChargeback, Ref: ref,
			Period: SettlementPeriodOf(now), Quota: -takenBack, CreatedTime: now,
		}).Error; err != nil {
			return err
		}
		remaining -= takenBack
	}
	return nil
}

// ListSettlementItems returns settlement items, newest month first, for one
// reseller (0: all) and one month ("": all), with names and status filled in.
func ListSettlementItems(resellerOrgId int, period string, startIdx, num int, now int64) ([]*ResellerSettlementItem, int64, error) {
	query := DB.Model(&ResellerSettlementItem{})
	if resellerOrgId > 0 {
		query = query.Where("reseller_org_id = ?", resellerOrgId)
	}
	if period != "" {
		query = query.Where("period = ?", period)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []*ResellerSettlementItem{}
	if err := query.Order("period DESC, id DESC").Limit(num).Offset(startIdx).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	ids := map[int]struct{}{}
	for _, item := range items {
		ids[item.ResellerOrgId] = struct{}{}
		ids[item.CustomerOrgId] = struct{}{}
	}
	names := orgNames(ids)
	for _, item := range items {
		item.ResellerName = names[item.ResellerOrgId]
		item.CustomerName = names[item.CustomerOrgId]
		item.Status = settlementItemStatus(item, now)
	}
	return items, total, nil
}

// ListSettlementAdjustments returns adjustments, newest first, for one
// reseller (0: all).
func ListSettlementAdjustments(resellerOrgId, startIdx, num int) ([]*ResellerSettlementAdjustment, int64, error) {
	query := DB.Model(&ResellerSettlementAdjustment{})
	if resellerOrgId > 0 {
		query = query.Where("reseller_org_id = ?", resellerOrgId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []*ResellerSettlementAdjustment{}
	if err := query.Order("id DESC").Limit(num).Offset(startIdx).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := map[int]struct{}{}
	for _, row := range rows {
		ids[row.ResellerOrgId] = struct{}{}
		ids[row.CustomerOrgId] = struct{}{}
	}
	names := orgNames(ids)
	for _, row := range rows {
		row.ResellerName = names[row.ResellerOrgId]
		row.CustomerName = names[row.CustomerOrgId]
	}
	return rows, total, nil
}

// ListResellerPayouts returns payouts, newest first, for one reseller (0: all).
func ListResellerPayouts(resellerOrgId, startIdx, num int) ([]*ResellerPayout, int64, error) {
	query := DB.Model(&ResellerPayout{})
	if resellerOrgId > 0 {
		query = query.Where("reseller_org_id = ?", resellerOrgId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []*ResellerPayout{}
	if err := query.Order("id DESC").Limit(num).Offset(startIdx).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := map[int]struct{}{}
	for _, row := range rows {
		ids[row.ResellerOrgId] = struct{}{}
	}
	names := orgNames(ids)
	for _, row := range rows {
		row.ResellerName = names[row.ResellerOrgId]
	}
	return rows, total, nil
}
