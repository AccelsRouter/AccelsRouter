// Fork-only self-service org onboarding controllers: users apply to open their
// own org (auto-approved per type policy, otherwise queued for admin review),
// and orgs invite users who accept (consent-gated attach).
package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// sendOrgInvitationEmail best-effort emails an org join link to the invitee.
// Returns whether an email was actually sent: false (no error surfaced) when
// SMTP is not configured or sending fails, so callers always fall back to
// returning the join code/link for out-of-band delivery.
func sendOrgInvitationEmail(invitedEmail, orgName, code string) bool {
	if strings.TrimSpace(invitedEmail) == "" {
		return false
	}
	if common.SMTPServer == "" && common.SMTPAccount == "" {
		return false
	}
	link := fmt.Sprintf("%s/organization/join?code=%s", system_setting.ServerAddress, code)
	subject := fmt.Sprintf("邀请加入组织 %s", orgName)
	content := fmt.Sprintf(
		"<p>您被邀请加入组织 <b>%s</b>。</p>"+
			"<p>请点击 <a href='%s'>此处</a> 接受邀请（需使用受邀邮箱 %s 对应的账号登录）。</p>"+
			"<p>如果链接无法点击，请复制到浏览器打开：<br/>%s</p>",
		orgName, link, invitedEmail, link)
	if err := common.SendEmail(subject, invitedEmail, content); err != nil {
		common.SysLog("org invitation email failed: " + err.Error())
		return false
	}
	return true
}

type applyOrgRequest struct {
	Type    string `json:"type"`
	OrgName string `json:"org_name"`
	Contact string `json:"contact"`
	Remark  string `json:"remark"`
}

// ApplyForOrganization — POST /api/organization/apply
// Any authenticated user applies to open an org for THEMSELVES (they become
// owner). No effect on other users. Auto-approved types skip the queue.
func ApplyForOrganization(c *gin.Context) {
	var req applyOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	userId := c.GetInt("id")
	app := &model.OrgApplication{
		UserId:  userId,
		Type:    req.Type,
		OrgName: strings.TrimSpace(req.OrgName),
		Contact: strings.TrimSpace(req.Contact),
		Remark:  strings.TrimSpace(req.Remark),
	}
	if err := model.CreateOrgApplication(app); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// Decoupled per-type policy: auto-approve when enabled for this type.
	if model.OrgTypeAutoApproves(app.Type) {
		org, err := model.ApproveOrgApplication(app.Id, 0, "", "auto-approved")
		if err != nil {
			// Application stays pending for manual review; not a hard failure.
			common.ApiSuccess(c, gin.H{"status": model.OrgApplicationPending, "auto_approved": false})
			return
		}
		common.ApiSuccess(c, gin.H{"status": model.OrgApplicationApproved, "auto_approved": true, "org_id": org.Id})
		return
	}
	common.ApiSuccess(c, gin.H{"status": model.OrgApplicationPending, "auto_approved": false})
}

// GetMyOrgApplication — GET /api/organization/apply/self
func GetMyOrgApplication(c *gin.Context) {
	app, err := model.GetLatestOrgApplicationByUser(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, app) // null when none
}

// ---- admin review (decoupled by type via ?type=) ----

