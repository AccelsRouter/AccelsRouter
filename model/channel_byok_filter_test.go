package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Personal/org BYOK channels are user-owned private upstreams that only live in
// the channels table to reuse routing/billing. The admin channel list/search
// must exclude them, keyed off the ownership tables (not name/group strings).
func TestExcludeByokChannels(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}, &UserChannel{}, &OrgChannel{}))
	DB.Exec("DELETE FROM channels")
	DB.Exec("DELETE FROM user_channels")
	DB.Exec("DELETE FROM org_channels")

	platform := &Channel{Name: "platform-openai"}
	userByok := &Channel{Name: "[BYOK] user-1"}
	orgByok := &Channel{Name: "[BYOK] org-9"}
	require.NoError(t, DB.Create(platform).Error)
	require.NoError(t, DB.Create(userByok).Error)
	require.NoError(t, DB.Create(orgByok).Error)

	require.NoError(t, DB.Create(&UserChannel{UserId: 1, ChannelId: userByok.Id}).Error)
	require.NoError(t, DB.Create(&OrgChannel{OrgId: 9, ChannelId: orgByok.Id}).Error)

	var got []*Channel
	require.NoError(t, ExcludeByokChannels(DB.Model(&Channel{})).Find(&got).Error)

	ids := make([]int, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.Id)
	}
	assert.Equal(t, []int{platform.Id}, ids, "only the platform channel survives the admin-list filter")
}
