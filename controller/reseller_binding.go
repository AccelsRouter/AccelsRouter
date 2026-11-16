// Fork-only: the three consoles of binding an existing organization or person
// to a reseller (see model/reseller_binding.go) — the reseller invites and
// answers applications, the customer applies and answers offers, and the
// platform approves when the customer has paid it before.
package controller

import (
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// storedBindOffer validates the offer a reseller makes to a customer it does
// not have yet — the same rules as an existing customer's offer, against the
// standard catalog — and returns it in stored form, or a message.
func storedBindOffer(reseller *model.Organization, models []string, discounts map[string]float64) (storedModels, storedDiscounts, msg string) {
	if _, msg := validateCustomerOffer(reseller, resellerOfferableModels(reseller, "default"), models, discounts); msg != "" {
		return "", "", msg
	}
	storedModels, err := model.MarshalAllowedModels(models)
	if err != nil {
		return "", "", err.Error()
	}
	storedDiscounts, err = model.MarshalRetailDiscounts(discounts)
	if err != nil {
		return "", "", err.Error()
	}
	return storedModels, storedDiscounts, ""
}

// errBindOfferOutdated refuses an offer the reseller could no longer make: its
// wholesale prices or sellable models changed since it was sent.
var errBindOfferOutdated = common.NewMessage("The distributor's offer is out of date; ask the distributor to send a new one")

// bindOfferStands reports whether the offer on a request is one its reseller
// could still make now, checked again before a binding takes effect.
func bindOfferStands(req *model.ResellerBindRequest) error {
	reseller, err := model.GetOrganizationById(req.ResellerOrgId)
	if err != nil {
		return err
	}
	if reseller == nil {
		return model.ErrBindNotOffered
	}
	if _, msg := validateCustomerOffer(reseller, resellerOfferableModels(reseller, "default"), req.Models, req.Discounts); msg != "" {
		return errBindOfferOutdated
	}
	return nil
}

// sendResellerBindEmail mails the confirmation link of an invitation to become
// a reseller's customer. Reports whether the mail went out (SMTP may be unset).
func sendResellerBindEmail(invitedEmail, resellerName, code string) bool {
	if common.SMTPServer == "" && common.SMTPAccount == "" {
		return false
	}
	link := fmt.Sprintf("%s/organization/bind?code=%s", system_setting.ServerAddress, code)
	subject := fmt.Sprintf("分销商 %s 邀请您成为其客户", resellerName)
	// The body is HTML: a reseller's brand is free text, and the address was
	// only checked for an "@".
	content := fmt.Sprintf(
		"<p>分销商 <b>%s</b> 邀请您（%s）成为其客户，并给出了价格与可用模型的报价。</p>"+
			"<p>请点击 <a href='%s'>此处</a> 查看报价并确认（需使用该邮箱对应的账号登录；组织需由 owner 确认）。</p>"+
			"<p>确认前不会有任何变化；链接 7 天内有效。</p>"+
			"<p>如果链接无法点击，请复制到浏览器打开：<br/>%s</p>",
		html.EscapeString(resellerName), html.EscapeString(invitedEmail), link, link)
	if err := common.SendEmail(subject, invitedEmail, content); err != nil {
		common.SysLog("reseller bind invitation email failed: " + err.Error())
		return false
	}
	return true
}

type bindOfferRequest struct {
	Email     string             `json:"email"`
	Models    []string           `json:"models"`
	Discounts map[string]float64 `json:"discounts"`
	Note      string             `json:"note"`
}

// ---- reseller console ----

// GetMyResellerJoinCode — GET /api/reseller/join-code
func GetMyResellerJoinCode(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	code, err := model.GetResellerJoinCode(reseller.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"code": code.Code, "enabled": code.Enabled, "available": reseller.DirectPayEnabled})
}

