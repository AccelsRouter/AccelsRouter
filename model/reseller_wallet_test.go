package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wholesale ratio is clamped to the sane (0,1] range; anything else means
// "no discount" (1.0).
func TestEffectiveWholesaleRatio(t *testing.T) {
	assert.Equal(t, 1.0, (&Organization{WholesaleRatio: 0}).EffectiveWholesaleRatio(), "unset = no discount")
	assert.Equal(t, 1.0, (&Organization{WholesaleRatio: 1.5}).EffectiveWholesaleRatio(), ">1 is nonsensical = no discount")
	assert.Equal(t, 1.0, (&Organization{WholesaleRatio: -0.2}).EffectiveWholesaleRatio(), "negative = no discount")
	assert.Equal(t, 0.85, (&Organization{WholesaleRatio: 0.85}).EffectiveWholesaleRatio())
	assert.Equal(t, 1.0, (&Organization{WholesaleRatio: 1.0}).EffectiveWholesaleRatio())
}

// A self-service purchase atomically debits the buyer's personal quota and
// credits the reseller wallet, can never overspend the personal balance, and
// records one purchase-ledger row.
func TestPurchaseResellerCredit(t *testing.T) {
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&User{}))
	DB.Exec("DELETE FROM users")

	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 0)
	buyer := &User{Username: "reseller-admin", Quota: 900, AffCode: "rs-adm"}
	require.NoError(t, DB.Create(buyer).Error)

	// Buy 1000 credit at wholesale 0.85 ⇒ cost 850. Buyer has 900 ⇒ succeeds.
	require.NoError(t, PurchaseResellerCredit(reseller.Id, buyer.Id, 1000, 850, "trade-1", "buy"))

	var freshUser User
	require.NoError(t, DB.Where("id = ?", buyer.Id).First(&freshUser).Error)
	assert.Equal(t, 50, freshUser.Quota, "personal quota debited by cost")
	fresh, err := GetOrganizationById(reseller.Id)
	require.NoError(t, err)
	assert.Equal(t, 1000, fresh.WalletQuota, "wallet credited by full credit amount (the wholesale margin)")

	var ledgerCount int64
	require.NoError(t, DB.Model(&CreditLedger{}).Where("to_org_id = ? AND type = ?", reseller.Id, LedgerTypePurchase).Count(&ledgerCount).Error)
	assert.Equal(t, int64(1), ledgerCount)

	// Insufficient personal balance fails closed: no debit, no wallet change.
	err = PurchaseResellerCredit(reseller.Id, buyer.Id, 1000, 1000, "trade-2", "buy")
	require.Error(t, err)
	require.NoError(t, DB.Where("id = ?", buyer.Id).First(&freshUser).Error)
	assert.Equal(t, 50, freshUser.Quota, "failed purchase must not debit")
	fresh, _ = GetOrganizationById(reseller.Id)
	assert.Equal(t, 1000, fresh.WalletQuota, "failed purchase must not credit the wallet")

	// Non-positive amounts are rejected.
	require.Error(t, PurchaseResellerCredit(reseller.Id, buyer.Id, 0, 0, "t", "x"))
}
