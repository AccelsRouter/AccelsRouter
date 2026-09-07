package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// backfillResellerAdmins converts a pre-decoupling reseller owner (recorded as
// an OrgAccount) into a ResellerAdmin link and frees its single-payer slot, so
// the user can then also join an enterprise org. It must be idempotent and must
// never touch enterprise OrgAccounts.
func TestBackfillResellerAdmins(t *testing.T) {
	migrateOrgTables(t)

	reseller := mustCreateOrg(t, "legacy-reseller", OrgTypeReseller, 0)
	enterprise := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)

	// Legacy state: the reseller admin holds an OrgAccount (the pre-decoupling
	// shape); an enterprise member holds a normal OrgAccount.
	require.NoError(t, DB.Create(&OrgAccount{OrgId: reseller.Id, UserId: 100, Role: OrgRoleOwner, Status: OrgStatusActive}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: enterprise.Id, UserId: 200, Role: OrgRoleOwner, Status: OrgStatusActive}).Error)

	require.NoError(t, backfillResellerAdmins())

	// The reseller admin is now a link, and its payer slot is freed.
	adminOrg, err := GetResellerAdminOrg(100)
	require.NoError(t, err)
	require.NotNil(t, adminOrg)
	assert.Equal(t, reseller.Id, adminOrg.Id)

	var resellerAccounts int64
	require.NoError(t, DB.Model(&OrgAccount{}).Where("user_id = ?", 100).Count(&resellerAccounts).Error)
	assert.Equal(t, int64(0), resellerAccounts, "reseller OrgAccount must be removed")

	// The enterprise member is untouched.
	isAdmin, err := IsResellerAdmin(200)
	require.NoError(t, err)
	assert.False(t, isAdmin, "enterprise member must not become a reseller admin")
	var entAccounts int64
	require.NoError(t, DB.Model(&OrgAccount{}).Where("user_id = ?", 200).Count(&entAccounts).Error)
	assert.Equal(t, int64(1), entAccounts, "enterprise OrgAccount must be preserved")

	// Idempotent: a second run creates no duplicate link and changes nothing.
	require.NoError(t, backfillResellerAdmins())
	var links int64
	require.NoError(t, DB.Model(&ResellerAdmin{}).Where("reseller_org_id = ?", reseller.Id).Count(&links).Error)
	assert.Equal(t, int64(1), links, "backfill must not duplicate links")
}
