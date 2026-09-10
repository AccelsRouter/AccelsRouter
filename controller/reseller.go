// Fork-only reseller (distributor) console: a reseller org provisions and
// funds downstream customer orgs and views their balance/usage. Builds on the
// existing allocate/revoke primitives; every handler resolves the caller's own
// reseller org and refuses to touch anything it has no ledger relationship
// with.
package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// callerReseller resolves the caller's reseller org via the reseller-admin
// link (decoupled from the single-payer OrgAccount, so a reseller admin may
// also be an enterprise member). Returns false with an error already written
// when the caller is not a reseller admin or the org is suspended.
func callerReseller(c *gin.Context) (*model.Organization, bool) {
	org, err := model.GetResellerAdminOrg(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if org == nil {
		common.ApiErrorMsg(c, "仅代理商组织可访问")
		return nil, false
	}
	if org.Status == model.OrgStatusSuspended {
		common.ApiErrorMsg(c, "组织已被暂停")
		return nil, false
	}
	return org, true
}

type createCustomerRequest struct {
	Name         string `json:"name"`
	PriceGroup   string `json:"price_group"`
	InitialQuota int    `json:"initial_quota"`
}

// CreateMyCustomer — POST /api/organization/customers
func CreateMyCustomer(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	var req createCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.ValidateOrgName(strings.TrimSpace(req.Name)); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// The retail price group must be a configured group (or the default), so a
	// customer can't be assigned a non-existent group with undefined pricing.
	priceGroup := strings.TrimSpace(req.PriceGroup)
	if priceGroup != "" && priceGroup != "default" && !ratio_setting.ContainsGroupRatio(priceGroup) {
		common.ApiErrorMsg(c, "价格组不存在")
		return
	}
	customer, err := model.CreateResellerCustomer(reseller.Id, strings.TrimSpace(req.Name), priceGroup, req.InitialQuota, c.GetInt("id"))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "customer.create", fmt.Sprintf("org:%d", customer.Id), fmt.Sprintf("%s quota=%d", customer.Name, req.InitialQuota))
	common.ApiSuccess(c, customer)
}

// ListMyCustomers — GET /api/organization/customers
func ListMyCustomers(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	customers, err := model.ListResellerCustomers(reseller.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, customers)
}

