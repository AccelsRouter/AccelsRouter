package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Organization names are unique across enterprises, distributors and their
// customers (case-insensitive, trimmed), on every path that sets one: direct
// creation, customer provisioning, applications (also against pending ones),
// approval, and rename (which may keep its own name).
func TestOrganizationNamesAreUnique(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "Acme Reseller", OrgTypeReseller, 1000)

	// Direct creation: exact, case and whitespace variants all collide.
	for _, name := range []string{"Acme Reseller", "acme reseller", "  ACME RESELLER "} {
		err := CreateOrganization(&Organization{Name: name, Type: OrgTypeEnterprise, PriceGroup: "default"})
		assert.ErrorIs(t, err, ErrOrgNameTaken, name)
	}

	// A distributor cannot provision a customer named like itself or another org.
	_, err := CreateResellerCustomer(reseller.Id, "acme reseller", "retail", 100, 1)
	assert.ErrorIs(t, err, ErrOrgNameTaken)
	cust, err := CreateResellerCustomer(reseller.Id, "Acme Customer", "retail", 100, 1)
	require.NoError(t, err)

	// Applications: an existing org's name is refused, and so is a name that
	// another pending application already claims.
	assert.ErrorIs(t, CreateOrgApplication(&OrgApplication{UserId: 9001, Type: OrgTypeEnterprise, OrgName: "ACME Customer"}), ErrOrgNameTaken)
	require.NoError(t, CreateOrgApplication(&OrgApplication{UserId: 9001, Type: OrgTypeEnterprise, OrgName: "Fresh Org"}))
	assert.ErrorIs(t, CreateOrgApplication(&OrgApplication{UserId: 9002, Type: OrgTypeEnterprise, OrgName: "fresh org"}), ErrOrgNameTaken)

	// Approval re-checks: if the name was taken meanwhile, approval fails.
	var app OrgApplication
	require.NoError(t, DB.Where("user_id = ?", 9001).First(&app).Error)
	require.NoError(t, CreateOrganization(&Organization{Name: "Fresh Org", Type: OrgTypeEnterprise, PriceGroup: "default"}))
	_, err = ApproveOrgApplication(app.Id, 1, "", "")
	assert.ErrorIs(t, err, ErrOrgNameTaken)

	// Rename: another org's name is taken; the org's own current name is not.
	taken, err := OrgNameTaken("acme customer", reseller.Id)
	require.NoError(t, err)
	assert.True(t, taken)
	taken, err = OrgNameTaken("ACME RESELLER", reseller.Id)
	require.NoError(t, err)
	assert.False(t, taken, "a rename may keep its own name")
	_ = cust
}
