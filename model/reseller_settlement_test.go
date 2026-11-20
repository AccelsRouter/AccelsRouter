package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reseller's commission on customers that pay the platform themselves is
// fixed per month and customer, kept back while a card payment can still be
// charged back, corrected by adjustments, and settled by payouts whose amount
// the server computes; a negative balance comes out of the reseller's wallet.
func TestResellerCommissionSettlement(t *testing.T) {
	migrateOrgTables(t)
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&OrgUsageDaily{}, &ResellerSettlementItem{}, &ResellerSettlementAdjustment{}, &ResellerPayout{}, &FinanceBalanceSnapshot{}, &Redemption{}, &RefundRequest{}))
	for _, table := range []string{"org_usage_daily", "reseller_settlement_items", "reseller_settlement_adjustments", "reseller_payouts", "finance_balance_snapshots", "redemptions", "refund_requests"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}

	reseller := mustCreateOrg(t, "acme-reseller", OrgTypeReseller, 1000)
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]any{"direct_pay_enabled": true}))
	steady, err := CreateResellerCustomer(reseller.Id, "steady-co", "default", OrgFundingDirect, 0, 1)
	require.NoError(t, err)
	carded, err := CreateResellerCustomer(reseller.Id, "carded-co", "default", OrgFundingDirect, 0, 1)
	require.NoError(t, err)
	prepaid, err := CreateResellerCustomer(reseller.Id, "prepaid-co", "default", "", 100, 1)
	require.NoError(t, err)

	start, end, err := SettlementPeriodBounds("2026-10")
	require.NoError(t, err)
	assert.Equal(t, "2026-10", SettlementPeriodOf(start))
	assert.Equal(t, "2026-11", SettlementPeriodOf(end))
	_, _, err = SettlementPeriodBounds("October")
	require.Error(t, err)

	inOctober := start + 3600
	require.NoError(t, RecordOrgUsageDaily(inOctober, steady.Id, 0, reseller.Id, 7, "claude-opus", 1000, 900, 800, 100, 1, 1))
	require.NoError(t, RecordOrgUsageDaily(inOctober, carded.Id, 0, reseller.Id, 8, "claude-opus", 2000, 1900, 1600, 300, 1, 1))
	require.NoError(t, RecordOrgUsageDaily(inOctober, prepaid.Id, 0, reseller.Id, 9, "claude-opus", 500, 450, 400, 0, 1, 1))
	// The customer export states what each customer has paid for its calls.
	paid, err := ResellerCustomerPaid(reseller.Id)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{steady.Id: 900, carded.Id: 1900, prepaid.Id: 450}, paid)
	// carded-co paid by card ten days before the month ended.
	require.NoError(t, DB.Create(&TopUp{UserId: 8, Amount: 5, Money: 5, TradeNo: "STRIPE-CARDED", PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe, CreateTime: end - 10*86400, CompleteTime: end - 10*86400, Status: common.TopUpStatusSuccess}).Error)
	require.NoError(t, DB.Create(&OrgTopUp{TradeNo: "STRIPE-CARDED", OrgId: carded.Id, UserId: 8, CreatedTime: end - 10*86400}).Error)

	_, _, err = CloseSettlementPeriod("2026-10", end-1)
	require.Error(t, err, "a month still running cannot be closed")

	now := end + 3*86400
	items, adjustments, err := CloseSettlementPeriod("2026-10", now)
	require.NoError(t, err)
	assert.Equal(t, 2, items, "one item per self-funded customer; the prepaid one earns no commission")
	assert.Equal(t, 0, adjustments)
	items, adjustments, err = CloseSettlementPeriod("2026-10", now)
	require.NoError(t, err)
	assert.Equal(t, 0, items+adjustments, "closing again writes nothing")

	listed, total, err := ListSettlementItems(reseller.Id, "2026-10", 0, 10, now)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	byCustomer := map[int]*ResellerSettlementItem{}
	for _, item := range listed {
		byCustomer[item.CustomerOrgId] = item
	}
	assert.Equal(t, int64(100), byCustomer[steady.Id].CommissionQuota)
	assert.Equal(t, "payable", byCustomer[steady.Id].Status)
	assert.Equal(t, "steady-co", byCustomer[steady.Id].CustomerName)
	assert.Equal(t, int64(300), byCustomer[carded.Id].CommissionQuota)
	assert.Equal(t, "held", byCustomer[carded.Id].Status, "a card payment can still be charged back")
	assert.Equal(t, end+120*86400, byCustomer[carded.Id].ReleaseAt)

	// November is still open; a late refund moves October after it was closed.
	require.NoError(t, RecordOrgUsageDaily(end+3600, steady.Id, 0, reseller.Id, 7, "claude-opus", 500, 450, 400, 50, 1, 1))
	require.NoError(t, AdjustOrgUsageDaily(inOctober, steady.Id, 0, reseller.Id, 7, "claude-opus", -200, -180, -160, -20))
	items, adjustments, err = CloseSettlementPeriod("2026-10", now)
	require.NoError(t, err)
	assert.Equal(t, 0, items)
	assert.Equal(t, 1, adjustments)
	_, adjustments, err = CloseSettlementPeriod("2026-10", now+1)
	require.NoError(t, err)
	assert.Equal(t, 0, adjustments, "the difference is adjusted once")

	summary, err := GetResellerSettlementSummary(reseller.Id, now)
	require.NoError(t, err)
	assert.Equal(t, int64(50), summary.Accruing)
	assert.Equal(t, int64(300), summary.Held)
	assert.Equal(t, int64(100), summary.Payable)
	assert.Equal(t, int64(-20), summary.Adjustments)
	assert.Equal(t, int64(80), summary.Due)

	// The payout settles what is due, at the amount the server computed.
	payout := &ResellerPayout{ResellerOrgId: reseller.Id, Quota: 999999, Money: 0.16, Currency: "USD", Method: PayoutMethodBank, Reference: "wire-1"}
	require.NoError(t, RegisterResellerPayout(payout, now))
	assert.Equal(t, int64(80), payout.Quota)
	require.ErrorIs(t, RegisterResellerPayout(&ResellerPayout{ResellerOrgId: reseller.Id, Money: 1, Method: PayoutMethodBank, Reference: "wire-2"}, now), ErrNothingToSettle)
	require.Error(t, RegisterResellerPayout(&ResellerPayout{ResellerOrgId: reseller.Id, Money: 1, Method: PayoutMethodWalletDebit}, now), "a wallet debit is not a payout an administrator records")
	summary, err = GetResellerSettlementSummary(reseller.Id, now)
	require.NoError(t, err)
	assert.Equal(t, int64(0), summary.Due)
	assert.Equal(t, int64(80), summary.Paid)

	// A reversed payment whose money was spent takes back the commission on
	// it, at the rate the customer has earned the reseller: 300 / 1900.
	require.NoError(t, RecordChargebackAdjustment(carded.Id, "STRIPE-CARDED", 950))
	require.NoError(t, RecordChargebackAdjustment(carded.Id, "STRIPE-CARDED", 950), "the same payment is counted once")
	require.NoError(t, RecordChargebackAdjustment(prepaid.Id, "OTHER", 950), "a prepaid customer earns no commission to take back")
	summary, err = GetResellerSettlementSummary(reseller.Id, now)
	require.NoError(t, err)
	assert.Equal(t, int64(-150), summary.Due)

	// A payment larger than any single request is not cut short: with thirty-
	// eight billion charged and six billion earned in all, nineteen billion
	// reversed takes back three billion.
	require.NoError(t, RecordOrgUsageDaily(now, carded.Id, 0, reseller.Id, 8, "large-model", 40_000_000_000, 38_000_000_000-1900, 32_000_000_000, 6_000_000_000-300, 1, 1))
	require.NoError(t, RecordChargebackAdjustment(carded.Id, "STRIPE-LARGE", 19_000_000_000))
	var large ResellerSettlementAdjustment
	require.NoError(t, DB.Where("ref = ?", "STRIPE-LARGE").First(&large).Error)
	assert.Equal(t, int64(-3_000_000_000), large.Quota)
	require.NoError(t, DB.Delete(&large).Error)
	require.NoError(t, DB.Where("model_name = ?", "large-model").Delete(&OrgUsageDaily{}).Error)

	// What the reseller owes comes out of its wallet, in full or not at all.
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]any{"wallet_quota": 100}))
	_, err = ClawbackResellerDebt(reseller.Id, 1, now)
	require.ErrorIs(t, err, ErrWalletCannotCoverDebt)
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]any{"wallet_quota": 1000}))
	debit, err := ClawbackResellerDebt(reseller.Id, 1, now)
	require.NoError(t, err)
	assert.Equal(t, int64(-150), debit.Quota)
	assert.Equal(t, PayoutMethodWalletDebit, debit.Method)
	fresh, err := GetOrganizationById(reseller.Id)
	require.NoError(t, err)
	assert.Equal(t, 850, fresh.WalletQuota)
	_, err = ClawbackResellerDebt(reseller.Id, 1, now)
	require.ErrorIs(t, err, ErrNoDebtToClawBack)

	// A customer that has left its reseller does not take the commission with
	// it: a payment reversed afterwards is still charged to the reseller its
	// usage paid. What it spent since earned no commission, so the rate is
	// the 130 it earned the reseller over the 1270 it was charged in all.
	_, err = LeaveReseller(steady.Id)
	require.NoError(t, err)
	require.NoError(t, RecordOrgUsageDaily(now, steady.Id, 0, 0, 7, "claude-opus", 100, 100, 0, 0, 1, 1))
	require.NoError(t, RecordChargebackAdjustment(steady.Id, "STRIPE-AFTER-LEAVING", 500))
	var late ResellerSettlementAdjustment
	require.NoError(t, DB.Where("ref = ?", "STRIPE-AFTER-LEAVING").First(&late).Error)
	assert.Equal(t, reseller.Id, late.ResellerOrgId)
	assert.Equal(t, int64(-51), late.Quota)
	// Reversed payments together never take back more than the customer
	// earned the reseller: with everything it spent reversed, the second one
	// takes the 79 that are left of the 130.
	require.NoError(t, RecordChargebackAdjustment(steady.Id, "STRIPE-EVERYTHING", 1270))
	var rest ResellerSettlementAdjustment
	require.NoError(t, DB.Where("ref = ?", "STRIPE-EVERYTHING").First(&rest).Error)
	assert.Equal(t, int64(-79), rest.Quota)
	require.NoError(t, RecordChargebackAdjustment(steady.Id, "STRIPE-NOTHING-LEFT", 1270))
	require.NoError(t, DB.Where("ref IN ?", []string{"STRIPE-AFTER-LEAVING", "STRIPE-EVERYTHING"}).Delete(&ResellerSettlementAdjustment{}).Error)

	// A customer that went on to a second reseller earned both a commission:
	// 130 for the first and 60 for the second, of the 1670 it was charged in
	// all. Each gives back its own share of a reversed payment, once.
	second := mustCreateOrg(t, "second-reseller", OrgTypeReseller, 0)
	require.NoError(t, RecordOrgUsageDaily(now, steady.Id, 0, second.Id, 7, "second-era", 400, 400, 340, 60, 1, 1))
	for range 2 {
		require.NoError(t, RecordChargebackAdjustment(steady.Id, "STRIPE-TWO-RESELLERS", 835))
	}
	var split []ResellerSettlementAdjustment
	require.NoError(t, DB.Where("ref LIKE ?", "STRIPE-TWO-RESELLERS%").Order("quota ASC").Find(&split).Error)
	require.Len(t, split, 2)
	assert.Equal(t, reseller.Id, split[0].ResellerOrgId)
	assert.Equal(t, int64(-65), split[0].Quota)
	assert.Equal(t, "STRIPE-TWO-RESELLERS", split[0].Ref)
	assert.Equal(t, second.Id, split[1].ResellerOrgId)
	assert.Equal(t, int64(-30), split[1].Quota)
	require.NoError(t, DB.Where("ref LIKE ?", "STRIPE-TWO-RESELLERS%").Delete(&ResellerSettlementAdjustment{}).Error)
	require.NoError(t, DB.Where("model_name = ?", "second-era").Delete(&OrgUsageDaily{}).Error)

	// Once the hold has passed, the held item is paid too.
	later := end + 121*86400
	require.NoError(t, AddManualSettlementAdjustment(reseller.Id, carded.Id, 25, "goodwill", 1, later))
	require.Error(t, AddManualSettlementAdjustment(reseller.Id, carded.Id, 0, "nothing", 1, later))
	final := &ResellerPayout{ResellerOrgId: reseller.Id, Money: 0.65, Currency: "USD", Method: PayoutMethodCrypto, Reference: "0xabc"}
	require.NoError(t, RegisterResellerPayout(final, later))
	assert.Equal(t, int64(325), final.Quota)
	payouts, total, err := ListResellerPayouts(reseller.Id, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Equal(t, "acme-reseller", payouts[0].ResellerName)
	adjusted, total, err := ListSettlementAdjustments(reseller.Id, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	for _, row := range adjusted {
		assert.NotZero(t, row.PayoutId, "every adjustment was settled by a payout")
	}

	// The finance view of October. A large administrator deduction is shown
	// in full, not capped at what a single request may cost.
	require.NoError(t, DB.Create(&TopUp{UserId: 8, Amount: -5000, TradeNo: "DEDUCT-LARGE", PaymentMethod: PaymentMethodCreditDeduction, PaymentProvider: PaymentMethodCreditDeduction, CreateTime: inOctober, CompleteTime: inOctober, Status: common.TopUpStatusSuccess}).Error)
	report, err := GetFinanceReport(start, end-1, now)
	require.NoError(t, err)
	nonCash := map[string]FinanceNonCash{}
	for _, row := range report.NonCash {
		nonCash[row.Kind] = row
	}
	assert.Equal(t, int64(-5000*common.QuotaPerUnit), nonCash[PaymentMethodCreditDeduction].Quota)
	assert.Less(t, nonCash[PaymentMethodCreditDeduction].Quota, int64(-2_147_483_647))
	revenue := map[string]FinanceRevenueRow{}
	for _, row := range report.Revenue {
		revenue[row.Category] = row
	}
	assert.Equal(t, int64(2800-180), revenue["direct_customer"].PaidQuota)
	assert.Equal(t, int64(380), revenue["direct_customer"].CommissionQuota)
	assert.Equal(t, int64(2800-180-380), revenue["direct_customer"].NetQuota)
	assert.Equal(t, int64(400), revenue["prepaid_customer"].PaidQuota, "the reseller paid its wholesale price")
	assert.Equal(t, int64(0), revenue["prepaid_customer"].CommissionQuota)
	require.Len(t, report.Payables, 1)
	assert.Equal(t, int64(0), report.Payables[0].Opening)
	assert.Equal(t, int64(380), report.Payables[0].Accrued)
	assert.Equal(t, int64(380), report.Payables[0].Closing)
	require.Len(t, report.Receipts, 1)
	assert.Equal(t, "organization", report.Receipts[0].Source)
	assert.Equal(t, PaymentProviderStripe, report.Receipts[0].PaymentProvider)
	assert.Equal(t, int64(1), report.Receipts[0].Count)
	// After October: the adjustments and payouts move the account to zero.
	afterwards, err := GetFinanceReport(end, later+86400, later+86400)
	require.NoError(t, err)
	require.Len(t, afterwards.Payables, 1)
	assert.Equal(t, int64(380), afterwards.Payables[0].Opening)
	assert.Equal(t, int64(50), afterwards.Payables[0].Accrued)
	assert.Equal(t, int64(-20-150+25), afterwards.Payables[0].Adjusted)
	assert.Equal(t, int64(80-150+325), afterwards.Payables[0].Paid)
	assert.Equal(t, int64(380+50-145-255), afterwards.Payables[0].Closing)

	// Balances exist from the first daily snapshot on.
	for _, row := range report.Balances {
		assert.Nil(t, row.Closing, "no snapshot is that old")
	}
	require.NoError(t, RecordFinanceBalanceSnapshot(now))
	require.NoError(t, RecordFinanceBalanceSnapshot(now+60))
	var snapshots int64
	require.NoError(t, DB.Model(&FinanceBalanceSnapshot{}).Count(&snapshots).Error)
	assert.Equal(t, int64(5), snapshots)
	withBalances, err := GetFinanceReport(start, now+86400, now+86400)
	require.NoError(t, err)
	balances := map[string]FinanceBalanceRow{}
	for _, row := range withBalances.Balances {
		balances[row.Category] = row
	}
	require.NotNil(t, balances[FinanceBalanceReseller].Closing)
	assert.Equal(t, int64(850), *balances[FinanceBalanceReseller].Closing)
	assert.Nil(t, balances[FinanceBalanceReseller].Opening)
	assert.Equal(t, int64(100), balances[FinanceBalancePrepaidCustomer].Current)
}
