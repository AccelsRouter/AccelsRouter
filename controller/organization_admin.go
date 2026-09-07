// Fork-only platform-admin controllers for the organization system: create
// orgs, credit their wallets (invoiced top-up), and attach/detach accounts.
// All routes are AdminAuth-gated in router/api-router.go. The org console
// (org owners/admins managing their own members/customers) lives in
// controller/organization_console.go.
package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// AdminListOrganizations — GET /api/admin/organizations
func AdminListOrganizations(c *gin.Context) {
	page := common.GetPageQuery(c)
	orgs, total, err := model.ListOrganizations(page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(orgs)
	common.ApiSuccess(c, page)
}

type adminCreateOrgRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	PriceGroup string `json:"price_group"`
	OwnerId    int    `json:"owner_user_id"`
	Remark     string `json:"remark"`
}

// AdminCreateOrganization — POST /api/admin/organizations
func AdminCreateOrganization(c *gin.Context) {
	var req adminCreateOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.OwnerId > 0 {
		if u, err := model.GetUserById(req.OwnerId, false); err != nil || u == nil {
			common.ApiErrorMsg(c, "owner user not found")
			return
		}
	}
	org := &model.Organization{
		Name:        req.Name,
		Type:        req.Type,
		PriceGroup:  req.PriceGroup,
		OwnerUserId: req.OwnerId,
		Remark:      req.Remark,
	}
	if err := model.CreateOrganization(org); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "org.create", fmt.Sprintf("org:%d", org.Id), fmt.Sprintf("%s (%s)", org.Name, org.Type))
	common.ApiSuccess(c, org)
}

type adminUpdateOrgRequest struct {
	Name       *string `json:"name"`
	PriceGroup *string `json:"price_group"`
	Status     *string `json:"status"`
	Remark     *string `json:"remark"`
}

// AdminUpdateOrganization — PUT /api/admin/organizations/:id
func AdminUpdateOrganization(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req adminUpdateOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	fields := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if err := model.ValidateOrgName(name); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		fields["name"] = name
	}
	if req.PriceGroup != nil {
		fields["price_group"] = *req.PriceGroup
	}
	if req.Status != nil {
		if *req.Status != model.OrgStatusActive && *req.Status != model.OrgStatusSuspended {
			common.ApiErrorMsg(c, "invalid status")
			return
		}
		fields["status"] = *req.Status
	}
	if req.Remark != nil {
		fields["remark"] = *req.Remark
	}
	if len(fields) == 0 {
		common.ApiSuccess(c, nil)
		return
	}
	if err := model.UpdateOrganizationFields(id, fields); err != nil {
		common.ApiError(c, err)
		return
	}
	// Suspending an org must reflect on the hot path for its accounts at once.
	if req.Status != nil {
		invalidateOrgAccountsCache(id)
	}
	model.RecordOrgAudit(id, c.GetInt("id"), "org.update", fmt.Sprintf("org:%d", id), fmt.Sprintf("%v", fields))
	common.ApiSuccess(c, nil)
}

type adminCreditOrgRequest struct {
	Quota   int    `json:"quota"`
	TradeNo string `json:"trade_no"`
	Remark  string `json:"remark"`
}

// AdminCreditOrganization — POST /api/admin/organizations/:id/credit
// Invoiced top-up: platform credits the org wallet, appends a purchase ledger
// row atomically.
func AdminCreditOrganization(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req adminCreditOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Quota <= 0 {
		common.ApiErrorMsg(c, "quota must be positive")
		return
	}
	if err := model.PlatformCreditOrg(id, req.Quota, c.GetInt("id"), req.TradeNo, req.Remark); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(id, c.GetInt("id"), "org.credit", fmt.Sprintf("org:%d", id), fmt.Sprintf("quota=%d trade=%s", req.Quota, req.TradeNo))
	common.ApiSuccess(c, nil)
}

