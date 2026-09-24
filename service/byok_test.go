package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// A user must not be able to reset an admin-set BYOK fee to the default by
// deleting and recreating their last channel: RemoveByokGroupRatio drops only
// entries still at the default, never an admin override.
func TestByokFeeOverridePreservedOnRemove(t *testing.T) {
	originalDB := model.DB
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalFee := setting.ByokFeeRatio

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB = db
	setting.ByokFeeRatio = 0
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}

	t.Cleanup(func() {
		model.DB = originalDB
		setting.ByokFeeRatio = originalFee
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})

	// user-5 has an admin override (0.05); user-6 is a default (0) auto-entry.
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"user-5":0.05,"user-6":0}`))

	// Removing the override must NOT delete it (no fee evasion).
	require.NoError(t, RemoveByokGroupRatio("user-5"))
	assert.True(t, ratio_setting.ContainsGroupRatio("user-5"), "admin override must survive")
	assert.Equal(t, 0.05, ratio_setting.GetGroupRatio("user-5"))

	// Removing a default-valued auto-entry cleans it up.
	require.NoError(t, RemoveByokGroupRatio("user-6"))
	assert.False(t, ratio_setting.ContainsGroupRatio("user-6"), "default auto-entry is cleaned up")

	// EnsureByokGroupRatio never overwrites an existing (override) entry.
	require.NoError(t, EnsureByokGroupRatio("user-5"))
	assert.Equal(t, 0.05, ratio_setting.GetGroupRatio("user-5"), "ensure must not clobber an override")
}
