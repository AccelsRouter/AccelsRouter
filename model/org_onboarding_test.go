package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user opens their OWN org by applying + approval; approval creates the org
// and makes the applicant its owner. The applicant must be unmanaged, and
// re-approving a processed application is a no-op.
func TestOrgApplicationApproval(t *testing.T) {
	migrateOrgTables(t)

	app := &OrgApplication{UserId: 100, Type: OrgTypeReseller, OrgName: "acme-reseller"}
	require.NoError(t, CreateOrgApplication(app))

	// One pending application per user.
	dup := &OrgApplication{UserId: 100, Type: OrgTypeEnterprise, OrgName: "acme-2"}
	require.Error(t, CreateOrgApplication(dup))

	org, err := ApproveOrgApplication(app.Id, 1, "partner-a", "ok")
	require.NoError(t, err)
	require.NotNil(t, org)
	assert.Equal(t, OrgTypeReseller, org.Type)
	assert.Equal(t, "partner-a", org.PriceGroup)
	assert.Equal(t, 100, org.OwnerUserId)

	// A reseller admin is a management role, NOT a paying OrgAccount: the
	// applicant administers the reseller org but keeps its single-payer slot
	// free (no org_accounts row). Its hot-path payer record still points at the
	// reseller org, so the admin's own reseller keys route and bill as reseller
	// traffic.
	adminOrg, err := GetResellerAdminOrg(100)
	require.NoError(t, err)
	require.NotNil(t, adminOrg)
	assert.Equal(t, org.Id, adminOrg.Id)
	acc, err := GetOrgAccountByUser(100)
	require.NoError(t, err)
	assert.Nil(t, acc, "reseller admin must not occupy the single-payer slot")

	info, err := GetOrgPayerInfo(100)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, org.Id, info.OrgId)
	assert.Equal(t, org.Id, info.ResellerOrgId, "own keys are reseller traffic")

	// Re-approving the same (now approved) application fails.
	_, err = ApproveOrgApplication(app.Id, 1, "", "")
	require.Error(t, err)

	// A reseller admin cannot open a second reseller org.
	require.Error(t, CreateOrgApplication(&OrgApplication{UserId: 100, Type: OrgTypeReseller, OrgName: "acme-reseller-2"}))

	// But because the reseller role is decoupled from the payer, the SAME user
	// may also apply for an enterprise org (dual role: reseller admin +
	// enterprise member).
	require.NoError(t, CreateOrgApplication(&OrgApplication{UserId: 100, Type: OrgTypeEnterprise, OrgName: "another-org"}))
}

// Org names must be at least 3 characters and free of special characters.
func TestValidateOrgName(t *testing.T) {
	require.NoError(t, ValidateOrgName("acme"))
	require.NoError(t, ValidateOrgName("Acme Corp"))
	require.NoError(t, ValidateOrgName("acme-2_test"))
	require.NoError(t, ValidateOrgName("北京团队"))    // CJK letters count
	require.NoError(t, ValidateOrgName("  abc  ")) // trimmed to 3

	require.Error(t, ValidateOrgName(""))
	require.Error(t, ValidateOrgName("ab"))       // < 3
	require.Error(t, ValidateOrgName("acme!"))    // special char
	require.Error(t, ValidateOrgName("a@b.com"))  // special chars
	require.Error(t, ValidateOrgName("<script>")) // special chars
}

// Default onboarding policy mirrors OpenRouter: an enterprise org opens
// instantly (self-serve), while a reseller stays review-gated. This guards the
// product decision that opening an enterprise org needs no human approval but
// granting reseller (which can allocate credit to other orgs) does.
func TestDefaultOrgAutoApprovePolicy(t *testing.T) {
	assert.True(t, OrgTypeAutoApproves(OrgTypeEnterprise), "enterprise must open instantly by default")
	assert.False(t, OrgTypeAutoApproves(OrgTypeReseller), "reseller must stay review-gated by default")
}

// Invitations are consent tokens: only the accepting user is attached, an
// invite is single-use, and a user already in an org cannot accept.
func TestOrgInvitationConsent(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)

	// The invited user must exist with the matching email (accept is email-scoped).
	bob := &User{Username: "inv-bob", Email: "bob@acme.com", AffCode: "inv-bob-aff"}
	require.NoError(t, DB.Create(bob).Error)

	// Email is required on create.
	require.Error(t, CreateOrgInvitation(&OrgInvitation{OrgId: org.Id, Relation: OrgRelationMember, Role: OrgRoleMember, CreatedBy: 1}))

	inv := &OrgInvitation{OrgId: org.Id, Relation: OrgRelationMember, Role: OrgRoleMember, MonthlyBudget: 500, InvitedEmail: "bob@acme.com", CreatedBy: 1}
	require.NoError(t, CreateOrgInvitation(inv))
	assert.NotEmpty(t, inv.Code)

	// A user whose email does NOT match cannot accept.
	carol := &User{Username: "inv-carol", Email: "carol@acme.com", AffCode: "inv-carol-aff"}
	require.NoError(t, DB.Create(carol).Error)
	_, err := AcceptOrgInvitation(inv.Code, carol.Id)
	require.Error(t, err, "only the invited email may accept")

	// The invited user accepts → becomes a managed member.
	accepted, err := AcceptOrgInvitation(inv.Code, bob.Id)
	require.NoError(t, err)
	assert.Equal(t, org.Id, accepted.OrgId)
	info, err := GetOrgPayerInfo(bob.Id)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, org.Id, info.OrgId)
	assert.Equal(t, 500, info.MonthlyBudget)

	// Single-use: the same code cannot be accepted again.
	_, err = AcceptOrgInvitation(inv.Code, carol.Id)
	require.Error(t, err)

	// A user already in an org cannot accept a new invite.
	inv2 := &OrgInvitation{OrgId: org.Id, Relation: OrgRelationMember, Role: OrgRoleMember, InvitedEmail: "bob@acme.com", CreatedBy: 1}
	require.NoError(t, CreateOrgInvitation(inv2))
	_, err = AcceptOrgInvitation(inv2.Code, bob.Id)
	require.Error(t, err, "an already-managed user cannot be double-attached")

	// Revoked invites cannot be accepted.
	inv3 := &OrgInvitation{OrgId: org.Id, Relation: OrgRelationMember, Role: OrgRoleMember, InvitedEmail: "carol@acme.com", CreatedBy: 1}
	require.NoError(t, CreateOrgInvitation(inv3))
	require.NoError(t, RevokeOrgInvitation(org.Id, inv3.Id))
	_, err = AcceptOrgInvitation(inv3.Code, carol.Id)
	require.Error(t, err)

	// An invite can only be revoked by its owning org.
	other := mustCreateOrg(t, "other", OrgTypeEnterprise, 0)
	inv4 := &OrgInvitation{OrgId: org.Id, Relation: OrgRelationMember, Role: OrgRoleMember, InvitedEmail: "carol@acme.com", CreatedBy: 1}
	require.NoError(t, CreateOrgInvitation(inv4))
	require.Error(t, RevokeOrgInvitation(other.Id, inv4.Id), "a foreign org cannot revoke another org's invite")
}