// GetMyCustomerUsage — GET /api/organization/customers/:id/usage
// A reseller may view a customer's usage only if it is actually its customer
// (has ever allocated to it).
func GetMyCustomerUsage(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	customerId, _ := strconv.Atoi(c.Param("id"))
	isCustomer, err := model.IsResellerCustomer(reseller.Id, customerId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !isCustomer {
		common.ApiErrorMsg(c, "该组织不是你的客户")
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetOrgUsage(customerId, from, to)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// callerResellerCustomer resolves the caller's reseller org and the customer id
// in the :id path param, and authorizes that the org is actually this
// reseller's customer. Writes the error and returns ok=false otherwise.
func callerResellerCustomer(c *gin.Context) (reseller *model.Organization, customerId int, ok bool) {
	reseller, ok = callerReseller(c)
	if !ok {
		return nil, 0, false
	}
	customerId, _ = strconv.Atoi(c.Param("id"))
	isCustomer, err := model.IsResellerCustomer(reseller.Id, customerId)
	if err != nil {
		common.ApiError(c, err)
		return nil, 0, false
	}
	if !isCustomer {
		common.ApiErrorMsg(c, "该组织不是你的客户")
		return nil, 0, false
	}
	return reseller, customerId, true
}

// InviteMyCustomerOwner — POST /api/reseller/customers/:id/invitations
// Deliver a provisioned (ownerless) customer to its operator: invite an email
// to take over the customer org as its admin. The invitee accepts via the
// standard /organization/join?code=... flow (email-scoped, consent-gated).
func InviteMyCustomerOwner(c *gin.Context) {
	reseller, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	// Role must be admin (owner is not invitable); relation customer marks this
	// as a reseller-provisioned managed org.
	inv := &model.OrgInvitation{
		OrgId:        customerId,
		Relation:     model.OrgRelationCustomer,
		Role:         model.OrgRoleAdmin,
		InvitedEmail: strings.TrimSpace(req.Email),
		CreatedBy:    c.GetInt("id"),
	}
	if err := model.CreateOrgInvitation(inv); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	orgName := fmt.Sprintf("#%d", customerId)
	if customer, err := model.GetOrganizationById(customerId); err == nil && customer != nil {
		orgName = customer.Name
	}
	emailed := sendOrgInvitationEmail(inv.InvitedEmail, orgName, inv.Code)
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "customer.invite", fmt.Sprintf("org:%d", customerId), inv.InvitedEmail)
	common.ApiSuccess(c, gin.H{"code": inv.Code, "invited_email": inv.InvitedEmail, "expires_at": inv.ExpiresAt, "emailed": emailed})
}

// ListMyCustomerInvitations — GET /api/reseller/customers/:id/invitations
func ListMyCustomerInvitations(c *gin.Context) {
	_, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	rows, err := model.ListOrgInvitations(customerId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// RevokeMyCustomerInvitation — DELETE /api/reseller/customers/:id/invitations/:inv_id
func RevokeMyCustomerInvitation(c *gin.Context) {
	_, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	invId, _ := strconv.Atoi(c.Param("inv_id"))
	if err := model.RevokeOrgInvitation(customerId, invId); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

// resellerOfferableModels is the set of model names a reseller may assign to a
// customer on the given group: the group's routable models, further narrowed by
// the reseller's own admin-configured offerable set (empty = unrestricted).
// Only model NAMES are returned — never any channel/upstream information.
func resellerOfferableModels(reseller *model.Organization, group string) []string {
	if group == "" {
		group = "default"
	}
	groupModels := model.GetGroupEnabledModels(group)
	offer := reseller.AllowedModelSet()
	if offer == nil {
		return groupModels
	}
	out := make([]string, 0, len(groupModels))
	for _, m := range groupModels {
		if offer[m] {
			out = append(out, m)
		}
	}
	return out
}

// GetMyCustomerModels — GET /api/reseller/customers/:id/models
// Returns the customer's current allow-list and the catalog the reseller may
// assign from (never any channel info).
func GetMyCustomerModels(c *gin.Context) {
	reseller, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	customer, err := model.GetOrganizationById(customerId)
	if err != nil || customer == nil {
		common.ApiErrorMsg(c, "客户组织不存在")
		return
	}
	common.ApiSuccess(c, gin.H{
		"allowed": customer.AllowedModelList(),
		"catalog": resellerOfferableModels(reseller, customer.PriceGroup),
	})
}

// SetMyCustomerModels — PUT /api/reseller/customers/:id/models
// Assign which models the customer may use. Each must be within the reseller's
// offerable catalog. An empty list means unrestricted (the group default).
func SetMyCustomerModels(c *gin.Context) {
	reseller, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	var req struct {
		Models []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	customer, err := model.GetOrganizationById(customerId)
	if err != nil || customer == nil {
		common.ApiErrorMsg(c, "客户组织不存在")
		return
	}
	catalog := resellerOfferableModels(reseller, customer.PriceGroup)
	allowed := make(map[string]bool, len(catalog))
	for _, m := range catalog {
		allowed[m] = true
	}
	for _, m := range req.Models {
		if !allowed[strings.TrimSpace(m)] {
			common.ApiErrorMsg(c, "模型不在可分配范围内: "+m)
			return
		}
	}
	stored, err := model.MarshalAllowedModels(req.Models)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SetOrgAllowedModels(customerId, stored); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "customer.models", fmt.Sprintf("org:%d", customerId), fmt.Sprintf("count=%d", len(req.Models)))
	common.ApiSuccess(c, nil)
}

// GetMyResellerOrg — GET /api/organization/reseller/self
// Reseller-scoped org view (wallet, price group). Resolves via the reseller-
// admin link, so it works for a reseller admin who is not an OrgAccount member.
func GetMyResellerOrg(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	common.ApiSuccess(c, gin.H{
		"id":           reseller.Id,
		"name":         reseller.Name,
		"type":         reseller.Type,
		"status":       reseller.Status,
		"wallet_quota": reseller.WalletQuota,
		"price_group":  reseller.PriceGroup,
		"is_owner":     true,
	})
}

// maxResellerPurchaseQuota bounds a single self-service credit purchase, so a
// mistyped amount can't request an absurd wallet movement (the user's personal
// balance already caps what actually clears).
const maxResellerPurchaseQuota = 100_000_000

// GetMyResellerWallet — GET /api/reseller/wallet
// Returns the reseller's wallet, its wholesale ratio, and the caller's personal
// balance so the top-up dialog can show "cost = credit × ratio".
func GetMyResellerWallet(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	personal, err := model.GetUserQuota(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"wallet_quota":    reseller.WalletQuota,
		"wholesale_ratio": reseller.EffectiveWholesaleRatio(),
		"personal_quota":  personal,
	})
}

// PurchaseMyResellerCredit — POST /api/reseller/wallet/purchase
// Self-service: buy wallet credit at the reseller's wholesale ratio, paid from
// the caller's personal balance. This is what unblocks a $0 reseller wallet.
func PurchaseMyResellerCredit(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	var req struct {
		Quota int `json:"quota"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Quota <= 0 {
		common.ApiErrorMsg(c, "购买额度必须为正")
		return
	}
	if req.Quota > maxResellerPurchaseQuota {
		common.ApiErrorMsg(c, "单次购买额度超过上限")
		return
	}
	// cost (personal quota spent) = credit × wholesale ratio, rounded via the
	// centralized quota rounding helper. ratio ≤ 1 ⇒ cost ≤ credit.
	cost := common.QuotaRound(float64(req.Quota) * reseller.EffectiveWholesaleRatio())
	if cost <= 0 {
		cost = req.Quota
	}
	tradeNo := "rspur-" + common.GetUUID()
	if err := model.PurchaseResellerCredit(reseller.Id, c.GetInt("id"), req.Quota, cost, tradeNo, "reseller wallet purchase"); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "wallet.purchase", fmt.Sprintf("trade:%s", tradeNo), fmt.Sprintf("credit=%d cost=%d", req.Quota, cost))
	// The console refetches the wallet after this call, so return only the
	// purchase result — not an in-memory-derived (possibly stale) balance.
	common.ApiSuccess(c, gin.H{"quota": req.Quota, "cost": cost})
}

// ListMyResellerLedger — GET /api/organization/reseller/ledger
func ListMyResellerLedger(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgLedger(reseller.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}
