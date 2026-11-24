package controller

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// Settlement of reseller commission: the reseller reads its own account, the
// platform administrator closes months, records payouts and corrections.

// GetMyResellerEarnings — GET /api/reseller/earnings
func GetMyResellerEarnings(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	summary, err := model.GetResellerSettlementSummary(reseller.Id, common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// The reseller sees what collecting its customers' payments costs it.
	fees := model.GetAcquiringFeeSchedule()
	summary.AcquiringFees = &fees
	common.ApiSuccess(c, summary)
}

// AdminGetAcquiringFees — GET /api/admin/reseller-acquiring-fees
// What a payment into a self-funded customer's wallet costs its reseller, by
// payment channel.
func AdminGetAcquiringFees(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"schedule": model.GetAcquiringFeeSchedule(), "providers": model.AcquiringFeeProviders})
}

// AdminSetAcquiringFees — PUT /api/admin/reseller-acquiring-fees
// Replaces the fee schedule for payments completed from now on.
func AdminSetAcquiringFees(c *gin.Context) {
	var schedule model.AcquiringFeeSchedule
	if err := c.ShouldBindJSON(&schedule); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SaveAcquiringFeeSchedule(schedule); err != nil {
		common.ApiError(c, err)
		return
	}
	saved := model.GetAcquiringFeeSchedule()
	common.SysLog(fmt.Sprintf("acquiring fee schedule changed by user %d: default %v, channels %v", c.GetInt("id"), saved.Default, saved.Providers))
	common.ApiSuccess(c, saved)
}

