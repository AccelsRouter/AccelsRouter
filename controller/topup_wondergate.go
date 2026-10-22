package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

// WonderGatePayRequest is the JSON body the frontend submits to pull up a
// WonderGate (card / local payment) checkout.
type WonderGatePayRequest struct {
	Amount int64 `json:"amount"`
}

// getWonderGatePayMoney converts the user-facing top-up Amount to the USD value
// charged via WonderGate, reusing the global AmountDiscount and topup group
// ratio.
func getWonderGatePayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	return amount * setting.WonderGateUnitPrice * topupGroupRatio * discount
}

// RequestWonderGateAmount returns the USD cost (string, 2 dp) for the requested
// quota amount so the frontend can preview it.
func RequestWonderGateAmount(c *gin.Context) {
	var req WonderGatePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	minTopup := int64(setting.WonderGateMinTopUp)
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopup)})
		return
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getWonderGatePayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

// RequestWonderGatePay creates a WonderGate checkout order and returns the
// hosted payment page URL for the frontend to redirect to.
func RequestWonderGatePay(c *gin.Context) {
	if !isWonderGateTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "WonderGate 支付未启用"})
		return
	}

	var req WonderGatePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	minTopup := int64(setting.WonderGateMinTopUp)
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopup)})
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "用户不存在"})
		return
	}

	group, _ := model.GetUserGroup(id, true)
	payMoney := getWonderGatePayMoney(float64(req.Amount), group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	// WonderGate transactionId must be unique; reuse it as our trade_no so
	// webhook/reconcile line up. Alphanumeric only (no separators).
	tradeNo := fmt.Sprintf("WG%d%d%s", id, time.Now().UnixMilli(), randstr.String(6))

	// Token display mode: normalise Amount to USD-equivalent units so
	// RechargeWonderGate does not double-scale quota during settlement.
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = int64(float64(req.Amount) / common.QuotaPerUnit)
		if amount < 1 {
			amount = 1
		}
	}

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodWonderGate,
		PaymentProvider: model.PaymentProviderWonderGate,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("WonderGate 创建充值订单失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	_, _, appId := setting.WonderGateActiveCredentials()
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	amountStr := strconv.FormatFloat(payMoney, 'f', 2, 64)

	createReq := &service.WonderGateCheckoutRequest{
		TransactionID:   tradeNo,
		Currency:        setting.WonderGateCurrency,
		Amount:          amountStr,
		AppID:           appId,
		TransactionIP:   c.ClientIP(),
		CallbackURL:     paymentReturnPath("/console/topup?show_history=true"),
		NotificationURL: base + "/api/wondergate/webhook",
		ProductInfos: []map[string]interface{}{
			{
				"productName": "Account Top-up",
				"currency":    setting.WonderGateCurrency,
				"price":       amountStr,
				"sku":         "TOPUP",
				"quantity":    1,
				"productLink": base,
			},
		},
		// A balance top-up has no real billing address. Send the user's email
		// (the one field we actually hold) plus placeholders; country is
		// admin-configurable. NOTE: card AVS/fraud checks may require real
		// billing — collect it via a form if declines occur.
		BillingAddress: map[string]interface{}{
			"firstName": wonderGateBillingName(user, 0),
			"lastName":  wonderGateBillingName(user, 1),
			"email":     user.Email,
			"country":   setting.WonderGateBillingCountry,
			"state":     "NA",
			"city":      "NA",
			"address":   "NA",
			"zipCode":   "00000",
			"phone":     "0000000000",
		},
		TransactionMethod: setting.WonderGateTransactionMethod,
	}

	result, err := service.CreateWonderGateCheckout(c.Request.Context(), createReq)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("WonderGate 建单失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		_ = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderWonderGate, common.TopUpStatusFailed)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("WonderGate 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.2f unique_id=%s", id, tradeNo, req.Amount, payMoney, result.UniqueID))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"payment_url": result.RedirectURL,
			"order_id":    tradeNo,
		},
	})
}

// wonderGateBillingName derives a first/last name from the user for the
// billing address (index 0 = first, 1 = last). Falls back to safe placeholders.
func wonderGateBillingName(user *model.User, index int) string {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = strings.TrimSpace(user.Username)
	}
	parts := strings.Fields(name)
	if index < len(parts) {
		return parts[index]
	}
	if index == 0 {
		return "Customer"
	}
	return "User"
}

