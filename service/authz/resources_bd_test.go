package authz

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The business summary is opt-in: admins get nothing by default, root gets
// everything, and a per-user grant opens exactly the granted actions.
func TestBdPermissionsAreOptInPerAdmin(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	var found bool
	for _, resource := range Catalog() {
		if resource.Resource != ResourceBd {
			continue
		}
		found = true
		assert.Equal(t, "Business Summary", resource.LabelKey)
		actions := map[string]bool{}
		for _, action := range resource.Actions {
			actions[action.Action] = true
			assert.Empty(t, action.DefaultRoles, action.Action)
		}
		assert.Equal(t, map[string]bool{ActionRead: true, ActionWrite: true, ActionManage: true}, actions)
	}
	require.True(t, found)

	for _, p := range []Permission{BdRead, BdWrite, BdManage} {
		assert.False(t, Can(2, common.RoleAdminUser, p), p.Action)
		assert.True(t, Can(1, common.RoleRootUser, p), p.Action)
		assert.False(t, Can(3, common.RoleCommonUser, p), p.Action)
	}

	require.NoError(t, SetUserPermissions(2, PermissionsMap{ResourceBd: {ActionRead: true, ActionWrite: true}}))
	assert.True(t, Can(2, common.RoleAdminUser, BdRead))
	assert.True(t, Can(2, common.RoleAdminUser, BdWrite))
	assert.False(t, Can(2, common.RoleAdminUser, BdManage))
	assert.False(t, Can(4, common.RoleAdminUser, BdRead), "grants are per admin")
}
