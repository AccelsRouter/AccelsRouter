package model

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm/clause"
)

// What finance reads next to the consumption reconciliation: the money that
// came in, the revenue the consumption earned, what resellers are owed, and
// the balances customers hold with the platform.

// Balance categories of FinanceBalanceSnapshot.
const (
	FinanceBalancePersonal        = "personal"
	FinanceBalanceEnterprise      = "enterprise"
	FinanceBalanceReseller        = "reseller"
	FinanceBalancePrepaidCustomer = "prepaid_customer"
	FinanceBalanceDirectCustomer  = "direct_customer"
)

var financeBalanceCategories = []string{
	FinanceBalancePersonal, FinanceBalanceEnterprise, FinanceBalanceReseller,
	FinanceBalancePrepaidCustomer, FinanceBalanceDirectCustomer,
}

// FinanceBalanceSnapshot is the total balance of one kind of account at the
// start of a day (UTC+8). Balances are not kept historically anywhere else,
// so an opening or closing balance exists only from the first snapshot on.
type FinanceBalanceSnapshot struct {
	Id          int    `json:"id" gorm:"primarykey"`
	DayBucket   int64  `json:"day_bucket" gorm:"uniqueIndex:idx_fbs_key,priority:1;not null"`
	Category    string `json:"category" gorm:"type:varchar(32);uniqueIndex:idx_fbs_key,priority:2;not null"`
	Quota       int64  `json:"quota"`
	CreatedTime int64  `json:"created_time"`
}

// currentFinanceBalances totals what each kind of account holds right now.
func currentFinanceBalances() (map[string]int64, error) {
	balances := make(map[string]int64, len(financeBalanceCategories))
	var personal struct{ Total int64 }
	if err := DB.Model(&User{}).Select("COALESCE(SUM(quota), 0) AS total").Scan(&personal).Error; err != nil {
		return nil, err
	}
	balances[FinanceBalancePersonal] = personal.Total
	customers, err := CustomerOrgIdSet()
	if err != nil {
		return nil, err
	}
	var orgs []Organization
	if err := DB.Select("id", "type", "wallet_quota", "funding_mode").Find(&orgs).Error; err != nil {
		return nil, err
	}
	for _, org := range orgs {
		category := FinanceBalanceEnterprise
		switch {
		case org.Type == OrgTypeReseller:
			category = FinanceBalanceReseller
		case org.FundingMode == OrgFundingDirect:
			category = FinanceBalanceDirectCustomer
		case customers[org.Id]:
			category = FinanceBalancePrepaidCustomer
		}
		balances[category] += int64(org.WalletQuota)
	}
	return balances, nil
}

// RecordFinanceBalanceSnapshot writes the day's balances once; later calls on
// the same day change nothing.
func RecordFinanceBalanceSnapshot(now int64) error {
	day := time.Unix(now, 0).In(settlementZone)
	bucket := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, settlementZone).Unix()
	var written int64
	if err := DB.Model(&FinanceBalanceSnapshot{}).Where("day_bucket = ?", bucket).Count(&written).Error; err != nil {
		return err
	}
	if written >= int64(len(financeBalanceCategories)) {
		return nil
	}
	balances, err := currentFinanceBalances()
	if err != nil {
		return err
	}
	for _, category := range financeBalanceCategories {
		row := &FinanceBalanceSnapshot{DayBucket: bucket, Category: category, Quota: balances[category], CreatedTime: now}
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}

// FinanceReceiptRow is the completed online payments of one kind over the
// window. Money is in the payment method's own currency, so rows of
// different methods are not added together; AmountUSD is what was credited.
type FinanceReceiptRow struct {
	// Source: "personal" (a user's own balance) or "organization" (a
	// self-funded organization's wallet).
	Source          string  `json:"source"`
	PaymentProvider string  `json:"payment_provider"`
	PaymentMethod   string  `json:"payment_method"`
	Count           int64   `json:"count"`
	Money           float64 `json:"money"`
	AmountUSD       int64   `json:"amount_usd"`
}

// FinanceNonCash is quota that entered or left accounts without a payment.
type FinanceNonCash struct {
	// Kind: credit_grant | credit_deduction | redemption | org_recorded |
	// org_debit | org_chargeback.
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
	Quota int64  `json:"quota"`
}

// FinanceRevenueRow is the consumption of one kind of customer over the
// window: at list price, what the customer side paid the platform for it,
// the commission owed out of that, and what the platform keeps.
type FinanceRevenueRow struct {
	// Category: personal | enterprise | reseller_own | prepaid_customer |
	// direct_customer.
	Category        string `json:"category"`
	StandardQuota   int64  `json:"standard_quota"`
	PaidQuota       int64  `json:"paid_quota"`
	CommissionQuota int64  `json:"commission_quota"`
	NetQuota        int64  `json:"net_quota"`
	Requests        int64  `json:"requests"`
}

