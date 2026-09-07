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
	// free (GetOrgPayerInfo stays nil).
	adminOrg, err := GetResellerAdminOrg(100)
	require.NoError(t, err)
	require.NotNil(t, adminOrg)
	assert.Equal(t, org.Id, adminOrg.Id)

	info, err := GetOrgPayerInfo(100)
	require.NoError(t, err)
	assert.Nil(t, info, "reseller admin must not occupy the single-payer slot")

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
