package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BYOK is refused to every reseller party — reseller admins, reseller-org
// members and reseller customers — because their traffic must stay on
// platform-controlled upstreams. IsResellerParty is that boundary, so each
// membership shape must be recognised and ordinary platform users must not.
func TestIsResellerPartyCoversAdminsMembersAndCustomers(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 0)
	customer := mustCreateOrg(t, "customer", OrgTypeEnterprise, 0)
	enterprise := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	require.NoError(t, DB.Create(&ResellerCustomerLink{ResellerOrgId: reseller.Id, CustomerOrgId: customer.Id}).Error)

	require.NoError(t, DB.Create(&ResellerAdmin{UserId: 100, ResellerOrgId: reseller.Id, Status: OrgStatusActive}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: reseller.Id, UserId: 101, Role: OrgRoleMember, Status: OrgStatusActive}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: customer.Id, UserId: 102, Role: OrgRoleOwner, Relation: OrgRelationCustomer, Status: OrgStatusActive}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: enterprise.Id, UserId: 200, Role: OrgRoleOwner, Status: OrgStatusActive}).Error)

	cases := []struct {
		name   string
		userId int
		want   bool
	}{
		{"reseller admin", 100, true},
		{"reseller org member", 101, true},
		{"reseller customer user", 102, true},
		{"enterprise member", 200, false},
		{"unattached user", 300, false},
		{"invalid user id", 0, false},
	}
	for _, tc := range cases {
		got, err := IsResellerParty(tc.userId)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
}