// WonderGateWebhook is the unauthenticated endpoint WonderGate calls with async
// transaction/refund notifications. Signature is verified against the active
// SecretKey; the gateway retries on non-200 responses.
func WonderGateWebhook(c *gin.Context) {
	if !isWonderGateWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("WonderGate webhook 被拒绝 reason=webhook_disabled client_ip=%s", c.ClientIP()))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	fields, err := service.VerifyWonderGateNotification(body)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("WonderGate webhook 验签失败 client_ip=%s error=%q body=%q", c.ClientIP(), err.Error(), string(body)))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	code, _ := service.WonderGateNotificationInt(fields, "code")
	txnType := service.WonderGateNotificationString(fields, "transactionType")
	transactionId := service.WonderGateNotificationString(fields, "transactionId")

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("WonderGate webhook 收到通知 code=%d type=%s transaction_id=%s client_ip=%s", code, txnType, transactionId, c.ClientIP()))

	// Only Sale-type success credits a top-up. Refund/chargeback notifications
	// are acknowledged but handled out-of-band.
	if transactionId != "" && code == service.WonderGateCodeTransactionSuccess {
		LockOrder(transactionId)
		defer UnlockOrder(transactionId)
		if err := model.RechargeWonderGate(transactionId, c.ClientIP()); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("WonderGate 充值处理失败 trade_no=%s error=%q", transactionId, err.Error()))
			// Return 200 anyway is wrong here — let the gateway retry.
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("WonderGate 充值成功 trade_no=%s", transactionId))
	}

	// Acknowledge receipt (HTTP 200) so the gateway stops retrying.
	c.Status(http.StatusOK)
}

// wonderGateReconcileInterval is how often pending orders are polled.
const wonderGateReconcileInterval = 30 * time.Second

// wonderGateReconcileWindow bounds which pending orders are polled — anything
// older than this is assumed abandoned/expired and left alone.
const wonderGateReconcileWindow = 2 * time.Hour

// StartWonderGateReconcileTask launches a background poller that recovers
// WonderGate top-ups whose webhook never arrived (observed in production: the
// gateway's async notification can fail to reach us entirely, and it only
// retries 4 times within 30 minutes).
// Idempotent: RechargeWonderGate no-ops on already-credited orders, so this
// is safe alongside live webhooks.
func StartWonderGateReconcileTask() {
	go func() {
		ticker := time.NewTicker(wonderGateReconcileInterval)
		defer ticker.Stop()
		for range ticker.C {
			reconcileWonderGateOrders()
		}
	}()
}

func reconcileWonderGateOrders() {
	// Only the master node polls — replicas would just duplicate the work.
	if !common.IsMasterNode {
		return
	}
	if !isWonderGateTopUpEnabled() {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("wondergate reconcile panic: %v", r))
		}
	}()

	since := time.Now().Add(-wonderGateReconcileWindow).Unix()
	pending, err := model.GetPendingTopUpsByProvider(model.PaymentProviderWonderGate, since)
	if err != nil {
		common.SysError("wondergate reconcile: query pending failed: " + err.Error())
		return
	}
	if len(pending) == 0 {
		return
	}

	ctx := context.Background()
	for _, topUp := range pending {
		code, qErr := service.QueryWonderGateOrderCode(ctx, topUp.TradeNo, topUp.CreateTime)
		if qErr != nil {
			// Not yet queryable (user may never have reached the payment
			// page) or a transient gateway error; try again next tick.
			continue
		}
		switch code {
		case service.WonderGateCodeTransactionSuccess:
			LockOrder(topUp.TradeNo)
			if rErr := model.RechargeWonderGate(topUp.TradeNo, "reconcile"); rErr != nil {
				logger.LogError(ctx, fmt.Sprintf("WonderGate 对账补单失败 trade_no=%s error=%q", topUp.TradeNo, rErr.Error()))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("WonderGate 对账补单成功 trade_no=%s", topUp.TradeNo))
			}
			UnlockOrder(topUp.TradeNo)
		case service.WonderGateCodeTransactionDeclined, service.WonderGateCodeOrderClosed:
			if uErr := model.UpdatePendingTopUpStatus(topUp.TradeNo, model.PaymentProviderWonderGate, common.TopUpStatusFailed); uErr != nil &&
				!errors.Is(uErr, model.ErrTopUpNotFound) &&
				!errors.Is(uErr, model.ErrTopUpStatusInvalid) {
				logger.LogError(ctx, fmt.Sprintf("WonderGate 对账标记失败 trade_no=%s error=%q", topUp.TradeNo, uErr.Error()))
			}
		}
		// 102 (pending) and anything else: leave for the next tick / webhook.
	}
}
