package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// IsResellerParty reports whether a user belongs to the reseller side of the
// platform in any capacity: a reseller admin, a member of a reseller
// organization, or a user of a reseller-provisioned customer organization. Such
// users must stay on platform-controlled upstreams (see reseller upstream
// routing) and are therefore not allowed to configure BYOK. Errors bubble up so
// callers can fail closed.
func IsResellerParty(userId int) (bool, error) {
	if userId <= 0 {
		return false, nil
	}
	if isAdmin, err := IsResellerAdmin(userId); err != nil {
		return false, err
	} else if isAdmin {
		return true, nil
	}
	acc, err := GetOrgAccountByUser(userId)
	if err != nil {
		return false, err
	}
	if acc == nil {
		return false, nil
	}
	if IsCustomerOrg(acc.OrgId) {
		return true, nil
	}
	org, err := GetOrganizationById(acc.OrgId)
	if err != nil {
		return false, err
	}
	return org != nil && org.Type == OrgTypeReseller, nil
}

// DisablePersonalTokens disables every ENABLED token of the user that is not
// bound to any workspace — i.e. the user's personal keys, billed from the
// personal balance. Reseller parties (reseller admins, reseller-org members,
// reseller customers) must consume only through organization keys, which
// bill the organization wallet and take the reseller route; a personal key
// would bypass both. Idempotent; returns how many tokens were disabled.
func DisablePersonalTokens(userId int) (int, error) {
	if userId <= 0 {
		return 0, nil
	}
	var tokens []Token
	if err := DB.Where("user_id = ? AND status = ?", userId, common.TokenStatusEnabled).
		Where("id NOT IN (?)", DB.Table("workspace_tokens").Select("token_id")).
		Find(&tokens).Error; err != nil {
		return 0, err
	}
	if len(tokens) == 0 {
		return 0, nil
	}
	ids := make([]int, 0, len(tokens))
	for _, t := range tokens {
		ids = append(ids, t.Id)
	}
	if err := DB.Model(&Token{}).Where("id IN ?", ids).
		Update("status", common.TokenStatusDisabled).Error; err != nil {
		return 0, err
	}
	if err := invalidateTokensCache(tokens); err != nil {
		common.SysError("failed to invalidate disabled personal token cache: " + err.Error())
	}
	return len(tokens), nil
}

// DisableResellerPartiesPersonalTokens is the startup sweep for the rule
// enforced live by DisablePersonalTokens: every existing reseller party's
// personal keys are disabled once, so keys created before the rule cannot
// keep billing a personal balance outside the reseller route. Idempotent.
func DisableResellerPartiesPersonalTokens() error {
	var userIds []int
	if err := DB.Model(&ResellerAdmin{}).Distinct().Pluck("user_id", &userIds).Error; err != nil {
		return err
	}
	var memberIds []int
	if err := DB.Table("org_accounts").
		Joins("join organizations on organizations.id = org_accounts.org_id").
		Where("organizations.type = ? OR org_accounts.org_id IN (?)", OrgTypeReseller,
			DB.Table("reseller_customer_links").Select("customer_org_id")).
		Distinct().Pluck("org_accounts.user_id", &memberIds).Error; err != nil {
		return err
	}
	userIds = append(userIds, memberIds...)
	seen := map[int]struct{}{}
	disabled := 0
	for _, id := range userIds {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		n, err := DisablePersonalTokens(id)
		if err != nil {
			return err
		}
		disabled += n
	}
	if disabled > 0 {
		common.SysLog(fmt.Sprintf("disabled %d personal token(s) of reseller parties", disabled))
	}
	return nil
}
