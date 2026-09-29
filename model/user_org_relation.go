package model

// UserOrgRelation is one organization tie of a platform user, for the admin
// user list: distributor admin of a reseller org, member of a reseller
// customer org (with the owning distributor), or member of an enterprise
// org. A user may hold two ties (distributor admin + enterprise member).
type UserOrgRelation struct {
	Kind            string `json:"kind"` // reseller_admin | customer | member
	OrgId           int    `json:"org_id"`
	OrgName         string `json:"org_name"`
	OrgType         string `json:"org_type"`
	Role            string `json:"role,omitempty"` // owner | admin | member (org accounts)
	ResellerOrgId   int    `json:"reseller_org_id,omitempty"`
	ResellerOrgName string `json:"reseller_org_name,omitempty"`
}

// UserOrgRelations batch-resolves the organization ties of the given users
// (three queries, no per-user lookups). Users without ties are absent.
func UserOrgRelations(userIds []int) (map[int][]UserOrgRelation, error) {
	out := map[int][]UserOrgRelation{}
	if len(userIds) == 0 {
		return out, nil
	}
	var admins []ResellerAdmin
	if err := DB.Where("user_id IN ? AND (status = ? OR status = '')", userIds, OrgStatusActive).Find(&admins).Error; err != nil {
		return nil, err
	}
	var accounts []OrgAccount
	if err := DB.Where("user_id IN ?", userIds).Find(&accounts).Error; err != nil {
		return nil, err
	}
	orgIds := map[int]struct{}{}
	for _, a := range admins {
		orgIds[a.ResellerOrgId] = struct{}{}
	}
	for _, a := range accounts {
		orgIds[a.OrgId] = struct{}{}
	}
	var links []ResellerCustomerLink
	if len(orgIds) > 0 {
		ids := make([]int, 0, len(orgIds))
		for id := range orgIds {
			ids = append(ids, id)
		}
		if err := DB.Where("customer_org_id IN ?", ids).Find(&links).Error; err != nil {
			return nil, err
		}
		for _, l := range links {
			orgIds[l.ResellerOrgId] = struct{}{}
		}
	}
	orgById := map[int]Organization{}
	if len(orgIds) > 0 {
		ids := make([]int, 0, len(orgIds))
		for id := range orgIds {
			ids = append(ids, id)
		}
		var orgs []Organization
		if err := DB.Select("id", "name", "type").Where("id IN ?", ids).Find(&orgs).Error; err != nil {
			return nil, err
		}
		for _, o := range orgs {
			orgById[o.Id] = o
		}
	}
	resellerOf := map[int]int{}
	for _, l := range links {
		resellerOf[l.CustomerOrgId] = l.ResellerOrgId
	}
	for _, a := range admins {
		org := orgById[a.ResellerOrgId]
		out[a.UserId] = append(out[a.UserId], UserOrgRelation{
			Kind: "reseller_admin", OrgId: a.ResellerOrgId, OrgName: org.Name, OrgType: org.Type,
		})
	}
	for _, a := range accounts {
		org := orgById[a.OrgId]
		rel := UserOrgRelation{Kind: "member", OrgId: a.OrgId, OrgName: org.Name, OrgType: org.Type, Role: a.Role}
		if rid, ok := resellerOf[a.OrgId]; ok && rid > 0 {
			rel.Kind = "customer"
			rel.ResellerOrgId = rid
			rel.ResellerOrgName = orgById[rid].Name
		}
		out[a.UserId] = append(out[a.UserId], rel)
	}
	return out, nil
}
