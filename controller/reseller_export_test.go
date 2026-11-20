package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The customer export has one row for each model a customer can call, with
// the customer's own columns merged over those rows.
func TestCustomersWorkbookHasOneRowPerModel(t *testing.T) {
	unit := int(common.QuotaPerUnit)
	customers := []*model.ResellerCustomer{
		{
			Org:          &model.Organization{Id: 7, Name: "=Acme", Status: "active", WalletQuota: 3 * unit, CreatedTime: 1795000000},
			NetAllocated: 5 * unit, OwnerEmail: "owner@acme.test", Members: 2,
			Models: []model.ResellerCustomerModel{{Name: "gpt-4o", Discount: 0.85}, {Name: "claude-sonnet-5", Discount: 1}},
		},
		{
			Org: &model.Organization{Id: 8, Name: "Direct Co", Status: "active", FundingMode: model.OrgFundingDirect, CreatedTime: 1795000000},
		},
		{
			Org:    &model.Organization{Id: 9, Name: "Solo", Status: "suspended", CreatedTime: 1795000000},
			Models: []model.ResellerCustomerModel{{Name: "deepseek-chat", Discount: 0.4}},
		},
	}
	book, err := customersWorkbook(customers, map[int]int64{7: int64(unit) / 2})
	require.NoError(t, err)
	defer book.Close()

	rows, err := book.GetRows("Customers")
	require.NoError(t, err)
	require.Len(t, rows, 5)
	assert.Equal(t, []string{
		"Customer", "ID", "Funding", "Status", "Owner email", "Members", "Wallet balance (USD)",
		"Net allocated (USD)", "Paid for calls (USD)", "Created", "Model", "Discount (1 = list price)",
	}, rows[0])
	// A name that looks like a formula is kept as text.
	assert.Equal(t, []string{"=Acme", "7", "allowance from you", "active", "owner@acme.test", "2", "3", "5", "0.5", "2026-11-18", "gpt-4o", "0.85"}, rows[1])
	assert.Equal(t, []string{"claude-sonnet-5", "1"}, rows[2][10:])
	// A customer that tops up directly has nothing allocated, and one that can
	// call nothing still has its row.
	assert.Equal(t, []string{"Direct Co", "8", "tops up directly", "active", "", "0", "0", "", "0", "2026-11-18"}, rows[3])
	assert.Equal(t, []string{"deepseek-chat", "0.4"}, rows[4][10:])
	formula, err := book.GetCellFormula("Customers", "A2")
	require.NoError(t, err)
	assert.Empty(t, formula)

	merged, err := book.GetMergeCells("Customers")
	require.NoError(t, err)
	ranges := make([]string, 0, len(merged))
	for _, cell := range merged {
		ranges = append(ranges, cell.GetStartAxis()+":"+cell.GetEndAxis())
	}
	assert.ElementsMatch(t, []string{"A2:A3", "B2:B3", "C2:C3", "D2:D3", "E2:E3", "F2:F3", "G2:G3", "H2:H3", "I2:I3", "J2:J3"}, ranges)
}
