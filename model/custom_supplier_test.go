package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestParseSupplierMoney(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  SupplierMoney
		ok    bool
	}{
		{"0", 0, true},
		{"500000", 50000000, true},
		{"12.5", 1250, true},
		{"12.05", 1205, true},
		{"12.", 0, false},
		{"12.345", 0, false},
		{"-1", 0, false},
		{"1e3", 0, false},
		{"", 0, false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseSupplierMoney(tc.input)
			if !tc.ok {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	encoded, err := common.Marshal(SupplierMoney(1205))
	require.NoError(t, err)
	assert.Equal(t, `"12.05"`, string(encoded))
}

func TestSupplierMissingFieldsAndLowBalance(t *testing.T) {
	yes := true
	discount := 75.0
	prepaid, remaining := SupplierMoney(50000000), SupplierMoney(6200000)
	complete := Supplier{LegalName: "L", ShortName: "S", BizContact: "c", Status: SupplierStatusActive, Owner: "o", SignEntity: "sg", GstApplicable: &yes, GstRate: &discount}
	models := []SupplierModel{{ModelName: "m", Variant: "@AWS", Source: "cloud_resale", DiscountPct: &discount}}
	contract := SupplierContract{EndDate: "2027-03-14", AutoRenew: &yes, NoticePeriod: "30d", Currency: "USD", PayMode: "prepaid", PrepaidAmount: &prepaid, RemainingAmount: &remaining, LowBalancePct: 20}

	assert.Empty(t, SupplierMissingFields(&complete, models, &contract))
	assert.True(t, contract.LowBalance(), "12.4% remaining is under the 20% threshold")

	gstUnanswered := complete
	gstUnanswered.GstApplicable = nil
	assert.Equal(t, []string{"gst_applicable", "models", "contract"}, SupplierMissingFields(&gstUnanswered, nil, nil))

	prepaidWithoutAmounts := contract
	prepaidWithoutAmounts.PrepaidAmount, prepaidWithoutAmounts.RemainingAmount = nil, nil
	incompleteRow := []SupplierModel{{ModelName: "m"}}
	assert.Equal(t,
		[]string{"models.1.variant", "models.1.source", "models.1.discount_pct", "contract.prepaid_amount", "contract.remaining_amount"},
		SupplierMissingFields(&complete, incompleteRow, &prepaidWithoutAmounts))
	assert.False(t, prepaidWithoutAmounts.LowBalance())
}

var supplierTestTables = []any{&Supplier{}, &SupplierModel{}, &SupplierContract{}, &SupplierContractSeq{}, &SupplierAttachment{}, &SupplierModelSearchLog{}}

// TestSupplierDatabaseMatrix covers migrations, contract numbering, voiding
// and model comparison on every supported database.
func TestSupplierDatabaseMatrix(t *testing.T) {
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
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "supplier_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(supplierTestTables...))
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.Migrator().DropTable(supplierTestTables...))
			// Running the migration twice proves it is idempotent.
			require.NoError(t, db.AutoMigrate(supplierTestTables...))
			require.NoError(t, db.AutoMigrate(supplierTestTables...))

			runSupplierContractScenario(t)
			runSupplierCompareScenario(t)
		})
	}
}

