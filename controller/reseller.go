// Fork-only reseller (distributor) console: a reseller org provisions and
// funds downstream customer orgs and views their balance/usage. Builds on the
// existing allocate/revoke primitives; every handler resolves the caller's own
// reseller org and refuses to touch anything it has no ledger relationship
// with.
package controller

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
		common.ApiErrorMsg(c, "仅分销商组织可访问")
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
	// A reseller customer always uses the default price group: its pricing is
	// driven by the reseller's retail discounts, not a per-group base rate. The
	// client field is ignored so it can never be assigned a divergent group.
	customer, err := model.CreateResellerCustomer(reseller.Id, strings.TrimSpace(req.Name), "default", req.InitialQuota, c.GetInt("id"))
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
	// Overlay the reseller's retail discount so the statement shows what the
	// customer owes (standard × per-model-series ratio). Reporting only.
	if customer, err := model.GetOrganizationById(customerId); err == nil && customer != nil {
		report.ApplyRetailDiscounts(model.ParseRetailDiscounts(customer.RetailDiscounts))
	}
	common.ApiSuccess(c, report)
}

// GetMyResellerUsage — GET /api/organization/reseller/usage
// The reseller's aggregated usage across ALL its own customers (per-customer in
// ByWorkspace, merged by model/member), with each customer's retail overlay.
// callerReseller scopes it to the caller's own reseller org.
func GetMyResellerUsage(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetResellerUsage(reseller.Id, from, to)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// ListMyResellerLogs — GET /api/organization/reseller/logs
// The reseller's aggregated call records across all its customers, or scoped to
// one customer when customer_id is a valid customer of it.
func ListMyResellerLogs(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	customerId, _ := strconv.Atoi(c.Query("customer_id"))
	var logs []*model.Log
	var total int64
	var err error
	if customerId > 0 {
		if isCust, _ := model.IsResellerCustomer(reseller.Id, customerId); !isCust {
			common.ApiErrorMsg(c, "该组织不是你的客户")
			return
		}
		logs, total, err = model.ListOrgLogs(customerId, from, to, page.GetStartIdx(), page.GetPageSize())
	} else {
		logs, total, err = model.ListResellerLogs(reseller.Id, from, to, page.GetStartIdx(), page.GetPageSize())
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(logs)
	common.ApiSuccess(c, page)
}

// ExportMyResellerLogs — GET /api/organization/reseller/logs/export (CSV)
func ExportMyResellerLogs(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	customerId, _ := strconv.Atoi(c.Query("customer_id"))
	writeOrgLogsCSV(c, reseller.Id, customerId, reseller.Name, from, to)
}

// GetMyCustomerPricing — GET /api/reseller/customers/:id/pricing
func GetMyCustomerPricing(c *gin.Context) {
	_, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	customer, err := model.GetOrganizationById(customerId)
	if err != nil || customer == nil {
		common.ApiErrorMsg(c, "客户组织不存在")
		return
	}
	common.ApiSuccess(c, gin.H{"discounts": model.ParseRetailDiscounts(customer.RetailDiscounts)})
}

// SetMyCustomerPricing — PUT /api/reseller/customers/:id/pricing
// Set the customer's per-model-series retail discount (reporting overlay; the
// platform still bills the customer at standard price). Ratios in (0,1].
func SetMyCustomerPricing(c *gin.Context) {
	reseller, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	var req struct {
		Discounts map[string]float64 `json:"discounts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	// A customer's per-model retail ratio must be at least the reseller's own
	// per-model wholesale ratio for that model (the reseller must not resell below
	// its cost — equal is allowed = zero margin), have at most two decimals, and
	// stay within (0,1]. The floor is resolved per model (exact name beats prefix).
	wholesale := model.ParseRetailDiscounts(reseller.WholesaleRatios)
	for token, ratio := range req.Discounts {
		if ratio <= 0 || ratio > 1 {
			common.ApiErrorMsg(c, "折扣比例必须在 (0,1] 之间: "+token)
			return
		}
		if !ratioAtMost2Decimals(ratio) {
			common.ApiErrorMsg(c, "折扣最多保留两位小数: "+token)
			return
		}
		floor := model.WholesaleRatioFor(token, wholesale)
		if ratio < floor {
			common.ApiErrorMsg(c, fmt.Sprintf("客户折扣不能低于该模型的批发折 %.2f: %s", floor, token))
			return
		}
	}
	stored, err := model.MarshalRetailDiscounts(req.Discounts)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOrganizationFields(customerId, map[string]interface{}{"retail_discounts": stored}); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "customer.pricing", fmt.Sprintf("org:%d", customerId), fmt.Sprintf("count=%d", len(req.Discounts)))
	common.ApiSuccess(c, nil)
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
	reseller, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	invId, _ := strconv.Atoi(c.Param("inv_id"))
	if err := model.RevokeOrgInvitation(customerId, invId); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "customer.invite.revoke", fmt.Sprintf("org:%d", customerId), fmt.Sprintf("inv:%d", invId))
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

// GetMyOrgContext — GET /api/organization/context
// One lightweight call the UI uses to decide what to show: whether the caller
// is a reseller admin, a reseller-provisioned customer, or a plain enterprise
// member. Drives the scoped "customer console" sidebar.
func GetMyOrgContext(c *gin.Context) {
	userId := c.GetInt("id")

	isResellerAdmin := false
	if org, err := model.GetResellerAdminOrg(userId); err == nil && org != nil {
		isResellerAdmin = true
	}

	isOrgMember := false
	isResellerCustomer := false
	orgType := ""
	brandName := ""
	brandLogo := ""
	if acc, err := model.GetOrgAccountByUser(userId); err == nil && acc != nil {
		isOrgMember = true
		if org, err := model.GetOrganizationById(acc.OrgId); err == nil && org != nil {
			orgType = org.Type
			isResellerCustomer = model.IsCustomerOrg(org.Id)
			// White-label: a customer sees its reseller's brand in place of the
			// platform brand.
			if isResellerCustomer {
				if resellerId, ok := model.ResellerOrgIdForCustomer(org.Id); ok {
					if reseller, err := model.GetOrganizationById(resellerId); err == nil && reseller != nil {
						brandName = reseller.BrandName
						brandLogo = reseller.BrandLogo
					}
				}
			}
		}
	}

	common.ApiSuccess(c, gin.H{
		"is_org_member":        isOrgMember,
		"is_reseller_admin":    isResellerAdmin,
		"is_reseller_customer": isResellerCustomer,
		"org_type":             orgType,
		"brand_name":           brandName,
		"brand_logo":           brandLogo,
	})
}

// GetMyCustomerLogs — GET /api/reseller/customers/:id/logs
// Individual call records (consume logs) for one of the reseller's customers,
// paginated, newest first.
// ExportMyCustomerLogs — GET /api/reseller/customers/:id/logs/export (CSV)
func ExportMyCustomerLogs(c *gin.Context) {
	_, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	name := ""
	if org, err := model.GetOrganizationById(customerId); err == nil && org != nil {
		name = org.Name
	}
	writeOrgLogsCSV(c, customerId, 0, name, from, to)
}

func GetMyCustomerLogs(c *gin.Context) {
	_, customerId, ok := callerResellerCustomer(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	logs, total, err := model.ListOrgLogs(customerId, from, to, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Cross-org privacy: don't expose the customer's end-user client IPs to the
	// reseller (the customer sees its own IPs via GET /organization/logs).
	for _, l := range logs {
		l.Ip = ""
	}
	page.SetTotal(int(total))
	page.SetItems(logs)
	common.ApiSuccess(c, page)
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
		"id":               reseller.Id,
		"name":             reseller.Name,
		"type":             reseller.Type,
		"status":           reseller.Status,
		"wallet_quota":     reseller.WalletQuota,
		"price_group":      reseller.PriceGroup,
		"wholesale_ratio":  reseller.WholesaleRatio,
		"wholesale_ratios": model.ParseRetailDiscounts(reseller.WholesaleRatios),
		"is_owner":         true,
	})
}

// ratioAtMost2Decimals reports whether a discount ratio has at most two decimal
// places (e.g. 0.85 ok, 0.855 not), tolerating float representation noise.
func ratioAtMost2Decimals(r float64) bool {
	scaled := r * 100
	return math.Abs(scaled-math.Round(scaled)) < 1e-6
}

// GetMyResellerAudit — GET /api/reseller/audit
// The reseller's own audit trail: every operation its admins performed on its
// customers (create / allocate / revoke / pricing / models / invite / brand …),
// recorded under the reseller org id. Read-only, paginated, newest first.
func GetMyResellerAudit(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgAuditLogs(reseller.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// GetMyResellerBrand — GET /api/reseller/brand
// The reseller's white-label brand shown to its downstream customers.
func GetMyResellerBrand(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	common.ApiSuccess(c, gin.H{
		"brand_name": reseller.BrandName,
		"brand_logo": reseller.BrandLogo,
	})
}

// SetMyResellerBrand — PUT /api/reseller/brand
// Self-service: the reseller sets the name+logo its customers see in place of
// the platform brand. Empty values clear the override (fall back to platform).
func SetMyResellerBrand(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	var req struct {
		BrandName string `json:"brand_name"`
		BrandLogo string `json:"brand_logo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	name := strings.TrimSpace(req.BrandName)
	logo := strings.TrimSpace(req.BrandLogo)
	if len([]rune(name)) > 64 {
		common.ApiErrorMsg(c, "品牌名称过长（最多 64 字）")
		return
	}
	// Logo is rendered as an <img src>; only allow web or inline-image sources
	// so a customer page can't be pointed at an arbitrary scheme.
	if logo != "" {
		if len(logo) > 8192 {
			common.ApiErrorMsg(c, "品牌 Logo 地址过长")
			return
		}
		if !strings.HasPrefix(logo, "https://") &&
			!strings.HasPrefix(logo, "http://") &&
			!strings.HasPrefix(logo, "data:image/") {
			common.ApiErrorMsg(c, "品牌 Logo 必须是 http(s) 链接或图片数据")
			return
		}
	}
	if err := model.UpdateOrganizationFields(reseller.Id, map[string]interface{}{
		"brand_name": name,
		"brand_logo": logo,
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "reseller.brand", fmt.Sprintf("org:%d", reseller.Id), name)
	common.ApiSuccess(c, gin.H{"brand_name": name, "brand_logo": logo})
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
	// Route-2: the reseller tops up its cost balance 1:1 (personal quota spent =
	// credit bought). The wholesale discount is no longer realized at top-up — it
	// is applied per call, per model, against the reseller wallet (see
	// service/org_funding.go). ratio-at-purchase is gone.
	cost := req.Quota
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
