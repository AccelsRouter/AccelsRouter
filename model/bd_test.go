package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBdTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&BdCompany{}, &BdLead{}, &BdLeadModel{}, &BdAllocation{}))
	DB.Exec("DELETE FROM bd_companies")
	DB.Exec("DELETE FROM bd_leads")
	DB.Exec("DELETE FROM bd_lead_models")
	DB.Exec("DELETE FROM bd_allocations")
}

func bdF(v float64) *float64 { return &v }

func bdModels(names ...string) []BdLeadModel {
	out := make([]BdLeadModel, len(names))
	for i, n := range names {
		out[i] = BdLeadModel{Model: n}
	}
	return out
}

func mkBdLead(t *testing.T, side, company string, models ...string) *BdLead {
	t.Helper()
	l := &BdLead{Side: side, Company: company, Models: bdModels(models...)}
	require.NoError(t, SaveBdLead(l, 1))
	return l
}

func TestBdModelKey(t *testing.T) {
	for _, in := range []string{"claude sonnet 4.6", "Claude-Sonnet-4.6", "claude.sonnet.4.6", "Claude_Sonnet_4.6", " claude  sonnet-4.6 "} {
		assert.Equal(t, "claudesonnet46", BdModelKey(in), in)
	}
	assert.NotEqual(t, BdModelKey("gpt-4o"), BdModelKey("gpt-4o-mini"))
	assert.Equal(t, "gpt-4o", cleanBdModel("  gpt-4o "), "names are kept as typed")
}

func TestBdLeadCodesPerCompany(t *testing.T) {
	setupBdTables(t)
	a := mkBdLead(t, BdSideBuy, "Vendor A", "claude sonnet 4.6")
	b := mkBdLead(t, BdSideBuy, "Vendor B", "claude sonnet 4.6")
	a2 := mkBdLead(t, BdSideBuy, "  vendor a ", "gemini 2.5 pro")
	s := mkBdLead(t, BdSideSell, "Vendor A", "claude sonnet 4.6")

	assert.Equal(t, "B-001", a.Code)
	assert.Equal(t, "B-002", b.Code)
	assert.Equal(t, "B-001", a2.Code, "same company keeps its code")
	assert.Equal(t, "S-001", s.Code, "customers are numbered separately")

	b.Company = "Vendor A"
	require.NoError(t, SaveBdLead(b, 2))
	assert.Equal(t, "B-001", b.Code)
	got, err := GetBdLead(b.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, got.CreatedBy, "creator is preserved on update")
	assert.Equal(t, 2, got.UpdatedBy)
}

func TestBdLeadModels(t *testing.T) {
	setupBdTables(t)
	assert.True(t, errors.Is(SaveBdLead(&BdLead{Side: "x", Company: "A", Models: bdModels("m")}, 1), ErrBdInvalidInput))
	assert.True(t, errors.Is(SaveBdLead(&BdLead{Side: BdSideBuy, Company: " ", Models: bdModels("m")}, 1), ErrBdInvalidInput))
	assert.True(t, errors.Is(SaveBdLead(&BdLead{Side: BdSideBuy, Company: "A"}, 1), ErrBdInvalidInput), "at least one model")
	assert.True(t, errors.Is(SaveBdLead(&BdLead{Side: BdSideBuy, Company: "A", Models: bdModels(" ", "")}, 1), ErrBdInvalidInput))
	assert.True(t, errors.Is(SaveBdLead(&BdLead{Side: BdSideBuy, Company: "A", Models: bdModels("Claude-Sonnet-4.6", "claude sonnet 4.6")}, 1), ErrBdInvalidInput), "duplicate after normalization")

	assert.True(t, errors.Is(SaveBdLead(&BdLead{Side: BdSideBuy, Company: "A", Models: []BdLeadModel{{Model: "m", PriceRate: bdF(300)}}}, 1), ErrBdInvalidInput))

	l := &BdLead{Side: BdSideBuy, Company: "A", Currencies: []string{"USD", "BTC", "USD", "CNY"},
		Models: []BdLeadModel{{Model: "Claude-Sonnet-4.6", ModelClass: "@AWS", PriceRate: bdF(70)}, {Model: "claude sonnet 4.6", ModelClass: "@Azure"}, {Model: "gemini 2.5 pro"}}}
	require.NoError(t, SaveBdLead(l, 1))
	got, err := GetBdLead(l.Id)
	require.NoError(t, err)
	require.Len(t, got.Models, 3)
	assert.Equal(t, "Claude-Sonnet-4.6", got.Models[0].Model, "kept as typed")
	assert.Equal(t, "@AWS", got.Models[0].ModelClass)
	assert.Equal(t, 70.0, *got.Models[0].PriceRate)
	assert.Nil(t, got.Models[1].PriceRate)

	c := &BdLead{Side: BdSideSell, Company: "C", Models: []BdLeadModel{{Model: "m", PriceRate: bdF(80)}}}
	require.NoError(t, SaveBdLead(c, 1))
	gotC, err := GetBdLead(c.Id)
	require.NoError(t, err)
	assert.Empty(t, gotC.Models, "customers carry no models")
	require.NoError(t, SaveBdLead(&BdLead{Side: BdSideSell, Company: "D"}, 1), "a customer needs no model")
	assert.Equal(t, "gemini 2.5 pro", got.Models[2].Model, "order is kept")
	assert.Equal(t, []string{"USD", "CNY"}, got.Currencies)
	assert.Equal(t, "contact", got.Stage)

	// Editing replaces the model list.
	l.Models = bdModels("qwen3 max")
	require.NoError(t, SaveBdLead(l, 1))
	got, err = GetBdLead(l.Id)
	require.NoError(t, err)
	require.Len(t, got.Models, 1)
	assert.Equal(t, "qwen3 max", got.Models[0].Model)
	leads, err := ListBdLeads()
	require.NoError(t, err)
	assert.Len(t, leads, 3)
}

