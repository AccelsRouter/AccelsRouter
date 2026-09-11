// Fork-only organization console: an org owner/admin manages their OWN org's
// members, customers, workspaces, ledger, BYOK channels, and allocations.
// Every handler resolves the caller's org from their own OrgAccount and
// refuses to touch anything outside it — the org id is never taken from the
// request body/path, closing cross-org access.
package controller

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// callerOrg resolves the caller's org and admin rights. Returns (org, isAdmin,
// ok); when !ok it has already written the error response.
func callerOrg(c *gin.Context) (*model.Organization, bool, bool) {
	acc, err := model.GetOrgAccountByUser(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return nil, false, false
	}
	if acc == nil || (acc.Role != model.OrgRoleOwner && acc.Role != model.OrgRoleAdmin) {
		common.ApiErrorMsg(c, "无组织管理权限")
		return nil, false, false
	}
	// A suspended admin/owner account keeps no management authority: suspension
	// must be a reliable containment lever for a compromised or rogue admin.
	if acc.Status == model.OrgStatusSuspended {
		common.ApiErrorMsg(c, "账号已被暂停")
		return nil, false, false
	}
	org, err := model.GetOrganizationById(acc.OrgId)
	if err != nil || org == nil {
		common.ApiErrorMsg(c, "organization not found")
		return nil, false, false
	}
	if org.Status == model.OrgStatusSuspended {
		common.ApiErrorMsg(c, "组织已被暂停")
		return nil, false, false
	}
	return org, acc.Role == model.OrgRoleOwner, true
}

// GetMyOrganization — GET /api/organization/self
func GetMyOrganization(c *gin.Context) {
	org, isOwner, ok := callerOrg(c)
	if !ok {
		return
	}
	common.ApiSuccess(c, gin.H{
		"id":           org.Id,
		"name":         org.Name,
		"type":         org.Type,
		"status":       org.Status,
		"wallet_quota": org.WalletQuota,
		"price_group":  org.PriceGroup,
		"is_owner":     isOwner,
	})
}

// ListMyOrgAccounts — GET /api/organization/accounts
func ListMyOrgAccounts(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	accounts, err := model.ListOrgAccounts(org.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, accounts)
}

type updateAccountRequest struct {
	MonthlyBudget *int    `json:"monthly_budget"`
	Status        *string `json:"status"`
	Role          *string `json:"role"`
}

// UpdateMyOrgAccount — PUT /api/organization/accounts/:user_id
func UpdateMyOrgAccount(c *gin.Context) {
	org, isOwner, ok := callerOrg(c)
	if !ok {
		return
	}
	userId, _ := strconv.Atoi(c.Param("user_id"))
	target, err := model.GetOrgAccountByUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if target == nil || target.OrgId != org.Id {
		common.ApiErrorMsg(c, "account not in your organization")
		return
	}
	if userId == c.GetInt("id") {
		common.ApiErrorMsg(c, "不能修改自己的账号状态")
		return
	}
	if target.Role == model.OrgRoleOwner && !isOwner {
		common.ApiErrorMsg(c, "只有 owner 可以修改 owner 账号")
		return
	}
	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	fields := map[string]interface{}{}
	if req.MonthlyBudget != nil {
		if *req.MonthlyBudget < 0 {
			common.ApiErrorMsg(c, "budget cannot be negative")
			return
		}
		fields["monthly_budget"] = *req.MonthlyBudget
	}
	if req.Status != nil {
		if *req.Status != model.OrgStatusActive && *req.Status != model.OrgStatusSuspended {
			common.ApiErrorMsg(c, "invalid status")
			return
		}
		fields["status"] = *req.Status
	}
	if req.Role != nil {
		if !isOwner {
			common.ApiErrorMsg(c, "只有 owner 可以调整角色")
			return
		}
		if *req.Role != model.OrgRoleAdmin && *req.Role != model.OrgRoleMember {
			common.ApiErrorMsg(c, "invalid role")
			return
		}
		fields["role"] = *req.Role
	}
	if len(fields) == 0 {
		common.ApiSuccess(c, nil)
		return
	}
	if err := model.UpdateOrgAccountFields(org.Id, userId, fields); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "account.update", fmt.Sprintf("user:%d", userId), fmt.Sprintf("%v", fields))
	common.ApiSuccess(c, nil)
}