// UpdateMyResellerJoinCode — PUT /api/reseller/join-code {enabled?, rotate?}
// Switches applications on or off, or replaces the code so links already
// handed out stop working.
func UpdateMyResellerJoinCode(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
		Rotate  bool  `json:"rotate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	code, err := model.UpdateResellerJoinCode(reseller.Id, req.Enabled, req.Rotate)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "bind.join_code", "", fmt.Sprintf("enabled=%t rotated=%t", code.Enabled, req.Rotate))
	common.ApiSuccess(c, gin.H{"code": code.Code, "enabled": code.Enabled, "available": reseller.DirectPayEnabled})
}

// ListMyResellerBindRequests — GET /api/reseller/bind-requests
func ListMyResellerBindRequests(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	rows, err := model.ListResellerBindRequests(reseller.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// InviteExistingCustomer — POST /api/reseller/bind-requests {email, models, discounts}
// Invites the organization or person behind an email address to become the
// reseller's customer under an offer. The answer is the same whoever the
// address belongs to, so nothing about it is disclosed.
func InviteExistingCustomer(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	var req bindOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	storedModels, storedDiscounts, msg := storedBindOffer(reseller, req.Models, req.Discounts)
	if msg != "" {
		common.ApiErrorMsg(c, msg)
		return
	}
	bind, err := model.CreateResellerBindInvite(reseller.Id, c.GetInt("id"), req.Email, storedDiscounts, storedModels)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	emailed := sendResellerBindEmail(bind.InvitedEmail, model.ResellerLabelOfOrg(reseller), bind.Code)
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "bind.invite", fmt.Sprintf("bind:%d", bind.Id), bind.InvitedEmail)
	// The link only works for the invited address's own account, so handing it
	// to the reseller when no mail could be sent gives nothing away.
	code := ""
	if !emailed {
		code = bind.Code
	}
	common.ApiSuccess(c, gin.H{"id": bind.Id, "invited_email": bind.InvitedEmail, "expires_at": bind.ExpiresAt, "emailed": emailed, "code": code})
}

// OfferToBindApplicant — POST /api/reseller/bind-requests/:id/approve {models, discounts}
// Answers an application with the offer the applicant then confirms.
func OfferToBindApplicant(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	var req bindOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	storedModels, storedDiscounts, msg := storedBindOffer(reseller, req.Models, req.Discounts)
	if msg != "" {
		common.ApiErrorMsg(c, msg)
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	bind, err := model.OfferResellerBinding(reseller.Id, id, c.GetInt("id"), storedDiscounts, storedModels)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "bind.offer", fmt.Sprintf("bind:%d", bind.Id), "")
	common.ApiSuccess(c, nil)
}

// CloseMyResellerBindRequest — DELETE /api/reseller/bind-requests/:id
// Rejects an application, or withdraws an offer not yet answered.
func CloseMyResellerBindRequest(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	bind, err := model.CloseResellerBindRequestAsReseller(reseller.Id, id, c.GetInt("id"), strings.TrimSpace(c.Query("note")))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(reseller.Id, c.GetInt("id"), "bind."+bind.Status, fmt.Sprintf("bind:%d", bind.Id), "")
	common.ApiSuccess(c, nil)
}

// ---- customer side ----

// bindRequestForCustomer is the view of a request its customer side gets: the
// offer, who makes it, and the code that opens its confirmation page.
func bindRequestForCustomer(req *model.ResellerBindRequest) gin.H {
	reseller, _ := model.GetOrganizationById(req.ResellerOrgId)
	name, logo := "", ""
	if reseller != nil {
		name, logo = model.ResellerLabelOfOrg(reseller), reseller.BrandLogo
	}
	return gin.H{
		"id":            req.Id,
		"code":          req.Code,
		"direction":     req.Direction,
		"status":        req.Status,
		"reseller_name": name,
		"reseller_logo": logo,
		"discounts":     req.Discounts,
		"models":        req.Models,
		"invited_email": req.InvitedEmail,
		"org_name":      req.OrgName,
		"needs_review":  req.NeedsReview,
		"expires_at":    req.ExpiresAt,
		"created_time":  req.CreatedTime,
	}
}

// ListMyResellerBindings — GET /api/organization/reseller-binding
// What is waiting on or for the caller: applications they made, offers to
// confirm, and invitations addressed to their email.
func ListMyResellerBindings(c *gin.Context) {
	rows, err := model.OpenResellerBindRequestsOfUser(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, bindRequestForCustomer(row))
	}
	common.ApiSuccess(c, out)
}

// PreviewResellerBinding — GET /api/organization/reseller-binding/preview?code=|join=
// The confirmation page: which reseller, its offer when there is one, and what
// binding would change for the caller — or why it cannot happen. `code` is a
// request's link, `join` a reseller's join code.
func PreviewResellerBinding(c *gin.Context) {
	userId := c.GetInt("id")
	resellerOrgId := 0
	var request gin.H
	if code := strings.TrimSpace(c.Query("code")); code != "" {
		req, err := model.GetResellerBindRequestByCode(code)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		// A link is shown to whoever may answer it and to nobody else.
		if req == nil || !bindRequestConcerns(req, userId) {
			common.ApiError(c, model.ErrBindRequestGone)
			return
		}
		resellerOrgId = req.ResellerOrgId
		request = bindRequestForCustomer(req)
	} else {
		id, err := model.ResellerOrgIdByJoinCode(c.Query("join"))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		resellerOrgId = id
	}
	reseller, err := model.GetOrganizationById(resellerOrgId)
	if err != nil || reseller == nil {
		common.ApiError(c, model.ErrBindNotOffered)
		return
	}
	resp := gin.H{
		"request":       request,
		"reseller_name": model.ResellerLabelOfOrg(reseller),
		"reseller_logo": reseller.BrandLogo,
	}
	outlook, err := model.ResellerBindOutlookFor(resellerOrgId, userId)
	var blocker *common.Message
	switch {
	case err == nil:
		resp["outlook"] = outlook
	case errors.As(err, &blocker):
		fields := blocker.Fields()
		fields["message"] = blocker.Error()
		resp["blocker"] = fields
	default:
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

// bindRequestConcerns reports whether a request is the user's to see: they
// applied or consented for the customer, or it is an unanswered invitation to
// their email address.
func bindRequestConcerns(req *model.ResellerBindRequest, userId int) bool {
	if req.CustomerUserId == userId {
		return true
	}
	if req.Direction != model.BindDirectionInvite || req.CustomerUserId != 0 {
		return false
	}
	user, err := model.GetUserById(userId, false)
	return err == nil && user != nil && model.NormalizeEmail(user.Email) == req.InvitedEmail
}

// ApplyToJoinReseller — POST /api/organization/reseller-binding/apply {join_code, remark}
func ApplyToJoinReseller(c *gin.Context) {
	var req struct {
		JoinCode string `json:"join_code"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Remark) > 255 {
		req.Remark = req.Remark[:255]
	}
	bind, err := model.ApplyToReseller(req.JoinCode, c.GetInt("id"), req.Remark)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(bind.ResellerOrgId, c.GetInt("id"), "bind.apply", fmt.Sprintf("bind:%d", bind.Id), "")
	common.ApiSuccess(c, gin.H{"id": bind.Id, "status": bind.Status})
}

