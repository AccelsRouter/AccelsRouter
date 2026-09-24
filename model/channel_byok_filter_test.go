package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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

	// IsByokChannel gates admin key-view / key-use endpoints: BYOK channels are
	// recognized, platform channels are not.
	assert.True(t, IsByokChannel(userByok.Id), "user BYOK channel is recognized")
	assert.True(t, IsByokChannel(orgByok.Id), "org BYOK channel is recognized")
	assert.False(t, IsByokChannel(platform.Id), "platform channel is not BYOK")
}

// BYOK keys are encrypted at rest by the migration, and the relay read path
// decrypts transparently; platform channel keys are left untouched.
func TestEncryptExistingByokKeys(t *testing.T) {
	common.CryptoSecret = "test-crypto-secret-fixed"
	require.NoError(t, DB.AutoMigrate(&Channel{}, &UserChannel{}, &OrgChannel{}))
	DB.Exec("DELETE FROM channels")
	DB.Exec("DELETE FROM user_channels")
	DB.Exec("DELETE FROM org_channels")

	plainByok := "sk-user-upstream-secret-123"
	byok := &Channel{Name: "[BYOK] user", Key: plainByok}
	platform := &Channel{Name: "platform", Key: "sk-platform-plaintext"}
	require.NoError(t, DB.Create(byok).Error)
	require.NoError(t, DB.Create(platform).Error)
	require.NoError(t, DB.Create(&UserChannel{UserId: 1, ChannelId: byok.Id}).Error)

	require.NoError(t, EncryptExistingByokKeys())

	// At rest, the BYOK key is encrypted; the platform key is untouched.
	var storedByok, storedPlatform Channel
	require.NoError(t, DB.Where("id = ?", byok.Id).First(&storedByok).Error)
	require.NoError(t, DB.Where("id = ?", platform.Id).First(&storedPlatform).Error)
	assert.True(t, common.IsByokSecretEncrypted(storedByok.Key), "BYOK key stored encrypted")
	assert.NotContains(t, storedByok.Key, plainByok)
	assert.Equal(t, "sk-platform-plaintext", storedPlatform.Key, "platform key untouched")

	// The relay read path decrypts transparently back to the original secret.
	key, _, apiErr := storedByok.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, plainByok, key)

	// Idempotent: a second run does not double-encrypt.
	before := storedByok.Key
	require.NoError(t, EncryptExistingByokKeys())
	var after Channel
	require.NoError(t, DB.Where("id = ?", byok.Id).First(&after).Error)
	assert.Equal(t, before, after.Key, "already-encrypted key is not re-encrypted")
}