func runSupplierContractScenario(t *testing.T) {
	yes := true
	discount := 75.0
	supplier, err := CreateSupplier(&Supplier{LegalName: "Example Ltd", ShortName: "Example", GstApplicable: &yes}, []SupplierModel{
		{ModelName: "claude-opus-5", Variant: "@AWS", Source: "cloud_resale", DiscountPct: &discount, Enabled: true},
		{},
	}, 7)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("V%03d", supplier.Id), supplier.Code)
	assert.Equal(t, SupplierStatusPotential, supplier.Status)

	year := time.Now().In(supplierContractZone).Year() % 100
	preview, err := PreviewSupplierContractNo(supplier.Id, "GW", SupplierContractFramework, 0)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("ACL-P-GW-FA-%02d-0001", year), preview)

	framework, err := CreateSupplierContract(supplier.Id, &SupplierContract{BusinessLine: "GW", ContractType: SupplierContractFramework, PayMode: "prepaid"}, 7)
	require.NoError(t, err)
	assert.Equal(t, preview, framework.ContractNo)
	assert.True(t, framework.IsCurrent, "the first contract becomes current")
	assert.EqualValues(t, defaultLowBalancePct, framework.LowBalancePct)

	single, err := CreateSupplierContract(supplier.Id, &SupplierContract{BusinessLine: "MA", ContractType: SupplierContractSingle}, 7)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("ACL-P-MA-SA-%02d-0002", year), single.ContractNo)
	assert.False(t, single.IsCurrent)

	order, err := CreateSupplierContract(supplier.Id, &SupplierContract{BusinessLine: "CO", ContractType: SupplierContractOrder, ParentContractId: framework.Id}, 7)
	require.NoError(t, err)
	assert.Equal(t, framework.ContractNo+"-O01", order.ContractNo, "orders take the framework number and do not use the main counter")
	assert.Equal(t, "GW", order.BusinessLine, "orders inherit the framework business line")

	_, err = CreateSupplierContract(supplier.Id, &SupplierContract{ContractType: SupplierContractOrder, ParentContractId: single.Id}, 7)
	assert.Error(t, err, "an order must belong to a framework agreement")

	_, err = VoidSupplierContract(framework.Id, "wrong line", 7)
	assert.Error(t, err, "a framework with active orders cannot be voided")
	_, err = VoidSupplierContract(order.Id, "duplicate", 7)
	require.NoError(t, err)
	voided, err := VoidSupplierContract(framework.Id, "wrong line", 7)
	require.NoError(t, err)
	assert.False(t, voided.IsCurrent)
	_, _, err = UpdateSupplierContract(framework.Id, &SupplierContract{EndDate: "2027-01-01"}, 7)
	assert.Error(t, err, "a voided contract cannot be changed")

	replacement, err := CreateSupplierContract(supplier.Id, &SupplierContract{BusinessLine: "GW", ContractType: SupplierContractFramework}, 7)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("ACL-P-GW-FA-%02d-0003", year), replacement.ContractNo, "voided numbers are never reused")
	assert.True(t, replacement.IsCurrent, "the supplier had no current contract after the void")

	_, err = SetCurrentSupplierContract(single.Id, 7)
	require.NoError(t, err)
	var currentIDs []int
	require.NoError(t, DB.Model(&SupplierContract{}).Where("supplier_id = ? AND is_current = ?", supplier.Id, true).Pluck("id", &currentIDs).Error)
	assert.Equal(t, []int{single.Id}, currentIDs)

	prepaid, remaining := SupplierMoney(20000000), SupplierMoney(12800000)
	_, updated, err := UpdateSupplierContract(single.Id, &SupplierContract{
		BusinessLine: "TL", EndDate: "2027-01-09", AutoRenew: &yes, NoticePeriod: "15d", Currency: "USDT",
		PayMode: "prepaid", PrepaidAmount: &prepaid, RemainingAmount: &remaining, LowBalancePct: 70,
	}, 7)
	require.NoError(t, err)
	assert.Equal(t, "MA", updated.BusinessLine, "the business line is fixed once numbered")
	assert.True(t, updated.LowBalance())

	detail, err := GetSupplierDetail(supplier.Id)
	require.NoError(t, err)
	assert.Len(t, detail.Models, 1, "blank model rows are dropped")
	assert.Len(t, detail.Contracts, 4)
	require.NotNil(t, detail.Contracts[2].PrepaidAmount)
	assert.Equal(t, prepaid, *detail.Contracts[2].PrepaidAmount, "amounts survive the database round trip")
	require.NotNil(t, detail.GstRate)
	assert.EqualValues(t, 9, *detail.GstRate, "charging GST stores the 9% rate")

	items, total, err := SearchSuppliers("OPUS", "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "the keyword matches model names case-insensitively")
	require.Len(t, items, 1)
	assert.True(t, items[0].LowBalance)
	require.NotNil(t, items[0].Current)
	assert.Equal(t, single.ContractNo, items[0].Current.ContractNo)
}

func runSupplierCompareScenario(t *testing.T) {
	cheap, dear := 55.0, 80.0
	_, err := CreateSupplier(&Supplier{ShortName: "Relay", Status: SupplierStatusTrial}, []SupplierModel{
		{ModelName: "gpt-5.6-sol", Source: "third_party_relay", DiscountPct: &cheap, Enabled: true},
	}, 7)
	require.NoError(t, err)
	_, err = CreateSupplier(&Supplier{ShortName: "Cloud", Status: SupplierStatusActive}, []SupplierModel{
		{ModelName: "gpt-5.6-sol", Source: "cloud_resale", DiscountPct: &dear, Enabled: true},
		{ModelName: "gpt-5.4-nano", Source: "cloud_resale", DiscountPct: &dear, Enabled: false},
	}, 7)
	require.NoError(t, err)
	_, err = CreateSupplier(&Supplier{ShortName: "Gone", Status: SupplierStatusTerminated}, []SupplierModel{
		{ModelName: "gpt-5.6-sol", DiscountPct: &cheap, Enabled: true},
	}, 7)
	require.NoError(t, err)

	require.NoError(t, RecordSupplierModelSearch(1, "gpt-5.6-sol"))
	require.NoError(t, RecordSupplierModelSearch(1, "gpt-5.6-sol"))
	require.NoError(t, RecordSupplierModelSearch(2, "gpt-5.6-sol"))

	catalog, err := SupplierModelCatalog("", 30, 10)
	require.NoError(t, err)
	require.NotEmpty(t, catalog)
	assert.Equal(t, "gpt-5.6-sol", catalog[0].ModelName)
	assert.Equal(t, 2, catalog[0].SearchCount, "repeat searches by one user on one day count once")
	assert.Equal(t, 2, catalog[0].SupplierCount, "terminated suppliers are excluded")
	for _, item := range catalog {
		assert.NotEqual(t, "gpt-5.4-nano", item.ModelName, "disabled model rows are not offered")
	}

	offers, err := CompareSupplierModel("gpt-5.6-sol", "discount")
	require.NoError(t, err)
	require.Len(t, offers, 2)
	assert.Equal(t, "Relay", offers[0].SupplierName)
	assert.True(t, offers[0].Best)
	assert.False(t, offers[1].Best)
}