// FinancePayableRow is one reseller's commission account over the window.
type FinancePayableRow struct {
	ResellerOrgId int    `json:"reseller_org_id"`
	ResellerName  string `json:"reseller_name"`
	Opening       int64  `json:"opening"`
	Accrued       int64  `json:"accrued"`
	Adjusted      int64  `json:"adjusted"`
	Paid          int64  `json:"paid"`
	Closing       int64  `json:"closing"`
	// Held and Due are as of now, not of the window's end: what is closed but
	// kept back, and what a payout would settle today.
	Held int64 `json:"held"`
	Due  int64 `json:"due"`
}

// FinanceBalanceRow is the balance of one kind of account. Opening and
// Closing come from the daily snapshots at or before the window's ends and
// are nil where no snapshot is that old; Current is live.
type FinanceBalanceRow struct {
	Category string `json:"category"`
	Opening  *int64 `json:"opening"`
	Closing  *int64 `json:"closing"`
	Current  int64  `json:"current"`
}

// FinanceReport is the finance view of a window.
type FinanceReport struct {
	From     int64               `json:"from"`
	To       int64               `json:"to"`
	Receipts []FinanceReceiptRow `json:"receipts"`
	NonCash  []FinanceNonCash    `json:"non_cash"`
	// Refunds paid out of personal balances over the window.
	RefundCount     int64               `json:"refund_count"`
	RefundAmountUSD float64             `json:"refund_amount_usd"`
	Revenue         []FinanceRevenueRow `json:"revenue"`
	Payables        []FinancePayableRow `json:"payables"`
	Balances        []FinanceBalanceRow `json:"balances"`
}

func financeReceipts(from, to int64) ([]FinanceReceiptRow, error) {
	placedForOrg := DB.Model(&OrgTopUp{}).Select("trade_no")
	notPayments := []string{PaymentMethodCreditGrant, PaymentMethodCreditDeduction, PaymentMethodBalance}
	rows := []FinanceReceiptRow{}
	for _, source := range []string{"personal", "organization"} {
		query := DB.Model(&TopUp{}).
			Select("payment_provider, payment_method, COUNT(*) AS count, COALESCE(SUM(money), 0) AS money, COALESCE(SUM(amount), 0) AS amount_usd").
			Where("status = ? AND complete_time >= ? AND complete_time <= ?", common.TopUpStatusSuccess, from, to).
			Where("payment_method NOT IN ?", notPayments).
			Group("payment_provider, payment_method")
		if source == "organization" {
			query = query.Where("trade_no IN (?)", placedForOrg)
		} else {
			query = query.Where("trade_no NOT IN (?)", placedForOrg)
		}
		var part []FinanceReceiptRow
		if err := query.Scan(&part).Error; err != nil {
			return nil, err
		}
		for i := range part {
			part[i].Source = source
		}
		rows = append(rows, part...)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Source != rows[j].Source {
			return rows[i].Source > rows[j].Source
		}
		return rows[i].AmountUSD > rows[j].AmountUSD
	})
	return rows, nil
}

func financeNonCash(from, to int64) ([]FinanceNonCash, error) {
	rows := []FinanceNonCash{}
	type sum struct{ Count, Quota int64 }
	for _, method := range []string{PaymentMethodCreditGrant, PaymentMethodCreditDeduction} {
		// These rows keep a whole-dollar amount, signed.
		var s struct{ Count, Amount int64 }
		if err := DB.Model(&TopUp{}).Select("COUNT(*) AS count, COALESCE(SUM(amount), 0) AS amount").
			Where("payment_method = ? AND complete_time >= ? AND complete_time <= ?", method, from, to).
			Scan(&s).Error; err != nil {
			return nil, err
		}
		rows = append(rows, FinanceNonCash{Kind: method, Count: s.Count, Quota: int64(common.QuotaFromFloat(float64(s.Amount) * common.QuotaPerUnit))})
	}
	var redeemed sum
	if err := DB.Model(&Redemption{}).Select("COUNT(*) AS count, COALESCE(SUM(quota), 0) AS quota").
		Where("redeemed_time >= ? AND redeemed_time <= ?", from, to).Scan(&redeemed).Error; err != nil {
		return nil, err
	}
	rows = append(rows, FinanceNonCash{Kind: "redemption", Count: redeemed.Count, Quota: redeemed.Quota})
	ledgerKinds := []struct {
		kind  string
		where string
		args  []any
	}{
		// A reseller buying wallet credit with its own balance is a transfer
		// between two balances, not something recorded by an administrator.
		{"org_recorded", "type = ? AND from_org_id = 0 AND trade_no NOT LIKE ?", []any{LedgerTypePurchase, "rspur-%"}},
		{"org_debit", "type = ?", []any{LedgerTypeDebit}},
		{"org_chargeback", "type = ?", []any{LedgerTypeChargeback}},
	}
	for _, kind := range ledgerKinds {
		var s sum
		if err := DB.Model(&CreditLedger{}).Select("COUNT(*) AS count, COALESCE(SUM(quota), 0) AS quota").
			Where(kind.where, kind.args...).
			Where("created_time >= ? AND created_time <= ?", from, to).Scan(&s).Error; err != nil {
			return nil, err
		}
		rows = append(rows, FinanceNonCash{Kind: kind.kind, Count: s.Count, Quota: s.Quota})
	}
	return rows, nil
}