// ListMyResellerCustomerPayments — GET /api/reseller/customer-payments
// The payments the reseller's customers made into their own wallets, with the
// fee each one cost the reseller.
func ListMyResellerCustomerPayments(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	payments, total, err := model.ListResellerCustomerPayments(reseller.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(payments)
	common.ApiSuccess(c, page)
}

// ListMyResellerSettlements — GET /api/reseller/settlements
func ListMyResellerSettlements(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	items, total, err := model.ListSettlementItems(reseller.Id, c.Query("period"), page.GetStartIdx(), page.GetPageSize(), common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

// ListMyResellerSettlementAdjustments — GET /api/reseller/settlements/adjustments
func ListMyResellerSettlementAdjustments(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListSettlementAdjustments(reseller.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// ListMyResellerPayouts — GET /api/reseller/payouts
func ListMyResellerPayouts(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListResellerPayouts(reseller.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// AdminListResellerSettlements — GET /api/admin/reseller-settlements
// Items of one month and/or one reseller (both optional).
func AdminListResellerSettlements(c *gin.Context) {
	resellerOrgId, _ := strconv.Atoi(c.Query("reseller_org_id"))
	page := common.GetPageQuery(c)
	items, total, err := model.ListSettlementItems(resellerOrgId, c.Query("period"), page.GetStartIdx(), page.GetPageSize(), common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

// AdminExportResellerSettlements — GET /api/admin/reseller-settlements/export (CSV)
func AdminExportResellerSettlements(c *gin.Context) {
	resellerOrgId, _ := strconv.Atoi(c.Query("reseller_org_id"))
	period := c.Query("period")
	items, _, err := model.ListSettlementItems(resellerOrgId, period, 0, 5000, common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	name := "reseller-settlements"
	if period != "" {
		name += "-" + period
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+name+".csv\"")
	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	usd := func(quota int64) string { return fmt.Sprintf("%.6f", float64(quota)/common.QuotaPerUnit) }
	_ = w.Write([]string{"Period", "Distributor", "Customer", "List price (USD)", "Customer paid (USD)", "Wholesale (USD)", "Commission (USD)", "Requests", "Status", "Payable from", "Payout"})
	for _, item := range items {
		payout := ""
		if item.PayoutId != 0 {
			payout = strconv.Itoa(item.PayoutId)
		}
		_ = w.Write([]string{
			item.Period, csvSafe(item.ResellerName), csvSafe(item.CustomerName),
			usd(item.StandardQuota), usd(item.ChargedQuota), usd(item.CostQuota), usd(item.CommissionQuota),
			strconv.FormatInt(item.Requests, 10), item.Status,
			time.Unix(item.ReleaseAt, 0).UTC().Format("2006-01-02"), payout,
		})
	}
}

// AdminCloseResellerSettlements — POST /api/admin/reseller-settlements/close
// Closes a month that has ended; closing again only picks up later usage.
func AdminCloseResellerSettlements(c *gin.Context) {
	var req struct {
		Period string `json:"period"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	items, adjustments, err := model.CloseSettlementPeriod(strings.TrimSpace(req.Period), common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "adjustments": adjustments})
}

// AdminListResellerSettlementAdjustments — GET /api/admin/reseller-adjustments
func AdminListResellerSettlementAdjustments(c *gin.Context) {
	resellerOrgId, _ := strconv.Atoi(c.Query("reseller_org_id"))
	page := common.GetPageQuery(c)
	rows, total, err := model.ListSettlementAdjustments(resellerOrgId, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// AdminAddResellerSettlementAdjustment — POST /api/admin/reseller-adjustments
// A correction to what a reseller is owed; quota is signed.
func AdminAddResellerSettlementAdjustment(c *gin.Context) {
	var req struct {
		ResellerOrgId int    `json:"reseller_org_id"`
		CustomerOrgId int    `json:"customer_org_id"`
		Quota         int64  `json:"quota"`
		Remark        string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	remark := strings.TrimSpace(req.Remark)
	if remark == "" {
		common.ApiErrorT(c, "A remark is required for an adjustment")
		return
	}
	if r := []rune(remark); len(r) > 255 {
		remark = string(r[:255])
	}
	if err := model.AddManualSettlementAdjustment(req.ResellerOrgId, req.CustomerOrgId, req.Quota, remark, c.GetInt("id"), common.GetTimestamp()); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(req.ResellerOrgId, c.GetInt("id"), "settlement.adjust", fmt.Sprintf("org:%d", req.ResellerOrgId), fmt.Sprintf("quota=%d", req.Quota))
	common.ApiSuccess(c, nil)
}

// AdminListResellerPayouts — GET /api/admin/reseller-payouts
func AdminListResellerPayouts(c *gin.Context) {
	resellerOrgId, _ := strconv.Atoi(c.Query("reseller_org_id"))
	page := common.GetPageQuery(c)
	rows, total, err := model.ListResellerPayouts(resellerOrgId, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// trimmedTo cuts a free-text field to the room its column has.
func trimmedTo(text string, limit int) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > limit {
		return string(r[:limit])
	}
	return text
}

// AdminRegisterResellerPayout — POST /api/admin/reseller-payouts
// Records a payment already made to a reseller and settles everything due to
// it. The settled amount is computed by the server.
func AdminRegisterResellerPayout(c *gin.Context) {
	var req struct {
		ResellerOrgId int     `json:"reseller_org_id"`
		Money         float64 `json:"money"`
		Currency      string  `json:"currency"`
		Method        string  `json:"method"`
		Reference     string  `json:"reference"`
		InvoiceNo     string  `json:"invoice_no"`
		Remark        string  `json:"remark"`
		PaidTime      int64   `json:"paid_time"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Money <= 0 || req.Money > 1e12 {
		common.ApiErrorT(c, "Enter the amount that was paid")
		return
	}
	reference := trimmedTo(req.Reference, 128)
	if reference == "" {
		common.ApiErrorT(c, "Enter the bank or on-chain reference of the payment")
		return
	}
	now := common.GetTimestamp()
	if req.PaidTime > now {
		req.PaidTime = now
	}
	payout := &model.ResellerPayout{
		ResellerOrgId: req.ResellerOrgId,
		Money:         req.Money,
		Currency:      strings.ToUpper(trimmedTo(req.Currency, 8)),
		Method:        req.Method,
		Reference:     reference,
		InvoiceNo:     trimmedTo(req.InvoiceNo, 64),
		Remark:        trimmedTo(req.Remark, 255),
		OperatorId:    c.GetInt("id"),
		PaidTime:      req.PaidTime,
	}
	if err := model.RegisterResellerPayout(payout, now); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(req.ResellerOrgId, c.GetInt("id"), "settlement.payout", fmt.Sprintf("org:%d", req.ResellerOrgId), fmt.Sprintf("quota=%d payout=%d", payout.Quota, payout.Id))
	model.NotifyOrgAdmins(req.ResellerOrgId, c.GetInt("id"), common.NewMessage("A commission payout of {{amount}} was registered, reference {{reference}}", map[string]any{
		"amount": strings.TrimSpace(fmt.Sprintf("%s %s", strconv.FormatFloat(payout.Money, 'f', -1, 64), payout.Currency)), "reference": payout.Reference,
	}))
	common.ApiSuccess(c, payout)
}

// AdminClawbackResellerDebt — POST /api/admin/reseller-settlements/clawback
// Takes what a reseller owes out of its wallet.
func AdminClawbackResellerDebt(c *gin.Context) {
	var req struct {
		ResellerOrgId int `json:"reseller_org_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	payout, err := model.ClawbackResellerDebt(req.ResellerOrgId, c.GetInt("id"), common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(req.ResellerOrgId, c.GetInt("id"), "settlement.clawback", fmt.Sprintf("org:%d", req.ResellerOrgId), fmt.Sprintf("quota=%d payout=%d", -payout.Quota, payout.Id))
	common.ApiSuccess(c, payout)
}

// AdminGetFinanceReport — GET /api/admin/reconciliation/finance
func AdminGetFinanceReport(c *gin.Context) {
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetFinanceReport(from, to, common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// StartFinanceDailyJobs keeps the finance books on the master node: once a
// day it writes the balance snapshot and closes the months that are due.
// Both are safe to repeat, so it simply runs every hour.
func StartFinanceDailyJobs() {
	if !common.IsMasterNode {
		return
	}
	time.Sleep(2 * time.Minute)
	for {
		func() {
			defer customRecoverJob("finance daily jobs")
			now := common.GetTimestamp()
			if err := model.RecordFinanceBalanceSnapshot(now); err != nil {
				common.SysLog(common.LogText("finance balance snapshot failed: %s", err.Error()))
			}
			if err := model.CloseDueSettlementPeriods(now); err != nil {
				common.SysLog(common.LogText("settlement close failed: %s", err.Error()))
			}
		}()
		time.Sleep(time.Hour)
	}
}
