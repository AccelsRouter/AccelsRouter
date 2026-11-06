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

// Ending a customer relationship is refused while the customer still holds
// an allowance; once it is zero the link goes, the customer's org keys are
// disabled, the org is suspended and its members keep their accounts.
func TestRemoveResellerCustomer(t *testing.T) {
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&Token{}))
	DB.Exec("DELETE FROM tokens")
	reseller := mustCreateOrg(t, "reseller-rm", OrgTypeReseller, 0)
	customer := mustCreateOrg(t, "customer-rm", OrgTypeEnterprise, 500)
	other := mustCreateOrg(t, "other-rm", OrgTypeEnterprise, 0)
	require.NoError(t, DB.Create(&ResellerCustomerLink{ResellerOrgId: reseller.Id, CustomerOrgId: customer.Id}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: customer.Id, UserId: 110, Role: OrgRoleOwner, Relation: OrgRelationCustomer, Status: OrgStatusActive}).Error)
	ws := &Workspace{OrgId: customer.Id, Name: "ws"}
	require.NoError(t, DB.Create(ws).Error)
	orgKey := &Token{UserId: 110, Name: "org", Key: "rm-org-key", Status: 1}
	personal := &Token{UserId: 110, Name: "personal", Key: "rm-personal-key", Status: 1}
	require.NoError(t, DB.Create(orgKey).Error)
	require.NoError(t, DB.Create(personal).Error)
	require.NoError(t, DB.Create(&WorkspaceToken{WorkspaceId: ws.Id, OrgId: customer.Id, TokenId: orgKey.Id}).Error)

	_, err := RemoveResellerCustomer(reseller.Id, customer.Id)
	require.ErrorIs(t, err, ErrCustomerHoldsBalance)
	_, err = RemoveResellerCustomer(reseller.Id, other.Id)
	require.Error(t, err)

	require.NoError(t, RevokeCustomerQuota(reseller.Id, customer.Id, 500, 1, "wind down"))
	disabled, err := RemoveResellerCustomer(reseller.Id, customer.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, disabled)
	assert.False(t, IsCustomerOrg(customer.Id))
	gone, err := GetOrganizationById(customer.Id)
	require.NoError(t, err)
	assert.Equal(t, OrgStatusSuspended, gone.Status)
	var orgTok, personalTok Token
	require.NoError(t, DB.First(&orgTok, orgKey.Id).Error)
	assert.Equal(t, 2, orgTok.Status, "org key disabled")
	require.NoError(t, DB.First(&personalTok, personal.Id).Error)
	assert.Equal(t, 1, personalTok.Status, "keys outside the org untouched")
	acc, err := GetOrgAccountByUser(110)
	require.NoError(t, err)
	require.NotNil(t, acc, "member account kept")

	_, err = RemoveResellerCustomer(reseller.Id, customer.Id)
	require.Error(t, err, "already removed")
}