func financeRevenue(from, to int64) ([]FinanceRevenueRow, error) {
	totalStandard, totalRequests, _, _, err := reconTotals(from, to)
	if err != nil {
		return nil, err
	}
	// Summed in the database, per organization and the reseller it had then.
	var usage []oudRow
	if err := DB.Model(&OrgUsageDaily{}).
		Select("org_id, reseller_org_id, COALESCE(SUM(standard_quota), 0) AS standard_quota, COALESCE(SUM(charged_quota), 0) AS charged_quota, COALESCE(SUM(cost_quota), 0) AS cost_quota, COALESCE(SUM(commission_quota), 0) AS commission_quota, COALESCE(SUM(requests), 0) AS requests").
		Where("day_bucket >= ? AND day_bucket <= ?", usageBucketOf(from), to).
		Group("org_id, reseller_org_id").
		Scan(&usage).Error; err != nil {
		return nil, err
	}
	directOrgs := map[int]bool{}
	var direct []int
	if err := DB.Model(&Organization{}).Where("funding_mode = ?", OrgFundingDirect).Pluck("id", &direct).Error; err != nil {
		return nil, err
	}
	for _, id := range direct {
		directOrgs[id] = true
	}
	byCategory := map[string]*FinanceRevenueRow{}
	row := func(category string) *FinanceRevenueRow {
		if byCategory[category] == nil {
			byCategory[category] = &FinanceRevenueRow{Category: category}
		}
		return byCategory[category]
	}
	var orgStandard, orgRequests int64
	for _, u := range usage {
		orgStandard += u.StandardQuota
		orgRequests += u.Requests
		var r *FinanceRevenueRow
		switch {
		case u.ResellerOrgId == 0:
			// An organization on its own: it pays what it was charged.
			r = row("enterprise")
			r.PaidQuota += u.ChargedQuota
			r.NetQuota += u.ChargedQuota
		case u.ResellerOrgId == u.OrgId:
			r = row("reseller_own")
			r.PaidQuota += u.ChargedQuota
			r.NetQuota += u.ChargedQuota
		case directOrgs[u.OrgId]:
			// The customer paid the platform; the margin goes to the reseller.
			r = row("direct_customer")
			r.PaidQuota += u.ChargedQuota
			r.CommissionQuota += u.CommissionQuota
			r.NetQuota += u.ChargedQuota - u.CommissionQuota
		default:
			// The reseller paid the platform its wholesale price.
			r = row("prepaid_customer")
			r.PaidQuota += u.CostQuota
			r.NetQuota += u.CostQuota
		}
		r.StandardQuota += u.StandardQuota
		r.Requests += u.Requests
	}
	personal := row("personal")
	personal.StandardQuota = max(totalStandard-orgStandard, 0)
	personal.PaidQuota = personal.StandardQuota
	personal.NetQuota = personal.StandardQuota
	personal.Requests = max(totalRequests-orgRequests, 0)
	rows := make([]FinanceRevenueRow, 0, 5)
	for _, category := range []string{"personal", "enterprise", "reseller_own", "prepaid_customer", "direct_customer"} {
		rows = append(rows, *row(category))
	}
	return rows, nil
}

