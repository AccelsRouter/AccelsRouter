package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBdLeadOpensSupplier checks that a new purchase lead creates a supplier
// record of the same name once, and that sales leads and later edits do not.
func TestBdLeadOpensSupplier(t *testing.T) {
	setupBdTables(t)
	require.NoError(t, DB.AutoMigrate(&Supplier{}, &SupplierModel{}, &SupplierChannel{}, &SupplierContractModel{}))
	DB.Exec("DELETE FROM suppliers")
	DB.Exec("DELETE FROM supplier_models")
	owner := User{Username: "bd_lead_owner", DisplayName: "Lead Owner", Password: "x", Role: common.RoleAdminUser, AffCode: "bdlead1"}
	require.NoError(t, DB.Create(&owner).Error)
	t.Cleanup(func() { DB.Unscoped().Delete(&owner) })

	lead := &BdLead{Side: BdSideBuy, Company: "Relay Works", Stage: "contact", OwnerId: owner.Id, Models: []BdLeadModel{
		{Model: "qwen3 max", ModelClass: "official", PriceRate: bdF(75)},
		{Model: "m"},
	}}
	require.NoError(t, SaveBdLead(lead, 3))

	var suppliers []Supplier
	require.NoError(t, DB.Where("legal_name = ?", "Relay Works").Find(&suppliers).Error)
	require.Len(t, suppliers, 1)
	s := suppliers[0]
	assert.Equal(t, "Relay Works", s.ShortName)
	assert.Equal(t, SupplierStatusPotential, s.Status)
	assert.Equal(t, owner.Id, s.OwnerId)
	assert.Equal(t, "Lead Owner", s.Owner)
	assert.Equal(t, 3, s.CreatedBy)
	assert.Regexp(t, `^V\d{3,}$`, s.Code)
	var models []SupplierModel
	require.NoError(t, DB.Where("supplier_id = ?", s.Id).Order("sort").Find(&models).Error)
	require.Len(t, models, 2)
	assert.Equal(t, "qwen3 max", models[0].ModelName)
	assert.Equal(t, "official", models[0].Variant)
	require.NotNil(t, models[0].DiscountPct)
	assert.InDelta(t, 75, *models[0].DiscountPct, 0.001)
	assert.Nil(t, models[1].DiscountPct)

	// The same company again (any case), an edit, and a sales lead add nothing.
	mkBdLead(t, BdSideBuy, " relay works ", "m")
	lead.Stage = "quote"
	require.NoError(t, SaveBdLead(lead, 3))
	mkBdLead(t, BdSideSell, "Customer Only", "m")
	var count int64
	require.NoError(t, DB.Model(&Supplier{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	// A supplier created first opens the lead, and its lead does not loop back.
	_, err := CreateSupplier(&Supplier{ShortName: "Archive First"}, []SupplierModel{{ModelName: "m"}}, 3)
	require.NoError(t, err)
	var leads []BdLead
	require.NoError(t, DB.Where("side = ? AND company = ?", BdSideBuy, "Archive First").Find(&leads).Error)
	require.Len(t, leads, 1)
	require.NoError(t, DB.Model(&Supplier{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}
