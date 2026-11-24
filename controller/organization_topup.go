package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ListMyOrgTopUps — GET /api/organization/topups
// The online payments made into the caller's organization wallet, newest
// first, for its owner and admins.
func ListMyOrgTopUps(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	orders, total, err := model.ListOrgTopUps(org.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(orders)
	common.ApiSuccess(c, page)
}

// customerTopUp is what a distributor sees of a customer's payment: how much,
// by what method, when and whether it completed. The order number and the
// paying member stay with the customer.
type customerTopUp struct {
	Amount        int64   `json:"amount"`
	Money         float64 `json:"money"`
	PaymentMethod string  `json:"payment_method"`
	Status        string  `json:"status"`
	CreateTime    int64   `json:"create_time"`
	CompleteTime  int64   `json:"complete_time"`
}

// ListMyCustomerTopUps — GET /api/reseller/customers/:id/topups
// The online payments one of the distributor's customers made into its own
// wallet, newest first.
func ListMyCustomerTopUps(c *gin.Context) {
	_, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	orders, total, err := model.ListOrgTopUps(customerId, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rows := make([]customerTopUp, 0, len(orders))
	for _, order := range orders {
		rows = append(rows, customerTopUp{
			Amount:        order.Amount,
			Money:         order.Money,
			PaymentMethod: order.PaymentMethod,
			Status:        order.Status,
			CreateTime:    order.CreateTime,
			CompleteTime:  order.CompleteTime,
		})
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// LeaveMyReseller — POST /api/organization/reseller/leave
// The owner of a customer that tops up its own wallet takes the organization
// out from under its distributor: it keeps its balance and keys and becomes
// the platform's own client at list price.
func LeaveMyReseller(c *gin.Context) {
	org, isOwner, ok := callerOrg(c)
	if !ok {
		return
	}
	if !isOwner {
		common.ApiErrorT(c, "Only the organization owner can do this")
		return
	}
	resellerOrgId, err := model.LeaveReseller(org.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(resellerOrgId, c.GetInt("id"), "customer.left", fmt.Sprintf("org:%d", org.Id), "")
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "reseller.leave", fmt.Sprintf("org:%d", resellerOrgId), "")
	model.NotifyOrgAdmins(resellerOrgId, 0, orgNotice("Organization {{org}} left and is no longer your customer", org, 0))
	common.ApiSuccess(c, nil)
}

// AdminRegisterOrgChargeback — POST /api/admin/topup-orders/chargeback
// {trade_no}. Registers that a payment channel took back a completed payment
// into an organization's wallet (see model.ReverseOrgTopUpOrder).
func AdminRegisterOrgChargeback(c *gin.Context) {
	var req struct {
		TradeNo string `json:"trade_no"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	tradeNo := strings.TrimSpace(req.TradeNo)
	if tradeNo == "" {
		common.ApiErrorT(c, "Enter the order number of the reversed payment")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	orgId, shortfall, err := model.ReverseOrgTopUpOrder(tradeNo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(orgId, c.GetInt("id"), "topup.chargeback", fmt.Sprintf("org:%d", orgId), fmt.Sprintf("trade=%s spent=%d", tradeNo, shortfall))
	if org, err := model.GetOrganizationById(orgId); err == nil && org != nil {
		model.NotifyOrgAdmins(orgId, c.GetInt("id"), orgNotice("A payment into the wallet of organization {{org}} was reversed by the payment channel, order {{order}}", org, 0, map[string]any{"order": tradeNo}))
	}
	common.ApiSuccess(c, gin.H{"org_id": orgId, "spent": shortfall})
}
