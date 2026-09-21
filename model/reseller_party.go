package model

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