// AdminListOrgAudit — GET /api/admin/organizations/:id/audit
func AdminListOrgAudit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgAuditLogs(id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// AdminListOrgLedger — GET /api/admin/organizations/:id/ledger
func AdminListOrgLedger(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgLedger(id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

// invalidateOrgAccountsCache drops the payer cache for every account under an
// org (used after org-wide status changes).
func invalidateOrgAccountsCache(orgId int) {
	accounts, err := model.ListOrgAccounts(orgId)
	if err != nil {
		return
	}
	for _, a := range accounts {
		model.InvalidateOrgPayerCache(a.UserId)
	}
}

type adminAttachRequest struct {
	OrgId         int    `json:"org_id"`
	UserId        int    `json:"user_id"`
	Relation      string `json:"relation"`
	Role          string `json:"role"`
	MonthlyBudget int    `json:"monthly_budget"`
	RegisteredBy  string `json:"registered_by"`
}

// AdminAttachOrgAccount — POST /api/admin/organizations/accounts
// Platform-provisioned membership: only a platform admin binds a user to an
// org, so an org operator cannot conscript an arbitrary user into its billing
// (the org console can manage and detach existing accounts, never attach new
// ones). Fails if the user is already managed anywhere (UNIQUE user_id).
func AdminAttachOrgAccount(c *gin.Context) {
	var req adminAttachRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	org, err := model.GetOrganizationById(req.OrgId)
	if err != nil || org == nil {
		common.ApiErrorMsg(c, "organization not found")
		return
	}
	if u, uErr := model.GetUserById(req.UserId, false); uErr != nil || u == nil {
		common.ApiErrorMsg(c, "user not found")
		return
	}
	if existing, _ := model.GetOrgAccountByUser(req.UserId); existing != nil {
		common.ApiErrorMsg(c, "该用户已归属某个组织，请先解绑")
		return
	}
	relation := req.Relation
	if relation == "" {
		if org.Type == model.OrgTypeReseller {
			relation = model.OrgRelationCustomer
		} else {
			relation = model.OrgRelationMember
		}
	}
	role := req.Role
	if role == "" {
		role = model.OrgRoleMember
	}
	if req.MonthlyBudget < 0 {
		common.ApiErrorMsg(c, "budget cannot be negative")
		return
	}
	acc := &model.OrgAccount{
		OrgId: req.OrgId, UserId: req.UserId, Relation: relation, Role: role,
		MonthlyBudget: req.MonthlyBudget, RegisteredBy: req.RegisteredBy,
	}
	if err := model.AttachOrgAccount(acc); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(req.OrgId, c.GetInt("id"), "account.attach", fmt.Sprintf("user:%d", req.UserId), fmt.Sprintf("relation=%s role=%s", relation, role))
	common.ApiSuccess(c, acc)
}

// AdminDetachOrgAccount — DELETE /api/admin/organizations/:id/accounts/:user_id
// Platform remediation path (a wrongly-attached user always has an exit).
func AdminDetachOrgAccount(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	userId, _ := strconv.Atoi(c.Param("user_id"))
	target, err := model.GetOrgAccountByUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if target == nil || target.OrgId != orgId {
		common.ApiErrorMsg(c, "account not in this organization")
		return
	}
	if err := model.DetachOrgAccount(orgId, userId); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(orgId, c.GetInt("id"), "account.detach", fmt.Sprintf("user:%d", userId), "")
	common.ApiSuccess(c, nil)
}

// AdminListResellerAdmins — GET /api/admin/organizations/:id/reseller-admins
func AdminListResellerAdmins(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	admins, err := model.ListResellerAdmins(orgId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, admins)
}

// AdminRevokeResellerAdmin — DELETE /api/admin/organizations/:id/reseller-admins/:user_id
// Per-admin offboarding / containment lever for a reseller org: severs one
// admin's access without suspending the whole org or banning the user.
func AdminRevokeResellerAdmin(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	userId, _ := strconv.Atoi(c.Param("user_id"))
	if err := model.RemoveResellerAdmin(orgId, userId); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(orgId, c.GetInt("id"), "reseller_admin.revoke", fmt.Sprintf("user:%d", userId), "")
	common.ApiSuccess(c, nil)
}