func financePayables(from, to, now int64) ([]FinancePayableRow, error) {
	byReseller := map[int]*FinancePayableRow{}
	row := func(resellerOrgId int) *FinancePayableRow {
		if byReseller[resellerOrgId] == nil {
			byReseller[resellerOrgId] = &FinancePayableRow{ResellerOrgId: resellerOrgId}
		}
		return byReseller[resellerOrgId]
	}
	before, err := commissionSums(0, usageBucketOf(from), 0)
	if err != nil {
		return nil, err
	}
	for _, sum := range before {
		row(sum.ResellerOrgId).Opening += sum.Commission
	}
	within, err := commissionSums(usageBucketOf(from), to+1, 0)
	if err != nil {
		return nil, err
	}
	for _, sum := range within {
		row(sum.ResellerOrgId).Accrued += sum.Commission
	}
	var adjustments []ResellerSettlementAdjustment
	if err := DB.Select("reseller_org_id", "quota", "created_time").Find(&adjustments).Error; err != nil {
		return nil, err
	}
	for _, adjustment := range adjustments {
		switch {
		case adjustment.CreatedTime < from:
			row(adjustment.ResellerOrgId).Opening += adjustment.Quota
		case adjustment.CreatedTime <= to:
			row(adjustment.ResellerOrgId).Adjusted += adjustment.Quota
		}
	}
	var payouts []ResellerPayout
	if err := DB.Select("reseller_org_id", "quota", "paid_time").Find(&payouts).Error; err != nil {
		return nil, err
	}
	for _, payout := range payouts {
		switch {
		case payout.PaidTime < from:
			row(payout.ResellerOrgId).Opening -= payout.Quota
		case payout.PaidTime <= to:
			row(payout.ResellerOrgId).Paid += payout.Quota
		}
	}
	ids := map[int]struct{}{}
	for id := range byReseller {
		ids[id] = struct{}{}
	}
	names := orgNames(ids)
	rows := make([]FinancePayableRow, 0, len(byReseller))
	for id, r := range byReseller {
		r.ResellerName = names[id]
		r.Closing = r.Opening + r.Accrued + r.Adjusted - r.Paid
		summary, err := GetResellerSettlementSummary(id, now)
		if err != nil {
			return nil, err
		}
		r.Held, r.Due = summary.Held, summary.Due
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Closing != rows[j].Closing {
			return rows[i].Closing > rows[j].Closing
		}
		return rows[i].ResellerOrgId < rows[j].ResellerOrgId
	})
	return rows, nil
}

func financeBalances(from, to int64) ([]FinanceBalanceRow, error) {
	current, err := currentFinanceBalances()
	if err != nil {
		return nil, err
	}
	// The newest snapshot at or before a moment, per category.
	snapshotAt := func(moment int64) (map[string]int64, error) {
		var newest struct{ Day int64 }
		if err := DB.Model(&FinanceBalanceSnapshot{}).Select("COALESCE(MAX(day_bucket), 0) AS day").
			Where("day_bucket <= ?", moment).Scan(&newest).Error; err != nil {
			return nil, err
		}
		if newest.Day == 0 {
			return nil, nil
		}
		var rows []FinanceBalanceSnapshot
		if err := DB.Where("day_bucket = ?", newest.Day).Find(&rows).Error; err != nil {
			return nil, err
		}
		at := make(map[string]int64, len(rows))
		for _, r := range rows {
			at[r.Category] = r.Quota
		}
		return at, nil
	}
	opening, err := snapshotAt(from)
	if err != nil {
		return nil, err
	}
	closing, err := snapshotAt(to)
	if err != nil {
		return nil, err
	}
	rows := make([]FinanceBalanceRow, 0, len(financeBalanceCategories))
	for _, category := range financeBalanceCategories {
		r := FinanceBalanceRow{Category: category, Current: current[category]}
		if opening != nil {
			value := opening[category]
			r.Opening = &value
		}
		if closing != nil {
			value := closing[category]
			r.Closing = &value
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// GetFinanceReport builds the finance view of [from, to].
func GetFinanceReport(from, to, now int64) (*FinanceReport, error) {
	report := &FinanceReport{From: from, To: to}
	var err error
	if report.Receipts, err = financeReceipts(from, to); err != nil {
		return nil, err
	}
	if report.NonCash, err = financeNonCash(from, to); err != nil {
		return nil, err
	}
	var refunds struct {
		Count  int64
		Amount float64
	}
	if err = DB.Model(&RefundRequest{}).Select("COUNT(*) AS count, COALESCE(SUM(amount_usd), 0) AS amount").
		Where("status = ? AND processed_at >= ? AND processed_at <= ?", RefundStatusRefunded, time.Unix(from, 0), time.Unix(to, 0)).
		Scan(&refunds).Error; err != nil {
		return nil, err
	}
	report.RefundCount, report.RefundAmountUSD = refunds.Count, refunds.Amount
	if report.Revenue, err = financeRevenue(from, to); err != nil {
		return nil, err
	}
	if report.Payables, err = financePayables(from, to, now); err != nil {
		return nil, err
	}
	if report.Balances, err = financeBalances(from, to); err != nil {
		return nil, err
	}
	return report, nil
}
