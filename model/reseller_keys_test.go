package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reseller admin holds no OrgAccount, yet its own keys must be reseller
// traffic on the hot path: the payer record points at the reseller org itself
// (routing + offerable-model cap), and a suspended or removed admin loses it.
func TestOrgPayerInfoForResellerAdmin(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 0)
	stored, err := MarshalAllowedModels([]string{"claude-opus-4-8"})
	require.NoError(t, err)
	require.NoError(t, SetOrgAllowedModels(reseller.Id, stored))
	require.NoError(t, DB.Create(&ResellerAdmin{UserId: 7100, ResellerOrgId: reseller.Id, Status: OrgStatusActive}).Error)
	InvalidateOrgPayerCache(7100)

	info, err := GetOrgPayerInfo(7100)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, reseller.Id, info.OrgId)
	assert.Equal(t, OrgTypeReseller, info.OrgType)
	assert.Equal(t, reseller.Id, info.ResellerOrgId, "own keys route as this reseller's traffic")
	assert.True(t, info.AllowedModels["claude-opus-4-8"], "capped by the reseller's offerable models")
	assert.False(t, info.AllowedModels["gpt-4o"])

	require.NoError(t, SetResellerAdminStatus(reseller.Id, 7100, OrgStatusSuspended))
	info, err = GetOrgPayerInfo(7100)
	require.NoError(t, err)
	assert.Nil(t, info, "a suspended admin link carries no payer record")

	// An unrelated user still has no payer.
	info, err = GetOrgPayerInfo(7199)
	require.NoError(t, err)
	assert.Nil(t, info)
}

// A key bound to the RESELLER org's own workspace bills the reseller wallet at
// the reseller's wholesale price: the billing info carries the org type and the
// reseller's own wholesale map, with no second (customer-style) debit target.
func TestGetWorkspaceBillingInfoForResellerOwnKey(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 1000)
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]interface{}{
		"wholesale_ratios": `{"claude-opus-4-8":0.8}`,
	}))
	ws := &Workspace{OrgId: reseller.Id, Name: "Default"}
	require.NoError(t, CreateWorkspace(ws))
	require.NoError(t, BindTokenToWorkspace(reseller.Id, ws.Id, 7301))

	info, err := GetWorkspaceBillingInfo(7301)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, OrgTypeReseller, info.OrgType)
	assert.Equal(t, reseller.Id, info.OrgId)
	assert.Zero(t, info.ResellerOrgId, "the reseller's own call has no separate reseller debit")
	assert.Equal(t, 0.8, WholesaleRatioFor("claude-opus-4-8", ParseRetailDiscounts(info.WholesaleRatios)))
	assert.Equal(t, 1.0, WholesaleRatioFor("gpt-4o", ParseRetailDiscounts(info.WholesaleRatios)), "unmatched model = full standard")
}

// Reseller parties consume only through organization keys: their personal
// (unbound) keys are disabled, org-bound keys untouched, and the startup sweep
// covers every kind of party while leaving ordinary users alone.
func TestDisablePersonalTokensForResellerParties(t *testing.T) {
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&Token{}))
	DB.Exec("DELETE FROM tokens")
	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 0)
	customer := mustCreateOrg(t, "customer", OrgTypeEnterprise, 0)
	enterprise := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	require.NoError(t, DB.Create(&ResellerCustomerLink{ResellerOrgId: reseller.Id, CustomerOrgId: customer.Id}).Error)
	require.NoError(t, DB.Create(&ResellerAdmin{UserId: 7401, ResellerOrgId: reseller.Id, Status: OrgStatusActive}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: customer.Id, UserId: 7402, Role: OrgRoleOwner, Relation: OrgRelationCustomer, Status: OrgStatusActive}).Error)
	require.NoError(t, DB.Create(&OrgAccount{OrgId: enterprise.Id, UserId: 7403, Role: OrgRoleOwner, Relation: OrgRelationMember, Status: OrgStatusActive}).Error)

	ws := &Workspace{OrgId: reseller.Id, Name: "Default"}
	require.NoError(t, CreateWorkspace(ws))
	mk := func(userId int, key string) *Token {
		tok := &Token{UserId: userId, Name: key, Key: key, Status: common.TokenStatusEnabled}
		require.NoError(t, DB.Create(tok).Error)
		return tok
	}
	adminPersonal := mk(7401, "admin-personal")
	adminOrgKey := mk(7401, "admin-org")
	require.NoError(t, BindTokenToWorkspace(reseller.Id, ws.Id, adminOrgKey.Id))
	customerPersonal := mk(7402, "customer-personal")
	enterprisePersonal := mk(7403, "enterprise-personal")

	n, err := DisablePersonalTokens(7401)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = DisablePersonalTokens(7401)
	require.NoError(t, err)
	assert.Zero(t, n, "idempotent")

	require.NoError(t, DisableResellerPartiesPersonalTokens())

	status := func(id int) int {
		var tok Token
		require.NoError(t, DB.First(&tok, id).Error)
		return tok.Status
	}
	assert.Equal(t, common.TokenStatusDisabled, status(adminPersonal.Id), "reseller admin personal key disabled")
	assert.Equal(t, common.TokenStatusEnabled, status(adminOrgKey.Id), "reseller org key untouched")
	assert.Equal(t, common.TokenStatusDisabled, status(customerPersonal.Id), "customer user personal key disabled")
	assert.Equal(t, common.TokenStatusEnabled, status(enterprisePersonal.Id), "enterprise member is not a reseller party")
}

