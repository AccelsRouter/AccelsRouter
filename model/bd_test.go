package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBdTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&BdCompany{}, &BdLead{}, &BdDeal{}))
	DB.Exec("DELETE FROM bd_companies")
	DB.Exec("DELETE FROM bd_leads")
	DB.Exec("DELETE FROM bd_deals")
}

func bdF(v float64) *float64 { return &v }

func TestNormalizeBdModel(t *testing.T) {
	for in, want := range map[string]string{
		"claude sonnet 4.6":   "claude sonnet 4.6",
		"Claude-Sonnet-4.6":   "claude sonnet 4.6",
		"claude.sonnet.4.6":   "claude sonnet 4.6",
		"Claude_Sonnet_4.6":   "claude sonnet 4.6",
		"  gemini   2.5-pro ": "gemini 2.5 pro",
		"deepseek-v3.2":       "deepseek v3.2",
		"qwen3.max":           "qwen3 max",
		"gpt-4.1.":            "gpt 4.1",
	} {
		assert.Equal(t, want, NormalizeBdModel(in), in)
	}
}

func TestIsBdPrepaid(t *testing.T) {
	assert.True(t, IsBdPrepaid("预付费"))
	assert.True(t, IsBdPrepaid("預付費"))
	assert.True(t, IsBdPrepaid("Prepaid"))
	assert.False(t, IsBdPrepaid("月结 30 天"))
	assert.False(t, IsBdPrepaid(""))
}

func TestBdLeadCodesPerCompany(t *testing.T) {
	setupBdTables(t)
	mk := func(side, company, m string) *BdLead {
		l := &BdLead{Side: side, Company: company, Model: m}
		require.NoError(t, SaveBdLead(l, 1))
		return l
	}
	a := mk(BdSideBuy, "Vendor A", "claude sonnet 4.6")
	b := mk(BdSideBuy, "Vendor B", "claude sonnet 4.6")
	a2 := mk(BdSideBuy, "  vendor a ", "gemini 2.5 pro")
	s := mk(BdSideSell, "Vendor A", "claude sonnet 4.6")

	assert.Equal(t, "B-001", a.Code)
	assert.Equal(t, "B-002", b.Code)
	assert.Equal(t, "B-001", a2.Code, "same company keeps its code")
	assert.Equal(t, "S-001", s.Code, "customers are numbered separately")

	// Moving a lead to another company takes that company's code.
	b.Company = "Vendor A"
	require.NoError(t, SaveBdLead(b, 2))
	assert.Equal(t, "B-001", b.Code)
	got, err := GetBdLead(b.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, got.CreatedBy, "creator is preserved on update")
	assert.Equal(t, 2, got.UpdatedBy)

	// A brand-new company continues the sequence.
	c := mk(BdSideBuy, "Vendor C", "qwen3 max")
	assert.Equal(t, "B-003", c.Code)
}

func TestBdLeadValidationAndNormalization(t *testing.T) {
	setupBdTables(t)
	err := SaveBdLead(&BdLead{Side: "x", Company: "A", Model: "m"}, 1)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))
	err = SaveBdLead(&BdLead{Side: BdSideBuy, Company: " ", Model: "m"}, 1)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))
	err = SaveBdLead(&BdLead{Side: BdSideBuy, Company: "A", Model: "m", Stage: "nope"}, 1)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))

	l := &BdLead{Side: BdSideBuy, Company: "A", Model: "Claude-Sonnet-4.6", Volume: bdF(1000),
		Currencies: []string{"USD", "BTC", "USD", "CNY"}}
	require.NoError(t, SaveBdLead(l, 1))
	got, err := GetBdLead(l.Id)
	require.NoError(t, err)
	assert.Equal(t, "claude sonnet 4.6", got.Model)
	assert.Equal(t, []string{"USD", "CNY"}, got.Currencies)
	assert.Nil(t, got.Volume, "volume is a sell-side field")
	assert.Equal(t, "contact", got.Stage)
	assert.Equal(t, "P1", got.Priority)
}

func TestBdDealRules(t *testing.T) {
	setupBdTables(t)
	// Purchase records need a supply price.
	err := SaveBdDeal(&BdDeal{Kind: BdSideBuy, Company: "A"}, 1, false)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))
	// Prepaid needs a contract amount.
	err = SaveBdDeal(&BdDeal{Kind: BdSideSell, Company: "X", Terms: "预付费"}, 1, false)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))
	err = SaveBdDeal(&BdDeal{Kind: BdSideSell, Company: "X", DealDate: "2026/10/01"}, 1, false)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))

	lead := &BdLead{Side: BdSideSell, Company: "X", Model: "claude sonnet 4.6"}
	require.NoError(t, SaveBdLead(lead, 1))

	// Without bd.manage the supplier fields are dropped on create...
	d := &BdDeal{Kind: BdSideSell, Company: "x", Model: "Claude-Sonnet-4.6", Rate: bdF(85),
		Terms: "预付费", Amount: bdF(40000), DealDate: "2026-10-01", Supplier: "A", SupplyRate: bdF(70)}
	require.NoError(t, SaveBdDeal(d, 1, false))
	assert.Equal(t, lead.Id, d.LeadId, "linked to the customer's lead")
	assert.Equal(t, "", d.Supplier)
	assert.Nil(t, d.SupplyRate)

	// ...a manager can set them...
	d.Supplier, d.SupplyRate = "Vendor A", bdF(70)
	require.NoError(t, SaveBdDeal(d, 9, true))
	// ...and a non-manager edit keeps them.
	d.Supplier, d.SupplyRate, d.Note = "Other", bdF(10), "edited"
	require.NoError(t, SaveBdDeal(d, 1, false))
	got, err := GetBdDeal(d.Id)
	require.NoError(t, err)
	assert.Equal(t, "Vendor A", got.Supplier)
	assert.Equal(t, 70.0, *got.SupplyRate)
	assert.Equal(t, "edited", got.Note)
	assert.Equal(t, 1, got.CreatedBy)

	// The supplier got a B- code when the manager assigned it.
	companies, err := ListBdCompanies()
	require.NoError(t, err)
	codes := map[string]string{}
	for _, c := range companies {
		codes[c.Side+"/"+c.Name] = c.Code
	}
	assert.Equal(t, "B-001", codes["buy/Vendor A"])
	assert.Equal(t, "S-001", codes["sell/X"])

	// Deleting the lead keeps the deal and drops the link.
	require.NoError(t, DeleteBdLead(lead.Id))
	got, err = GetBdDeal(d.Id)
	require.NoError(t, err)
	assert.Equal(t, 0, got.LeadId)

	require.NoError(t, DeleteBdDeal(d.Id))
	assert.True(t, errors.Is(DeleteBdDeal(d.Id), ErrBdNotFound))
}

