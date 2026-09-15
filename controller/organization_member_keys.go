// Member-scoped org API keys: a simplified, self-service key manager for any
// active organization member (not just owners/admins). Keys created here are
// bound to the org's default workspace so they bill the ORG wallet (gated by
// the member's monthly budget → workspace budget → org wallet), unlike personal
// keys on /keys which bill the member's own balance. This backs the "API Keys"
// entry in the org-member sidebar so members never accidentally mint a personal
// key that has no funds behind it.
package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// callerOrgMember resolves the caller's active organization from their
// OrgAccount, for any role (member/admin/owner) — the self-service key surface
// is open to every member, since spend is capped by the member's own budget.
func callerOrgMember(c *gin.Context) (*model.Organization, bool) {
	acc, err := model.GetOrgAccountByUser(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if acc == nil {
		common.ApiErrorMsg(c, "你不属于任何组织")
		return nil, false
	}
	if acc.Status == model.OrgStatusSuspended {
		common.ApiErrorMsg(c, "账号已被暂停")
		return nil, false
	}
	org, err := model.GetOrganizationById(acc.OrgId)
	if err != nil || org == nil {
		common.ApiErrorMsg(c, "organization not found")
		return nil, false
	}
	if org.Status == model.OrgStatusSuspended {
		common.ApiErrorMsg(c, "组织已被暂停")
		return nil, false
	}
	return org, true
}

// myOrgKeyTokenIds returns the caller's own token ids that are bound to a
// workspace in the given org.
func myOrgKeyTokenIds(orgId, userId int) ([]int, error) {
	var bindings []model.WorkspaceToken
	if err := model.DB.Where("org_id = ?", orgId).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}
	boundIds := make([]int, 0, len(bindings))
	for _, b := range bindings {
		boundIds = append(boundIds, b.TokenId)
	}
	var tokens []model.Token
	if err := model.DB.Select("id").
		Where("id IN ? AND user_id = ?", boundIds, userId).
		Find(&tokens).Error; err != nil {
		return nil, err
	}
	out := make([]int, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, t.Id)
	}
	return out, nil
}

// ListMyOrgKeys — GET /api/organization/keys
func ListMyOrgKeys(c *gin.Context) {
	org, ok := callerOrgMember(c)
	if !ok {
		return
	}
	tokenIds, err := myOrgKeyTokenIds(org.Id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	out := make([]gin.H, 0, len(tokenIds))
	if len(tokenIds) > 0 {
		var tokens []model.Token
		if err := model.DB.Where("id IN ?", tokenIds).Order("id DESC").Find(&tokens).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, tk := range tokens {
			out = append(out, gin.H{
				"token_id":        tk.Id,
				"name":            tk.Name,
				"status":          tk.Status,
				"key_masked":      "sk-" + maskChannelKey(tk.Key),
				"unlimited_quota": tk.UnlimitedQuota,
				"remain_quota":    tk.RemainQuota,
				"created_time":    tk.CreatedTime,
			})
		}
	}
	common.ApiSuccess(c, out)
}

// ensureDefaultWorkspace returns the org's first active workspace, creating a
// "Default" one only when the org has NO workspaces at all. Member key creation
// must not require an admin to have provisioned a workspace first — but if
// workspaces exist and every one is suspended, an admin has deliberately closed
// org-wallet spend, so we refuse rather than mint a fresh unlimited workspace
// that would reopen the very control they set (and hand workspace creation to a
// non-admin around it).
func ensureDefaultWorkspace(orgId int) (*model.Workspace, error) {
	workspaces, err := model.ListWorkspaces(orgId)
	if err != nil {
		return nil, err
	}
	for _, ws := range workspaces {
		if ws.Status != model.OrgStatusSuspended {
			return ws, nil
		}
	}
	if len(workspaces) > 0 {
		return nil, fmt.Errorf("组织暂无可用的工作区，请联系管理员")
	}
	ws := &model.Workspace{OrgId: orgId, Name: "Default"}
	if err := model.CreateWorkspace(ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// CreateMyOrgKey — POST /api/organization/keys
func CreateMyOrgKey(c *gin.Context) {
	org, ok := callerOrgMember(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "API Key"
	}
	if len([]rune(name)) > 64 {
		common.ApiErrorMsg(c, "名称过长（最多 64 字）")
		return
	}
	ws, err := ensureDefaultWorkspace(org.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Unlimited at the token level: an org key is bounded by the member budget,
	// the workspace budget and the org wallet (OrgWalletFunding), not by a
	// per-token quota the member would have to top up themselves.
	token := &model.Token{
		UserId:         c.GetInt("id"),
		Name:           name,
		Key:            key,
		CreatedTime:    common.GetTimestamp(),
		AccessedTime:   common.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Status:         common.TokenStatusEnabled,
	}
	if err := token.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.BindTokenToWorkspace(org.Id, ws.Id, token.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "member.key.create", fmt.Sprintf("workspace:%d", ws.Id), fmt.Sprintf("token:%d %s", token.Id, name))
	common.ApiSuccess(c, gin.H{"token_id": token.Id, "key": "sk-" + key})
}

// resolveOwnedOrgKey parses :token_id and authorizes it: the token must be one
// of the caller's own keys bound to this org. Writes the error and returns
// ok=false on any failure.
func resolveOwnedOrgKey(c *gin.Context, org *model.Organization) (int, bool) {
	tokenId := 0
	if _, err := fmt.Sscanf(c.Param("token_id"), "%d", &tokenId); err != nil || tokenId <= 0 {
		common.ApiErrorMsg(c, "invalid token id")
		return 0, false
	}
	tokenIds, err := myOrgKeyTokenIds(org.Id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return 0, false
	}
	for _, id := range tokenIds {
		if id == tokenId {
			return tokenId, true
		}
	}
	common.ApiErrorMsg(c, "该密钥不存在或不属于你")
	return 0, false
}

// GetMyOrgKey — POST /api/organization/keys/:token_id/key
// Reveals the full key so a member can re-copy it after creation (the list only
// returns a masked value). Mirrors the personal-token reveal endpoint.
func GetMyOrgKey(c *gin.Context) {
	org, ok := callerOrgMember(c)
	if !ok {
		return
	}
	tokenId, ok := resolveOwnedOrgKey(c, org)
	if !ok {
		return
	}
	token, err := model.GetTokenByIds(tokenId, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"key": "sk-" + token.Key})
}

// DeleteMyOrgKey — DELETE /api/organization/keys/:token_id
func DeleteMyOrgKey(c *gin.Context) {
	org, ok := callerOrgMember(c)
	if !ok {
		return
	}
	tokenId, ok := resolveOwnedOrgKey(c, org)
	if !ok {
		return
	}
	// Delete the token first, then unbind: if the unbind failed after a
	// successful delete we would only orphan a binding row to a gone token
	// (harmless), whereas the reverse could leave a live unlimited key silently
	// converted to personal billing.
	if err := model.DeleteTokenById(tokenId, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UnbindTokenFromWorkspace(tokenId); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(org.Id, c.GetInt("id"), "member.key.delete", fmt.Sprintf("token:%d", tokenId), "")
	common.ApiSuccess(c, nil)
}
