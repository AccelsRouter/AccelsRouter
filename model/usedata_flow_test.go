package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func seedFlowQuotaData(t *testing.T, quotaData QuotaData) {
	t.Helper()
	require.NoError(t, DB.Create(&quotaData).Error)
}

func seedFlowLookupData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "west"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, DB.Delete(&Token{Id: 11}).Error)
}

func TestGetFlowQuotaDataUsesQuotaDataRoleSpecificDimensions(t *testing.T) {
	truncateTables(t)
	seedFlowLookupData(t)

	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 1,
		CreatedAt: 1000,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 1,
		CreatedAt: 1100,
		Count:     1,
		Quota:     50,
		TokenUsed: 20,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 2,
		CreatedAt: 1200,
		Count:     1,
		Quota:     25,
		TokenUsed: 10,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "default",
		ModelName: "gpt-b",
		ChannelID: 1,
		CreatedAt: 1300,
		Count:     3,
		Quota:     70,
		TokenUsed: 30,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		ModelName: "legacy",
		CreatedAt: 1400,
		Count:     99,
		Quota:     999,
		TokenUsed: 999,
	})

	rootRows, err := GetFlowQuotaData(900, 2000, "", 0, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, rootRows, 3)
	// Token 11 was soft-deleted, so its name is intentionally left empty for the
	// frontend to render a localized "deleted (id)" label instead.
	require.Equal(t, FlowQuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		TokenName:   "",
		UseGroup:    "vip",
		ChannelID:   1,
		ChannelName: "east",
		ModelName:   "gpt-a",
		TokenUsed:   60,
		Count:       3,
		Quota:       150,
	}, *rootRows[0])
	// A token that still exists resolves to its current name.
	require.Equal(t, 22, rootRows[1].TokenID)
	require.Equal(t, "backup", rootRows[1].TokenName)

	adminRows, err := GetFlowQuotaData(900, 2000, "alice", 0, common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, adminRows, 2)
	require.Equal(t, 0, adminRows[0].TokenID)
	require.Empty(t, adminRows[0].TokenName)
	require.Empty(t, adminRows[0].NodeName)
	require.Equal(t, "alice", adminRows[0].Username)
	require.Equal(t, "vip", adminRows[0].UseGroup)
	require.Equal(t, "east", adminRows[0].ChannelName)
	require.Equal(t, 150, adminRows[0].Quota)

	selfRows, err := GetFlowQuotaData(900, 2000, "", 1, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, selfRows, 1)
	require.Empty(t, selfRows[0].Username)
	require.Equal(t, 0, selfRows[0].ChannelID)
	require.Empty(t, selfRows[0].ChannelName)
	require.Empty(t, selfRows[0].TokenName)
	require.Equal(t, "vip", selfRows[0].UseGroup)
	require.Equal(t, 175, selfRows[0].Quota)
}

func TestLogQuotaDataSplitsRowsByUseGroupTokenChannelAndNode(t *testing.T) {
	truncateTables(t)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3661,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     100,
		TokenUsed: 40,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     50,
		TokenUsed: 20,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "default",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     25,
		TokenUsed: 10,
	})

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Order("quota DESC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, int64(3600), rows[0].CreatedAt)
	require.Equal(t, "vip", rows[0].UseGroup)
	require.Equal(t, 11, rows[0].TokenID)
	require.Equal(t, 1, rows[0].ChannelID)
	require.Equal(t, "node-a", rows[0].NodeName)
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 150, rows[0].Quota)
	require.Equal(t, 60, rows[0].TokenUsed)
	require.Equal(t, "default", rows[1].UseGroup)
	require.Equal(t, 25, rows[1].Quota)
}

