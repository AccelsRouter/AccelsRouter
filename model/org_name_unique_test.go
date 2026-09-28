package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Organization names are unique across enterprises, distributors and their
// customers (case-insensitive, trimmed), on every path that sets one: direct
// creation, customer provisioning, applications (also against pending ones),
// approval, and rename (which may keep its own name).
func TestOrganizationNamesAreUnique(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "Acme Reseller", OrgTypeReseller, 1000)

	// Direct creation: exact, case and whitespace variants all collide.
	for _, name := range []string{"Acme Reseller", "acme reseller", "  ACME RESELLER "} {
		err := CreateOrganization(&Organization{Name: name, Type: OrgTypeEnterprise, PriceGroup: "default"})
		assert.ErrorIs(t, err, ErrOrgNameTaken, name)
	}

	// A distributor cannot provision a customer named like itself or another org.
	_, err := CreateResellerCustomer(reseller.Id, "acme reseller", "retail", 100, 1)
	assert.ErrorIs(t, err, ErrOrgNameTaken)
	cust, err := CreateResellerCustomer(reseller.Id, "Acme Customer", "retail", 100, 1)
	require.NoError(t, err)

	// Applications: an existing org's name is refused, and so is a name that
	// another pending application already claims.
	assert.ErrorIs(t, CreateOrgApplication(&OrgApplication{UserId: 9001, Type: OrgTypeEnterprise, OrgName: "ACME Customer"}), ErrOrgNameTaken)
	require.NoError(t, CreateOrgApplication(&OrgApplication{UserId: 9001, Type: OrgTypeEnterprise, OrgName: "Fresh Org"}))
	assert.ErrorIs(t, CreateOrgApplication(&OrgApplication{UserId: 9002, Type: OrgTypeEnterprise, OrgName: "fresh org"}), ErrOrgNameTaken)

	// Approval re-checks: if the name was taken meanwhile, approval fails.
	var app OrgApplication
	require.NoError(t, DB.Where("user_id = ?", 9001).First(&app).Error)
	require.NoError(t, CreateOrganization(&Organization{Name: "Fresh Org", Type: OrgTypeEnterprise, PriceGroup: "default"}))
	_, err = ApproveOrgApplication(app.Id, 1, "", "")
	assert.ErrorIs(t, err, ErrOrgNameTaken)

	// Rename: another org's name is taken; the org's own current name is not.
	taken, err := OrgNameTaken("acme customer", reseller.Id)
	require.NoError(t, err)
	assert.True(t, taken)
	taken, err = OrgNameTaken("ACME RESELLER", reseller.Id)
	require.NoError(t, err)
	assert.False(t, taken, "a rename may keep its own name")
	_ = cust
}

// Invite = open the account: an unregistered email gets a user + customer-org
// membership + a 7-day activation link; activation sets the password once;
// renewal issues a fresh window; a registered email is left to the consent
// flow; revocation kills the link.
func TestProvisionedCustomerAccountLifecycle(t *testing.T) {
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&User{}))
	reseller := mustCreateOrg(t, "Prov Reseller", OrgTypeReseller, 1000)
	cust, err := CreateResellerCustomer(reseller.Id, "Prov Customer", "retail", 100, 1)
	require.NoError(t, err)

	inv, user, err := ProvisionCustomerAccount(cust.Id, "  Zhong.Sheng@Example.com ", 1)
	require.NoError(t, err)
	assert.Equal(t, "zhong.sheng@example.com", user.Email)
	assert.Equal(t, "zhong.sheng", user.Username)
	assert.Equal(t, OrgInvitationProvisioned, inv.Status)
	assert.Equal(t, user.Id, inv.AcceptedUserId)
	assert.InDelta(t, time.Now().Add(orgActivationTTL).Unix(), inv.ExpiresAt, 5)
	acc, err := GetOrgAccountByUser(user.Id)
	require.NoError(t, err)
	require.NotNil(t, acc)
	assert.Equal(t, cust.Id, acc.OrgId)
	assert.Equal(t, OrgRelationCustomer, acc.Relation)

	// Same local part elsewhere gets a numeric suffix.
	_, user2, err := ProvisionCustomerAccount(cust.Id, "zhong.sheng@other.com", 1)
	require.NoError(t, err)
	assert.Equal(t, "zhong.sheng-2", user2.Username)

	// A registered email is not provisioned again.
	_, _, err = ProvisionCustomerAccount(cust.Id, "zhong.sheng@example.com", 1)
	assert.ErrorIs(t, err, ErrEmailAlreadyRegistered)

	// Activation: password policy, single use, sets a password that verifies.
	_, err = ActivateProvisionedAccount(inv.Code, "short")
	require.Error(t, err)
	activated, err := ActivateProvisionedAccount(inv.Code, "Secret-Pass-1")
	require.NoError(t, err)
	assert.Equal(t, user.Id, activated.Id)
	assert.True(t, common.ValidatePasswordAndHash("Secret-Pass-1", activated.Password))
	_, err = ActivateProvisionedAccount(inv.Code, "Secret-Pass-2")
	require.Error(t, err, "the code is single-use")

	// Renewal rotates the code and window of a still-provisioned invitation;
	// an expired one becomes valid again, an accepted one cannot be renewed.
	inv2, _, err := ProvisionCustomerAccount(cust.Id, "third@example.com", 1)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&OrgInvitation{}).Where("id = ?", inv2.Id).Update("expires_at", time.Now().Add(-time.Hour).Unix()).Error)
	_, err = GetActivationByCode(inv2.Code)
	require.Error(t, err, "expired link is refused")
	renewed, err := RenewProvisionedInvitation(cust.Id, inv2.Id)
	require.NoError(t, err)
	assert.NotEqual(t, inv2.Code, renewed.Code)
	_, err = GetActivationByCode(renewed.Code)
	require.NoError(t, err)
	_, err = RenewProvisionedInvitation(cust.Id, inv.Id)
	require.Error(t, err, "an accepted invitation cannot be renewed")

	// Revocation kills a provisioned link.
	require.NoError(t, RevokeOrgInvitation(cust.Id, inv2.Id))
	_, err = GetActivationByCode(renewed.Code)
	require.Error(t, err)
}