// A reseller's customer organization is owned by the first operator who comes
// in — by accepting an invitation or by setting the password of a provisioned
// account — and a later operator stays an admin. Organizations opened before
// that rule get their first operator as owner at startup.
func TestFirstCustomerOperatorOwnsTheOrganization(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "owner-rule-reseller", OrgTypeReseller, 0)
	roleOf := func(orgId, userId int) string {
		var acc OrgAccount
		require.NoError(t, DB.Where("org_id = ? AND user_id = ?", orgId, userId).First(&acc).Error)
		return acc.Role
	}
	inviteAndAccept := func(org *Organization, name string) *User {
		user := &User{Username: name, Email: name + "@example.com", AffCode: name + "-aff"}
		require.NoError(t, DB.Create(user).Error)
		inv := &OrgInvitation{OrgId: org.Id, Relation: OrgRelationCustomer, Role: OrgRoleAdmin, InvitedEmail: user.Email, CreatedBy: 1}
		require.NoError(t, CreateOrgInvitation(inv))
		_, err := AcceptOrgInvitation(inv.Code, user.Id)
		require.NoError(t, err)
		return user
	}

	invited, err := CreateResellerCustomer(reseller.Id, "owner-rule-invited", "", "", 100, 1)
	require.NoError(t, err)
	first := inviteAndAccept(invited, "owner-rule-first")
	second := inviteAndAccept(invited, "owner-rule-second")
	assert.Equal(t, OrgRoleOwner, roleOf(invited.Id, first.Id))
	assert.Equal(t, OrgRoleAdmin, roleOf(invited.Id, second.Id))
	fresh, err := GetOrganizationById(invited.Id)
	require.NoError(t, err)
	assert.Equal(t, first.Id, fresh.OwnerUserId)

	// A provisioned account owns nothing until its password is set.
	provisioned, err := CreateResellerCustomer(reseller.Id, "owner-rule-provisioned", "", "", 100, 1)
	require.NoError(t, err)
	inv, opened, err := ProvisionCustomerAccount(provisioned.Id, "owner-rule-opened@example.com", 1)
	require.NoError(t, err)
	require.NoError(t, backfillCustomerOrgOwners())
	assert.Equal(t, OrgRoleAdmin, roleOf(provisioned.Id, opened.Id))
	_, err = ActivateProvisionedAccount(inv.Code, "Secret-Pass-1")
	require.NoError(t, err)
	assert.Equal(t, OrgRoleOwner, roleOf(provisioned.Id, opened.Id))

	// An organization from before the rule: its earliest operator is the owner.
	legacy, err := CreateResellerCustomer(reseller.Id, "owner-rule-legacy", "", "", 100, 1)
	require.NoError(t, err)
	for _, userId := range []int{910001, 910002} {
		require.NoError(t, DB.Create(&OrgAccount{OrgId: legacy.Id, UserId: userId, Relation: OrgRelationCustomer, Role: OrgRoleAdmin, Status: OrgStatusActive}).Error)
	}
	require.NoError(t, backfillCustomerOrgOwners())
	require.NoError(t, backfillCustomerOrgOwners())
	assert.Equal(t, OrgRoleOwner, roleOf(legacy.Id, 910001))
	assert.Equal(t, OrgRoleAdmin, roleOf(legacy.Id, 910002))
	assert.Equal(t, OrgRoleAdmin, roleOf(invited.Id, second.Id), "an organization that has an owner is left alone")

	// An organization that is not a reseller's customer keeps its roles.
	enterprise := mustCreateOrg(t, "owner-rule-enterprise", OrgTypeEnterprise, 0)
	admin := inviteAndAccept(enterprise, "owner-rule-admin")
	assert.Equal(t, OrgRoleAdmin, roleOf(enterprise.Id, admin.Id))

	// Customers are told whose customer they are: the brand, or the name.
	assert.Equal(t, "owner-rule-reseller", ResellerLabelOfCustomer(invited.Id))
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]any{"brand_name": "Northwind"}))
	assert.Equal(t, "Northwind", ResellerLabelOfCustomer(invited.Id))
	assert.Empty(t, ResellerLabelOfCustomer(enterprise.Id))
}
