package model

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// What collecting a self-funded customer's payment costs the platform is the
// reseller's to bear: each completed payment into such a customer's wallet
// takes a fee off what the reseller is owed, as a settlement adjustment. The
// fee is a share of the payment plus a fixed amount, set per payment channel.

// SettlementAdjustAcquiringFee is the adjustment a payment's fee is booked as;
// its Ref is the payment's order number.
const SettlementAdjustAcquiringFee = "acquiring_fee"

// AcquiringFeeOptionKey stores the fee schedule as JSON.
const AcquiringFeeOptionKey = "ResellerAcquiringFees"

// AcquiringFee is what one payment costs: Rate of its amount (0.04 is 4%)
// plus Fixed US dollars.
type AcquiringFee struct {
	Rate  float64 `json:"rate"`
	Fixed float64 `json:"fixed"`
}

// AcquiringFeeSchedule is the fee of every payment channel: Providers by the
// channel's name, Default for the ones not listed.
type AcquiringFeeSchedule struct {
	Default   AcquiringFee            `json:"default"`
	Providers map[string]AcquiringFee `json:"providers"`
}

// AcquiringFeeProviders are the channels a fee can be set for.
var AcquiringFeeProviders = []string{
	PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem,
	PaymentProviderWaffo, PaymentProviderWaffoPancake, PaymentProviderWonderGate,
}

// GetAcquiringFeeSchedule returns the schedule in force: 4% of every payment
// until an administrator saves another.
func GetAcquiringFeeSchedule() AcquiringFeeSchedule {
	schedule := AcquiringFeeSchedule{Default: AcquiringFee{Rate: 0.04}, Providers: map[string]AcquiringFee{}}
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[AcquiringFeeOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return schedule
	}
	var saved AcquiringFeeSchedule
	if err := common.UnmarshalJsonStr(raw, &saved); err != nil {
		common.SysError("the saved acquiring fee schedule cannot be read, using the default: " + err.Error())
		return schedule
	}
	if saved.Providers == nil {
		saved.Providers = map[string]AcquiringFee{}
	}
	return saved
}

// SaveAcquiringFeeSchedule replaces the schedule. It applies to payments
// completed from now on; fees already booked stay as they are.
func SaveAcquiringFeeSchedule(schedule AcquiringFeeSchedule) error {
	if schedule.Providers == nil {
		schedule.Providers = map[string]AcquiringFee{}
	}
	fees := []AcquiringFee{schedule.Default}
	for provider, fee := range schedule.Providers {
		if !slices.Contains(AcquiringFeeProviders, provider) {
			return common.NewMessage("Unknown payment channel")
		}
		fees = append(fees, fee)
	}
	for _, fee := range fees {
		// Written so that NaN fails both.
		if !(fee.Rate >= 0 && fee.Rate <= 0.5) {
			return common.NewMessage("The fee rate must be between 0% and 50%")
		}
		if !(fee.Fixed >= 0 && fee.Fixed <= 100) {
			return common.NewMessage("The fixed fee must be between 0 and 100 USD")
		}
	}
	raw, err := common.Marshal(schedule)
	if err != nil {
		return err
	}
	return UpdateOption(AcquiringFeeOptionKey, string(raw))
}

// acquiringFeeRef is the adjustment reference of a payment's fee: the order
// number, or its digest when the number would not fit the column.
func acquiringFeeRef(tradeNo string) string {
	if len(tradeNo) <= 88 {
		return tradeNo
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(tradeNo)))
}

// chargeAcquiringFee books the fee of a payment that has just been credited to
// a customer's wallet against the customer's reseller, in the payment's own
// transaction. quota is what the payment put into the wallet. A customer
// without a reseller, a credit that is not a payment order, and a fee of zero
// book nothing; an order is charged once.
func chargeAcquiringFee(tx *gorm.DB, customerOrgId int, tradeNo string, quota int) error {
	if tradeNo == "" || quota <= 0 {
		return nil
	}
	var link ResellerCustomerLink
	linked := tx.Where("customer_org_id = ?", customerOrgId).Limit(1).Find(&link)
	if linked.Error != nil || linked.RowsAffected == 0 {
		return linked.Error
	}
	var order TopUp
	if err := tx.Select("payment_provider", "payment_method").Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return err
	}
	provider := order.PaymentProvider
	if provider == "" {
		provider = order.PaymentMethod
	}
	schedule := GetAcquiringFeeSchedule()
	terms, listed := schedule.Providers[provider]
	if !listed {
		terms = schedule.Default
	}
	paid := decimal.NewFromInt(int64(quota))
	rate := decimal.NewFromFloat(terms.Rate)
	fixed := decimal.NewFromFloat(terms.Fixed)
	// A fee never exceeds the payment it is charged on.
	fee, err := common.WalletQuotaFromDecimalStrict(decimal.Min(paid,
		paid.Mul(rate).Add(fixed.Mul(decimal.NewFromFloat(common.QuotaPerUnit)))))
	if err != nil {
		return err
	}
	if fee <= 0 {
		return nil
	}
	now := common.GetTimestamp()
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ResellerSettlementAdjustment{
		ResellerOrgId: link.ResellerOrgId, CustomerOrgId: customerOrgId,
		Type: SettlementAdjustAcquiringFee, Ref: acquiringFeeRef(tradeNo),
		Period: SettlementPeriodOf(now), Quota: -int64(fee),
		Remark:      fmt.Sprintf("%s: %s%% + $%s", provider, rate.Mul(decimal.NewFromInt(100)).String(), fixed.String()),
		CreatedTime: now,
	}).Error
}