// AdminListOrgApplications — GET /api/admin/organizations/applications?status=&type=
func AdminListOrgApplications(c *gin.Context) {
	page := common.GetPageQuery(c)
	rows, total, err := model.ListOrgApplications(c.Query("status"), c.Query("type"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

type reviewApplicationRequest struct {
	PriceGroup     string   `json:"price_group"`
	WholesaleRatio *float64 `json:"wholesale_ratio"`
	Note           string   `json:"note"`
}

// AdminApproveOrgApplication — POST /api/admin/organizations/applications/:id/approve
func AdminApproveOrgApplication(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req reviewApplicationRequest
	_ = c.ShouldBindJSON(&req)
	org, err := model.ApproveOrgApplication(id, c.GetInt("id"), req.PriceGroup, req.Note)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// Set the reseller's wholesale ratio at approval time (optional; bounded to
	// (0,1], 0 = no discount). Applied to the freshly created org.
	if req.WholesaleRatio != nil && org != nil {
		if *req.WholesaleRatio < 0 || *req.WholesaleRatio > 1 {
			common.ApiErrorMsg(c, "wholesale_ratio must be within (0, 1]")
			return
		}
		if err := model.UpdateOrganizationFields(org.Id, map[string]interface{}{"wholesale_ratio": *req.WholesaleRatio}); err != nil {
			common.ApiError(c, err)
			return
		}
		org.WholesaleRatio = *req.WholesaleRatio
	}
	common.ApiSuccess(c, org)
}

// AdminRejectOrgApplication — POST /api/admin/organizations/applications/:id/reject
func AdminRejectOrgApplication(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req reviewApplicationRequest
	_ = c.ShouldBindJSON(&req)
	if err := model.RejectOrgApplication(id, c.GetInt("id"), req.Note); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- invitations (org console: create/list/revoke) ----

type inviteRequest struct {
	Relation      string `json:"relation"`
	Role          string `json:"role"`
	MonthlyBudget int    `json:"monthly_budget"`
	InvitedEmail  string `json:"invited_email"`
}

// CreateMyOrgInvitation — POST /api/organization/invitations
func CreateMyOrgInvitation(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	var req inviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Relation == "" {
		if org.Type == model.OrgTypeReseller {
			req.Relation = model.OrgRelationCustomer
		} else {
			req.Relation = model.OrgRelationMember
		}
	}
	if req.Role == "" {
		req.Role = model.OrgRoleMember
	}
	inv := &model.OrgInvitation{
		OrgId: org.Id, Relation: req.Relation, Role: req.Role,
		MonthlyBudget: req.MonthlyBudget, InvitedEmail: strings.TrimSpace(req.InvitedEmail),
		CreatedBy: c.GetInt("id"),
	}
	if err := model.CreateOrgInvitation(inv); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	emailed := sendOrgInvitationEmail(inv.InvitedEmail, org.Name, inv.Code)
	common.ApiSuccess(c, gin.H{"code": inv.Code, "expires_at": inv.ExpiresAt, "emailed": emailed})
}

// ListMyOrgInvitations — GET /api/organization/invitations
func ListMyOrgInvitations(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	rows, err := model.ListOrgInvitations(org.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// RevokeMyOrgInvitation — DELETE /api/organization/invitations/:id
func RevokeMyOrgInvitation(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	invId, _ := strconv.Atoi(c.Param("id"))
	if err := model.RevokeOrgInvitation(org.Id, invId); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- invitation accept (the invited user consents) ----

// PreviewOrgInvitation — GET /api/organization/invitations/preview?code=
// Shows the inviting org + role so the user knows what they're accepting.
func PreviewOrgInvitation(c *gin.Context) {
	inv, err := model.GetOrgInvitationByCode(c.Query("code"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if inv == nil || inv.Status != model.OrgInvitationPending {
		common.ApiErrorMsg(c, "邀请无效或已失效")
		return
	}
	org, _ := model.GetOrganizationById(inv.OrgId)
	orgName := ""
	orgType := ""
	if org != nil {
		orgName = org.Name
		orgType = org.Type
	}
	common.ApiSuccess(c, gin.H{
		"org_name":   orgName,
		"org_type":   orgType,
		"relation":   inv.Relation,
		"role":       inv.Role,
		"expires_at": inv.ExpiresAt,
	})
}

type acceptInviteRequest struct {
	Code string `json:"code"`
}

// AcceptOrgInvitation — POST /api/organization/invitations/accept
// The authenticated user consents; only then are they attached and billed to
// the org.
func AcceptOrgInvitation(c *gin.Context) {
	var req acceptInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	inv, err := model.AcceptOrgInvitation(strings.TrimSpace(req.Code), c.GetInt("id"))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{"org_id": inv.OrgId})
}