// A dev-only mock task (upstream id "cgt-mock-…") is logged for the tester but
// never enters the quota_data rollup that reconciliation and the dashboard
// read, because nothing was consumed upstream.
func TestMockTaskBillingLogSkipsQuotaData(t *testing.T) {
	truncateTables(t)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM quota_data")
	})
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	previousExport, previousConsume := common.DataExportEnabled, common.LogConsumeEnabled
	common.DataExportEnabled, common.LogConsumeEnabled = true, true
	t.Cleanup(func() { common.DataExportEnabled, common.LogConsumeEnabled = previousExport, previousConsume })

	task := &Task{TaskID: "pub-1"}
	task.PrivateData.UpstreamTaskID = MockTaskIDPrefix + "1791956920-26"
	require.True(t, task.IsMock())
	require.False(t, (&Task{TaskID: "cgt-20261028-1"}).IsMock())

	for _, mock := range []bool{true, false} {
		RecordTaskBillingLog(RecordTaskBillingLogParams{
			UserId: 1, LogType: LogTypeConsume, ChannelId: 1, ModelName: "seedance", Quota: 100,
			Other: NewLogOther(), SkipQuotaData: mock,
		})
	}
	SaveQuotaDataCache()

	var logCount int64
	require.NoError(t, DB.Model(&Log{}).Where("model_name = ?", "seedance").Count(&logCount).Error)
	require.Equal(t, int64(2), logCount, "both tasks are logged")
	var rows []QuotaData
	require.NoError(t, DB.Where("model_name = ?", "seedance").Find(&rows).Error)
	require.Len(t, rows, 1, "only the real task reaches quota_data")
	require.Equal(t, 100, rows[0].Quota)
}

// Rows of different usage units never merge, so a model's seconds and its
// tokens stay separately summable.
func TestLogQuotaDataKeepsUsageUnitsApart(t *testing.T) {
	truncateTables(t)
	t.Cleanup(func() { DB.Exec("DELETE FROM quota_data") })
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	base := QuotaDataLogParams{UserID: 1, Username: "alice", ModelName: "seedance", CreatedAt: 7200, UseGroup: "default", ChannelID: 1, NodeName: "n"}
	for _, p := range []struct {
		unit   string
		amount int64
		quota  int
	}{{UsageUnitSecond, 10, 100}, {UsageUnitSecond, 5, 50}, {"", 0, 20}, {UsageUnitImage, 2, 30}} {
		params := base
		params.UsageUnit, params.UsageAmount, params.Quota = p.unit, p.amount, p.quota
		LogQuotaData(params)
	}
	SaveQuotaDataCache()
	// A second flush increments the persisted rows instead of duplicating them.
	params := base
	params.UsageUnit, params.UsageAmount, params.Quota = UsageUnitSecond, 1, 10
	LogQuotaData(params)
	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Where("model_name = ?", "seedance").Order("usage_unit asc").Find(&rows).Error)
	require.Len(t, rows, 3)
	require.Equal(t, "", rows[0].UsageUnit)
	require.Equal(t, 20, rows[0].Quota)
	require.Equal(t, UsageUnitImage, rows[1].UsageUnit)
	require.Equal(t, int64(2), rows[1].UsageAmount)
	require.Equal(t, UsageUnitSecond, rows[2].UsageUnit)
	require.Equal(t, int64(16), rows[2].UsageAmount)
	require.Equal(t, 160, rows[2].Quota)
	require.Equal(t, 3, rows[2].Count)
}

// quotaDataLegacy is the quota_data shape of the last release, without the
// usage columns, so the upgrade path (ADD COLUMN) is exercised on each engine.
type quotaDataLegacy struct {
	Id                  int    `json:"id"`
	UserID              int    `json:"user_id" gorm:"index"`
	Username            string `json:"username" gorm:"size:64;default:''"`
	ModelName           string `json:"model_name" gorm:"size:64;default:''"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint"`
	UseGroup            string `json:"use_group" gorm:"size:64;default:''"`
	TokenID             int    `json:"token_id" gorm:"default:0"`
	ChannelID           int    `json:"channel_id" gorm:"default:0"`
	NodeName            string `json:"node_name" gorm:"size:64;default:''"`
	TokenUsed           int    `json:"token_used" gorm:"default:0"`
	Count               int    `json:"count" gorm:"default:0"`
	Quota               int    `json:"quota" gorm:"default:0"`
	CacheReadTokens     int64  `json:"cache_read_tokens" gorm:"default:0"`
	CacheCreationTokens int64  `json:"cache_creation_tokens" gorm:"default:0"`
}