// returnAcquiringFee gives a reseller back the fee of a payment that has been
// reversed: money that did not stay earns the platform no fee from it. A
// payment that was never charged a fee, or whose fee was already returned,
// changes nothing.
func returnAcquiringFee(tx *gorm.DB, tradeNo string) error {
	ref := acquiringFeeRef(tradeNo)
	var fee ResellerSettlementAdjustment
	charged := tx.Where("type = ? AND ref = ?", SettlementAdjustAcquiringFee, ref).Limit(1).Find(&fee)
	if charged.Error != nil || charged.RowsAffected == 0 {
		return charged.Error
	}
	now := common.GetTimestamp()
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ResellerSettlementAdjustment{
		ResellerOrgId: fee.ResellerOrgId, CustomerOrgId: fee.CustomerOrgId,
		Type: SettlementAdjustAcquiringFee, Ref: ref + "#back",
		Period: SettlementPeriodOf(now), Quota: -fee.Quota, CreatedTime: now,
	}).Error
}

// ResellerCustomerPayment is one payment a customer made into its own wallet
// while it was the reseller's, with the fee it cost the reseller.
type ResellerCustomerPayment struct {
	TradeNo       string `json:"trade_no"`
	CustomerOrgId int    `json:"customer_org_id"`
	CustomerName  string `json:"customer_name"`
	Provider      string `json:"provider"`
	// Quota is what the payment put into the customer's wallet.
	Quota int64 `json:"quota"`
	// Fee is what the payment took off the reseller's account; FeeReturned
	// says it was given back because the payment was reversed.
	Fee         int64 `json:"fee"`
	FeeReturned bool  `json:"fee_returned"`
	// Reversed: the payment channel took the payment back.
	Reversed    bool  `json:"reversed"`
	CreatedTime int64 `json:"created_time"`
}

// ListResellerCustomerPayments returns the payments the reseller's current
// customers made into their own wallets since they became its customers,
// newest first. These never move the reseller's wallet; they are listed for
// the fee each one costs it.
func ListResellerCustomerPayments(resellerOrgId, startIdx, num int) ([]*ResellerCustomerPayment, int64, error) {
	query := DB.Model(&CreditLedger{}).
		Joins("JOIN reseller_customer_links ON reseller_customer_links.customer_org_id = credit_ledgers.to_org_id AND reseller_customer_links.reseller_org_id = ?", resellerOrgId).
		Where("credit_ledgers.type = ? AND credit_ledgers.created_time >= reseller_customer_links.created_time", LedgerTypeTopUp)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var credits []CreditLedger
	if err := query.Select("credit_ledgers.*").Order("credit_ledgers.id DESC").Limit(num).Offset(startIdx).Find(&credits).Error; err != nil {
		return nil, 0, err
	}
	payments := make([]*ResellerCustomerPayment, 0, len(credits))
	if len(credits) == 0 {
		return payments, total, nil
	}
	orgIds := map[int]struct{}{}
	var tradeNos, feeRefs []string
	for _, credit := range credits {
		orgIds[credit.ToOrgId] = struct{}{}
		if credit.TradeNo != "" {
			tradeNos = append(tradeNos, credit.TradeNo)
			ref := acquiringFeeRef(credit.TradeNo)
			feeRefs = append(feeRefs, ref, ref+"#back")
		}
	}
	orders := map[string]TopUp{}
	fees := map[string]int64{}
	if len(tradeNos) > 0 {
		var found []TopUp
		if err := DB.Select("trade_no", "payment_provider", "payment_method", "status").Where("trade_no IN ?", tradeNos).Find(&found).Error; err != nil {
			return nil, 0, err
		}
		for _, order := range found {
			orders[order.TradeNo] = order
		}
		var booked []ResellerSettlementAdjustment
		if err := DB.Where("type = ? AND reseller_org_id = ? AND ref IN ?", SettlementAdjustAcquiringFee, resellerOrgId, feeRefs).Find(&booked).Error; err != nil {
			return nil, 0, err
		}
		for _, fee := range booked {
			fees[fee.Ref] = fee.Quota
		}
	}
	names := orgNames(orgIds)
	for _, credit := range credits {
		order := orders[credit.TradeNo]
		provider := order.PaymentProvider
		if provider == "" {
			provider = order.PaymentMethod
		}
		ref := acquiringFeeRef(credit.TradeNo)
		_, returned := fees[ref+"#back"]
		payments = append(payments, &ResellerCustomerPayment{
			TradeNo: credit.TradeNo, CustomerOrgId: credit.ToOrgId, CustomerName: names[credit.ToOrgId],
			Provider: provider, Quota: int64(credit.Quota),
			Fee: -fees[ref], FeeReturned: returned,
			Reversed:    order.TradeNo != "" && order.Status != common.TopUpStatusSuccess,
			CreatedTime: credit.CreatedTime,
		})
	}
	return payments, total, nil
}