func TestBdUsageByBinding(t *testing.T) {
	setupBdTables(t)
	require.NoError(t, DB.AutoMigrate(&QuotaData{}, &OrgAccount{}))
	DB.Exec("DELETE FROM quota_data")
	DB.Exec("DELETE FROM org_accounts")

	q := int(common.QuotaPerUnit) // $1
	from, to := int64(1000), int64(2000)
	require.NoError(t, DB.Create(&[]QuotaData{
		{UserID: 10, ChannelID: 1, CreatedAt: 1500, Quota: 5 * q},
		{UserID: 11, ChannelID: 2, CreatedAt: 1500, Quota: 3 * q},
		{UserID: 12, ChannelID: 3, CreatedAt: 1500, Quota: 7 * q},
		{UserID: 10, ChannelID: 1, CreatedAt: 2000, Quota: 100 * q}, // outside [from,to)
	}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: 50, UserId: 11}).Error)

	// Two leads of one supplier share the company binding; the later save wins.
	a := &BdLead{Side: BdSideBuy, Company: "Vendor A", Model: "m1", Bind: &BdBinding{ChannelIds: []int{9}}}
	require.NoError(t, SaveBdLead(a, 1))
	a2 := &BdLead{Side: BdSideBuy, Company: "vendor a", Model: "m2", Bind: &BdBinding{ChannelIds: []int{1, 2, 1}, UserIds: []int{10}}}
	require.NoError(t, SaveBdLead(a2, 1))
	b := &BdLead{Side: BdSideBuy, Company: "Vendor B", Model: "m1", Bind: &BdBinding{ChannelIds: []int{2}}}
	require.NoError(t, SaveBdLead(b, 1))
	x := &BdLead{Side: BdSideSell, Company: "X", Model: "m1", Bind: &BdBinding{UserIds: []int{10}, OrgIds: []int{50}, ChannelIds: []int{3}}}
	require.NoError(t, SaveBdLead(x, 1))
	// A save without a binding leaves it alone.
	x.Note = "n"
	require.NoError(t, SaveBdLead(x, 1))
	none := &BdLead{Side: BdSideSell, Company: "Unbound", Model: "m1"}
	require.NoError(t, SaveBdLead(none, 1))

	companies, err := ListBdCompanies()
	require.NoError(t, err)
	ids := map[string]BdCompany{}
	for _, c := range companies {
		ids[c.Side+"/"+c.Name] = c
	}
	assert.Equal(t, []int{1, 2}, ids["buy/Vendor A"].ChannelIds)
	assert.Empty(t, ids["buy/Vendor A"].UserIds, "suppliers keep channels only")
	assert.Equal(t, []int{10}, ids["sell/X"].UserIds)
	assert.Equal(t, []int{50}, ids["sell/X"].OrgIds)
	assert.Empty(t, ids["sell/X"].ChannelIds, "customers keep users and orgs only")

	u, err := GetBdUsage(from, to)
	require.NoError(t, err)
	assert.InDelta(t, 8.0, u.Companies[ids["buy/Vendor A"].Id], 1e-9)
	assert.InDelta(t, 3.0, u.Companies[ids["buy/Vendor B"].Id], 1e-9)
	assert.InDelta(t, 8.0, u.Companies[ids["sell/X"].Id], 1e-9, "user 10 + org 50's user 11")
	_, bound := u.Companies[ids["sell/Unbound"].Id]
	assert.False(t, bound, "unbound companies are absent")
	assert.InDelta(t, 8.0, u.BuyTotal, 1e-9, "channel 2 is counted once")
	assert.InDelta(t, 8.0, u.SellTotal, 1e-9)
}

func TestBdLeadMonthlyAmount(t *testing.T) {
	setupBdTables(t)
	buy := &BdLead{Side: BdSideBuy, Company: "A", Model: "m", Terms: "预付费", MonthlyAmount: bdF(50000)}
	require.NoError(t, SaveBdLead(buy, 1))
	got, err := GetBdLead(buy.Id)
	require.NoError(t, err)
	assert.Equal(t, 50000.0, *got.MonthlyAmount)

	sell := &BdLead{Side: BdSideSell, Company: "X", Model: "m", MonthlyAmount: bdF(1)}
	require.NoError(t, SaveBdLead(sell, 1))
	got, err = GetBdLead(sell.Id)
	require.NoError(t, err)
	assert.Nil(t, got.MonthlyAmount, "supplier-only field")

	err = SaveBdLead(&BdLead{Side: BdSideBuy, Company: "A", Model: "m", MonthlyAmount: bdF(-1)}, 1)
	assert.True(t, errors.Is(err, ErrBdInvalidInput))
}