func (quotaDataLegacy) TableName() string { return "quota_data" }

// The usage columns migrate onto an existing quota_data table on every engine
// (idempotently), legacy rows read as "no unit", and the upsert and the
// reconciliation aggregates work with the unit in their keys.
func TestQuotaDataUsageColumnsDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			// The rollup code addresses quota_data by its bare name, so the
			// table cannot be prefixed; refuse to touch a database that already
			// holds one rather than drop someone's data.
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			if db.Migrator().HasTable("quota_data") {
				require.NoError(t, sqlDB.Close())
				t.Skip("quota_data already exists in the test database; point TEST_*_DSN at an empty database")
			}
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&QuotaData{}))
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				require.NoError(t, sqlDB.Close())
			})
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			// Upgrade path: a last-release table with a row, migrated twice.
			require.NoError(t, db.AutoMigrate(&quotaDataLegacy{}))
			require.NoError(t, db.Create(&quotaDataLegacy{UserID: 1, Username: "a", ModelName: "gpt", CreatedAt: 3600, ChannelID: 7, Count: 1, Quota: 10, TokenUsed: 5}).Error)
			require.False(t, db.Migrator().HasColumn(&QuotaData{}, "usage_unit"))
			for range 2 {
				require.NoError(t, db.AutoMigrate(&QuotaData{}))
			}
			require.True(t, db.Migrator().HasColumn(&QuotaData{}, "usage_unit"))
			require.True(t, db.Migrator().HasColumn(&QuotaData{}, "usage_amount"))
			var legacy QuotaData
			require.NoError(t, db.Where("model_name = ?", "gpt").First(&legacy).Error)
			require.Equal(t, "", legacy.UsageUnit)
			require.Equal(t, int64(0), legacy.UsageAmount)

			// Upsert keyed by unit, flushed twice.
			CacheQuotaDataLock.Lock()
			CacheQuotaData = make(map[string]*QuotaData)
			CacheQuotaDataLock.Unlock()
			base := QuotaDataLogParams{UserID: 1, Username: "a", ModelName: "seedance", CreatedAt: 3600, UseGroup: "default", ChannelID: 7, NodeName: "n"}
			for _, p := range []struct {
				unit   string
				amount int64
				quota  int
			}{{UsageUnitSecond, 10, 100}, {"", 0, 20}} {
				params := base
				params.UsageUnit, params.UsageAmount, params.Quota = p.unit, p.amount, p.quota
				LogQuotaData(params)
			}
			SaveQuotaDataCache()
			params := base
			params.UsageUnit, params.UsageAmount, params.Quota = UsageUnitSecond, 5, 50
			LogQuotaData(params)
			SaveQuotaDataCache()
			var rows []QuotaData
			require.NoError(t, db.Where("model_name = ?", "seedance").Order("usage_unit asc").Find(&rows).Error)
			require.Len(t, rows, 2)
			require.Equal(t, UsageUnitSecond, rows[1].UsageUnit)
			require.Equal(t, int64(15), rows[1].UsageAmount)
			require.Equal(t, 150, rows[1].Quota)

			// Reconciliation aggregates group by unit on this engine.
			matrix, err := reconMatrixRows(0, 7200)
			require.NoError(t, err)
			require.Equal(t, []ReconMatrixRow{
				{Model: "seedance", ChannelId: 7, Channel: "#7", Quota: 170, Requests: 3, Tokens: 0, UsageUnit: UsageUnitSecond, UsageAmount: 15},
				{Model: "gpt", ChannelId: 7, Channel: "#7", Quota: 10, Requests: 1, Tokens: 5},
			}, matrix)
			byModel, err := reconByColumn(0, 7200, "model_name")
			require.NoError(t, err)
			require.Equal(t, []ReconRow{
				{Key: "seedance", Quota: 170, Requests: 3, Tokens: 0, UsageUnit: UsageUnitSecond, UsageAmount: 15},
				{Key: "gpt", Quota: 10, Requests: 1, Tokens: 5},
			}, byModel)
		})
	}
}
