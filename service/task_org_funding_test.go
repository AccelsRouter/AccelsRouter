package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// An org-funded async task must settle against the wallets the submission
// reserved from: the customer's org wallet at the retail discount and the
// reseller's wallet at the wholesale ratio — never the member's own balance.
func TestTaskAdjustFundingSettlesOrgWallets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Organization{}, &model.OrgAccount{}, &model.Workspace{}, &model.OrgUsageDaily{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, common.RedisEnabled = db, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "member", Quota: 500}).Error)
	require.NoError(t, db.Create(&model.Organization{Id: 1, Name: "customer", Type: model.OrgTypeEnterprise, Status: model.OrgStatusActive, WalletQuota: 10000}).Error)
	require.NoError(t, db.Create(&model.Organization{Id: 2, Name: "reseller", Type: model.OrgTypeReseller, Status: model.OrgStatusActive, WalletQuota: 10000}).Error)

	task := &model.Task{UserId: 7, Quota: 1000, SubmitTime: 1_700_000_000}
	task.Properties.OriginModelName = "seedance-2-5"
	task.PrivateData.BillingSource = BillingSourceOrgWallet
	task.PrivateData.OrgFunding = &model.TaskOrgFunding{
		OrgId: 1, UserId: 7, DiscountRatio: 0.5,
		ResellerOrgId: 2, WholesaleRatio: 0.8,
		Consumed: 500, ResellerConsumed: 800,
	}

	walletOf := func(id int) int {
		var org model.Organization
		require.NoError(t, db.First(&org, id).Error)
		return org.WalletQuota
	}
	userQuota := func() int {
		var user model.User
		require.NoError(t, db.First(&user, 7).Error)
		return user.Quota
	}
	rollup := func() model.OrgUsageDaily {
		var rows []model.OrgUsageDaily
		require.NoError(t, db.Find(&rows).Error)
		require.Len(t, rows, 1, "one bucket row for the submission hour")
		return rows[0]
	}

	// Overshoot: actual cost exceeded the reservation by 100 standard units.
	require.NoError(t, taskAdjustFunding(task, 100))
	assert.Equal(t, 10000-50, walletOf(1), "customer pays the delta at retail")
	assert.Equal(t, 10000-80, walletOf(2), "reseller pays the delta at wholesale")
	assert.Equal(t, 500, userQuota(), "member balance untouched")
	// The usage rollup moves with the wallets, in the submission's hour bucket,
	// without counting another request.
	row := rollup()
	assert.Equal(t, int64(1_700_000_000-1_700_000_000%3600), row.DayBucket)
	assert.Equal(t, "seedance-2-5", row.ModelName)
	assert.Equal(t, 2, row.ResellerOrgId)
	assert.Equal(t, int64(100), row.StandardQuota)
	assert.Equal(t, int64(50), row.ChargedQuota)
	assert.Equal(t, int64(80), row.CostQuota)
	assert.Equal(t, int64(0), row.Requests)

	// Full refund of a failed task returns the whole reservation.
	require.NoError(t, taskAdjustFunding(task, -1000))
	assert.Equal(t, 10000-50+500, walletOf(1))
	assert.Equal(t, 10000-80+800, walletOf(2))
	assert.Equal(t, 500, userQuota(), "member balance untouched")
	row = rollup()
	assert.Equal(t, int64(100-1000), row.StandardQuota)
	assert.Equal(t, int64(50-500), row.ChargedQuota)
	assert.Equal(t, int64(80-800), row.CostQuota)

	// A reseller's OWN task carries no reseller id in its snapshot (single
	// debit at the wholesale ratio), but its rollup row is reseller traffic
	// whose cost is what it paid, exactly as the submission recorded it.
	require.NoError(t, db.Where("1 = 1").Delete(&model.OrgUsageDaily{}).Error)
	own := &model.Task{UserId: 7, Quota: 1000, SubmitTime: 1_700_000_000}
	own.Properties.OriginModelName = "seedance-2-5"
	own.PrivateData.BillingSource = BillingSourceOrgWallet
	own.PrivateData.OrgFunding = &model.TaskOrgFunding{OrgId: 2, UserId: 7, DiscountRatio: 0.8, Consumed: 800}
	require.NoError(t, taskAdjustFunding(own, -1000))
	row = rollup()
	assert.Equal(t, 2, row.ResellerOrgId, "own traffic is tagged with the reseller itself")
	assert.Equal(t, int64(-1000), row.StandardQuota)
	assert.Equal(t, int64(-800), row.ChargedQuota)
	assert.Equal(t, int64(-800), row.CostQuota, "cost follows the refund like the submission's cost did")
}