func TestBdAllocations(t *testing.T) {
	setupBdTables(t)
	sup := mkBdLead(t, BdSideBuy, "Vendor A", "claude sonnet 4.6", "gemini 2.5 pro")
	cus := mkBdLead(t, BdSideSell, "X", "claude sonnet 4.6")

	bad := []*BdAllocation{
		{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "claude sonnet 4.6", SupplyRate: bdF(70)},                    // no sale price
		{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "claude sonnet 4.6", SaleRate: bdF(85)},                      // no supply price
		{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "qwen3 max", SupplyRate: bdF(70), SaleRate: bdF(85)},         // not offered
		{CustomerLeadId: sup.Id, SupplierLeadId: sup.Id, Model: "claude sonnet 4.6", SupplyRate: bdF(70), SaleRate: bdF(85)}, // wrong side
		{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "claude sonnet 4.6", SupplyRate: bdF(70), SaleRate: bdF(85), AllocDate: "2026/10/01"},
	}
	for i, a := range bad {
		assert.True(t, errors.Is(SaveBdAllocation(a, 1), ErrBdInvalidInput), i)
	}

	a := &BdAllocation{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "Claude-Sonnet-4.6",
		SupplyRate: bdF(70), SaleRate: bdF(85), AllocDate: "2026-10-01"}
	require.NoError(t, SaveBdAllocation(a, 1))
	assert.Equal(t, "claude sonnet 4.6", a.Model, "stored with the supplier's spelling")
	assert.Equal(t, "Vendor A", a.Supplier)
	assert.Equal(t, "X", a.Customer)

	// The same supplier model goes to a customer only once.
	dup := &BdAllocation{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "claude_sonnet_4.6", SupplyRate: bdF(60), SaleRate: bdF(80)}
	assert.True(t, errors.Is(SaveBdAllocation(dup, 1), ErrBdInvalidInput))
	// A different model of that supplier is fine.
	other := &BdAllocation{CustomerLeadId: cus.Id, SupplierLeadId: sup.Id, Model: "gemini 2.5 pro", SupplyRate: bdF(60), SaleRate: bdF(80)}
	require.NoError(t, SaveBdAllocation(other, 2))

	// Editing keeps the creator.
	a.SaleRate = bdF(88)
	require.NoError(t, SaveBdAllocation(a, 3))
	got, err := GetBdAllocation(a.Id)
	require.NoError(t, err)
	assert.Equal(t, 88.0, *got.SaleRate)
	assert.Equal(t, 1, got.CreatedBy)

	// Allocated models and leads are protected.
	sup.Models = bdModels("gemini 2.5 pro")
	assert.True(t, errors.Is(SaveBdLead(sup, 1), ErrBdInvalidInput), "allocated model cannot be removed")
	sup.Models = bdModels("Claude-Sonnet-4.6", "gemini 2.5 pro")
	require.NoError(t, SaveBdLead(sup, 1), "respelling an allocated model is fine")
	assert.True(t, errors.Is(DeleteBdLead(cus.Id), ErrBdInvalidInput))
	assert.True(t, errors.Is(DeleteBdLead(sup.Id), ErrBdInvalidInput))

	// Renaming a lead updates the snapshot.
	cus.Company = "X Corp"
	require.NoError(t, SaveBdLead(cus, 1))
	got, err = GetBdAllocation(a.Id)
	require.NoError(t, err)
	assert.Equal(t, "X Corp", got.Customer)

	// Leads on hold take no new allocations.
	held := mkBdLead(t, BdSideSell, "Held", "claude sonnet 4.6")
	held.Stage = "hold"
	require.NoError(t, SaveBdLead(held, 1))
	assert.True(t, errors.Is(SaveBdAllocation(&BdAllocation{CustomerLeadId: held.Id, SupplierLeadId: sup.Id, Model: "claude sonnet 4.6", SupplyRate: bdF(1), SaleRate: bdF(2)}, 1), ErrBdInvalidInput))

	require.NoError(t, DeleteBdAllocation(a.Id))
	require.NoError(t, DeleteBdAllocation(other.Id))
	assert.True(t, errors.Is(DeleteBdAllocation(a.Id), ErrBdNotFound))
	require.NoError(t, DeleteBdLead(cus.Id))
	_, err = GetBdLead(cus.Id)
	assert.True(t, errors.Is(err, ErrBdNotFound))
}
