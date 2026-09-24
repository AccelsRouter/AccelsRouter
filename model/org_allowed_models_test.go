package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The model allow-list round-trips through storage, treats empty/blank/invalid
// as unrestricted (nil), and de-duplicates on write.
func TestAllowedModelsSerialization(t *testing.T) {
	stored, err := MarshalAllowedModels([]string{"gpt-4o", " claude-3.5 ", "gpt-4o", "", "  "})
	require.NoError(t, err)
	assert.NotEmpty(t, stored)

	org := &Organization{AllowedModels: stored}
	set := org.AllowedModelSet()
	require.NotNil(t, set)
	assert.True(t, set["gpt-4o"])
	assert.True(t, set["claude-3.5"], "values are trimmed")
	assert.Len(t, set, 2, "duplicates and blanks dropped")
	assert.ElementsMatch(t, []string{"gpt-4o", "claude-3.5"}, org.AllowedModelList())

	// Empty list serializes to "" and reads back as unrestricted (nil).
	empty, err := MarshalAllowedModels([]string{"", "  "})
	require.NoError(t, err)
	assert.Equal(t, "", empty)
	assert.Nil(t, (&Organization{AllowedModels: empty}).AllowedModelSet())
	assert.Nil(t, (&Organization{AllowedModels: "[]"}).AllowedModelSet())
	assert.Nil(t, (&Organization{AllowedModels: "not json"}).AllowedModelSet(), "invalid = unrestricted, never accidental lockout")
}

// GetOrgPayerInfo surfaces the customer org's allow-list to the hot path, and
// SetOrgAllowedModels updates it (and is read back correctly).
func TestOrgPayerInfoAllowedModels(t *testing.T) {
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&OrgAccount{}))

	org := mustCreateOrg(t, "cust", OrgTypeEnterprise, 0)
	stored, err := MarshalAllowedModels([]string{"gpt-4o"})
	require.NoError(t, err)
	require.NoError(t, SetOrgAllowedModels(org.Id, stored))

	require.NoError(t, DB.Create(&OrgAccount{OrgId: org.Id, UserId: 4242, Relation: OrgRelationCustomer, Role: OrgRoleAdmin, Status: OrgStatusActive}).Error)
	InvalidateOrgPayerCache(4242)

	info, err := GetOrgPayerInfo(4242)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, info.AllowedModels)
	assert.True(t, info.AllowedModels["gpt-4o"])
	assert.False(t, info.AllowedModels["claude-3.5"], "an unassigned model is not allowed")

	// Clearing the list makes it unrestricted again.
	require.NoError(t, SetOrgAllowedModels(org.Id, ""))
	InvalidateOrgPayerCache(4242)
	info, err = GetOrgPayerInfo(4242)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Nil(t, info.AllowedModels, "empty allow-list = unrestricted")
}
