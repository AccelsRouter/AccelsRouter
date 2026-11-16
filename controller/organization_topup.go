package controller

import (
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