// AcceptResellerBindOffer — POST /api/organization/reseller-binding/accept {code, org_name, transfer_balance}
// The customer's consent. The binding takes effect at once, or waits for the
// platform when the customer has paid it before.
func AcceptResellerBindOffer(c *gin.Context) {
	var req struct {
		Code            string `json:"code"`
		OrgName         string `json:"org_name"`
		TransferBalance bool   `json:"transfer_balance"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	offer, err := model.GetResellerBindRequestByCode(req.Code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if offer == nil {
		common.ApiError(c, model.ErrBindRequestGone)
		return
	}
	if err := bindOfferStands(offer); err != nil {
		common.ApiError(c, err)
		return
	}
	bind, err := model.AcceptResellerBinding(req.Code, c.GetInt("id"), req.OrgName, req.TransferBalance)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordResellerBindOutcome(bind, c.GetInt("id"), "bind.accept")
	common.ApiSuccess(c, gin.H{"id": bind.Id, "status": bind.Status, "org_id": bind.CustomerOrgId})
}

// recordResellerBindOutcome writes a step of a request to the reseller's audit
// trail and, once the customer's organization is known, to the customer's.
func recordResellerBindOutcome(bind *model.ResellerBindRequest, operatorId int, action string) {
	detail := "status=" + bind.Status
	if bind.TransferQuota > 0 {
		detail += fmt.Sprintf(" transferred=%d", bind.TransferQuota)
	}
	model.RecordOrgAudit(bind.ResellerOrgId, operatorId, action, fmt.Sprintf("bind:%d", bind.Id), detail)
	if bind.CustomerOrgId > 0 {
		model.RecordOrgAudit(bind.CustomerOrgId, operatorId, action, fmt.Sprintf("org:%d", bind.ResellerOrgId), detail)
	}
}

// CloseMyResellerBinding — DELETE /api/organization/reseller-binding/:id
// Withdraws the caller's own request, or turns down an invitation.
func CloseMyResellerBinding(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	bind, err := model.CloseResellerBindRequestAsCustomer(id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordResellerBindOutcome(bind, c.GetInt("id"), "bind.decline")
	common.ApiSuccess(c, nil)
}

// ---- platform ----

// AdminListResellerBindRequests — GET /api/admin/organizations/bind-requests?status=
func AdminListResellerBindRequests(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.ListResellerBindRequestsForReview(c.Query("status"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// AdminApproveResellerBindRequest — POST /api/admin/organizations/bind-requests/:id/approve {note}
// The platform's yes for a customer that has paid it before: the binding
// takes effect, if the reseller's offer still stands.
func AdminApproveResellerBindRequest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	pending, err := model.GetResellerBindRequestById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if pending == nil {
		common.ApiError(c, model.ErrBindRequestGone)
		return
	}
	if err := bindOfferStands(pending); err != nil {
		common.ApiError(c, err)
		return
	}
	reviewResellerBindRequest(c, id, true)
}

// AdminRejectResellerBindRequest — POST /api/admin/organizations/bind-requests/:id/reject {note}
func AdminRejectResellerBindRequest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	reviewResellerBindRequest(c, id, false)
}

func reviewResellerBindRequest(c *gin.Context, id int, approve bool) {
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	bind, err := model.ReviewResellerBinding(id, c.GetInt("id"), approve, strings.TrimSpace(req.Note))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordResellerBindOutcome(bind, c.GetInt("id"), "bind.review")
	common.ApiSuccess(c, gin.H{"status": bind.Status})
}