// DetachMyOrgAccount — DELETE /api/organization/accounts/:user_id
func DetachMyOrgAccount(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	userId, _ := strconv.Atoi(c.Param("user_id"))
	target, err := model.GetOrgAccountByUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if target == nil || target.OrgId != org.Id {
		common.ApiErrorMsg(c, "account not in your organization")
		return
	}
	if target.Role == model.OrgRoleOwner {
		common.ApiErrorMsg(c, "不能移除组织所有者")
		return
	}
	if err := model.DetachOrgAccount(org.Id, userId); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "account.detach", fmt.Sprintf("user:%d", userId), "")
	common.ApiSuccess(c, nil)
}

// ListMyOrgLedger — GET /api/organization/ledger
// GetMyOrgLogs — GET /api/organization/logs
// The org's own individual call records (consume logs), paginated — so a
// reseller customer (and any org admin) can see every call under its org, not
// just its own key.
func GetMyOrgLogs(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	logs, total, err := model.ListOrgLogs(org.Id, from, to, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(logs)
	common.ApiSuccess(c, page)
}

func ListMyOrgLedger(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgLedger(org.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

type allocateRequest struct {
	ToOrgId int    `json:"to_org_id"`
	Quota   int    `json:"quota"`
	Remark  string `json:"remark"`
}

// AllocateFromMyOrg — POST /api/organization/allocate
// Reseller allocates credit to a nested customer org from its own wallet.
func AllocateFromMyOrg(c *gin.Context) {
	org, ok := callerReseller(c)
	if !ok {
		return
	}
	var req allocateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Quota <= 0 {
		common.ApiErrorMsg(c, "quota must be positive")
		return
	}
	// A reseller may only fund its own linked customers.
	if isCustomer, err := model.IsResellerCustomer(org.Id, req.ToOrgId); err != nil {
		common.ApiError(c, err)
		return
	} else if !isCustomer {
		common.ApiErrorMsg(c, "该组织不是你的客户")
		return
	}
	if r := []rune(req.Remark); len(r) > 255 {
		req.Remark = string(r[:255])
	}
	if err := model.TransferOrgCredit(org.Id, req.ToOrgId, req.Quota, c.GetInt("id"), model.LedgerTypeAllocate, req.Remark); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "credit.allocate", fmt.Sprintf("org:%d", req.ToOrgId), fmt.Sprintf("quota=%d", req.Quota))
	common.ApiSuccess(c, nil)
}

// RevokeFromMyOrg — POST /api/organization/revoke
// Reseller pulls back UNCONSUMED credit from a customer org, bounded by its
// net allocation to that org (no parent link ⇒ can't over-reclaim).
func RevokeFromMyOrg(c *gin.Context) {
	org, ok := callerReseller(c)
	if !ok {
		return
	}
	var req allocateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Quota <= 0 {
		common.ApiErrorMsg(c, "quota must be positive")
		return
	}
	if isCustomer, err := model.IsResellerCustomer(org.Id, req.ToOrgId); err != nil {
		common.ApiError(c, err)
		return
	} else if !isCustomer {
		common.ApiErrorMsg(c, "该组织不是你的客户")
		return
	}
	net, err := model.NetAllocatedBetween(org.Id, req.ToOrgId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Quota > net {
		common.ApiErrorMsg(c, "回收额度不能超过对该组织的净划拨额")
		return
	}
	// from = customer, to = reseller; the customer's own unconsumed balance
	// still bounds the actual pull (TransferOrgCredit's conditional deduct).
	if r := []rune(req.Remark); len(r) > 255 {
		req.Remark = string(r[:255])
	}
	if err := model.TransferOrgCredit(req.ToOrgId, org.Id, req.Quota, c.GetInt("id"), model.LedgerTypeRevoke, req.Remark); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "credit.revoke", fmt.Sprintf("org:%d", req.ToOrgId), fmt.Sprintf("quota=%d", req.Quota))
	common.ApiSuccess(c, nil)
}

// ListMyOrgAudit — GET /api/organization/audit
func ListMyOrgAudit(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgAuditLogs(org.Id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}