// The reseller console must show the reseller's OWN key traffic alongside its
// customers': call records include tokens bound to the reseller org (paid
// overlay = wholesale), and the usage report counts rows billed to the reseller
// org itself — whether tagged with reseller_org_id or only with org_id.
func TestResellerConsoleIncludesOwnKeyTraffic(t *testing.T) {
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&Log{}, &OrgUsageDaily{}))
	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 1000)
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]interface{}{
		"wholesale_ratios": `{"claude-opus-4-8":0.8}`,
	}))
	customer, err := CreateResellerCustomer(reseller.Id, "customer-one", "retail", 400, 1)
	require.NoError(t, err)

	ownWs := &Workspace{OrgId: reseller.Id, Name: "Default"}
	require.NoError(t, CreateWorkspace(ownWs))
	custWs := &Workspace{OrgId: customer.Id, Name: "prod"}
	require.NoError(t, CreateWorkspace(custWs))
	require.NoError(t, BindTokenToWorkspace(reseller.Id, ownWs.Id, 7501))
	require.NoError(t, BindTokenToWorkspace(customer.Id, custWs.Id, 7502))

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&Log{UserId: 7601, Type: LogTypeConsume, TokenId: 7501, ModelName: "claude-opus-4-8", Quota: 1000, CreatedAt: now}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 7602, Type: LogTypeConsume, TokenId: 7502, ModelName: "claude-opus-4-8", Quota: 500, CreatedAt: now}).Error)

	logs, total, err := ListResellerLogs(reseller.Id, now-10, now+10, 0, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	byToken := map[int]*Log{}
	for _, l := range logs {
		byToken[l.TokenId] = l
	}
	require.Contains(t, byToken, 7501, "the reseller's own key row is listed")
	assert.Equal(t, "reseller", byToken[7501].CustomerName)
	assert.Equal(t, 0.8, byToken[7501].RetailRatio, "own row's paid overlay is the wholesale ratio")
	assert.Equal(t, 800, byToken[7501].RetailQuota)
	assert.Equal(t, "customer-one", byToken[7502].CustomerName)

	// Rollup: an own-key row written before own calls were tagged (reseller_org_id
	// 0, org_id = reseller) and one written after (tagged) both count once.
	require.NoError(t, RecordOrgUsageDaily(now, reseller.Id, ownWs.Id, 0, 7601, "claude-opus-4-8", 1000, 800, 0, 10, 20))
	require.NoError(t, RecordOrgUsageDaily(now, reseller.Id, ownWs.Id, reseller.Id, 7601, "deepseek-v4-pro", 300, 300, 300, 5, 5))
	require.NoError(t, RecordOrgUsageDaily(now, customer.Id, custWs.Id, reseller.Id, 7602, "claude-opus-4-8", 500, 500, 400, 7, 7))
	report, err := GetResellerUsageFromDaily(reseller.Id, now-10, now+10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, report.TotalRequests)
	assert.EqualValues(t, 1800, report.TotalQuota, "own (1000+300) + customer (500) standard")
	assert.EqualValues(t, 700, report.TotalCostQuota, "own tagged cost (300) + customer wholesale (400)")
}
