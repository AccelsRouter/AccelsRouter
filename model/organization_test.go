package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrateOrgTables makes the org schema available on the shared in-memory
// test DB (TestMain lives in task_cas_test.go).
func migrateOrgTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(
		&Organization{}, &OrgAccount{}, &CreditLedger{}, &Workspace{}, &WorkspaceToken{}, &OrgChannel{},
		&OrgApplication{}, &OrgInvitation{}, &OrgSsoDomain{}, &OrgAuditLog{}, &ResellerCustomerLink{},
		&ResellerAdmin{},
	))
	// isolate between tests
	DB.Exec("DELETE FROM organizations")
	DB.Exec("DELETE FROM org_accounts")
	DB.Exec("DELETE FROM credit_ledgers")
	DB.Exec("DELETE FROM workspaces")
	DB.Exec("DELETE FROM workspace_tokens")
	DB.Exec("DELETE FROM org_applications")
	DB.Exec("DELETE FROM org_invitations")
	DB.Exec("DELETE FROM org_sso_domains")
	DB.Exec("DELETE FROM org_audit_logs")
	DB.Exec("DELETE FROM reseller_customer_links")
	DB.Exec("DELETE FROM reseller_admins")
}

// The audit trail is append-only, scoped per org, and returned newest-first.
func TestOrgAuditLog(t *testing.T) {
	migrateOrgTables(t)
	orgA := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	orgB := mustCreateOrg(t, "other", OrgTypeEnterprise, 0)

	RecordOrgAudit(orgA.Id, 1, "workspace.create", "workspace:1", "prod")
	RecordOrgAudit(orgA.Id, 2, "credit.allocate", "org:9", "quota=100")
	RecordOrgAudit(orgB.Id, 3, "org.update", "org:2", "name=x")
	RecordOrgAudit(0, 1, "ignored", "", "") // invalid org id → no row

	rows, total, err := ListOrgAuditLogs(orgA.Id, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total, "only orgA's rows, invalid ignored")
	require.Len(t, rows, 2)
	assert.Equal(t, "credit.allocate", rows[0].Action, "newest first")
	assert.Equal(t, "workspace.create", rows[1].Action)

	// Another org's trail is isolated.
	rowsB, totalB, err := ListOrgAuditLogs(orgB.Id, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalB)
	require.Len(t, rowsB, 1)
	assert.Equal(t, "org.update", rowsB[0].Action)
}

func mustCreateOrg(t *testing.T, name, typ string, wallet int) *Organization {
	t.Helper()
	org := &Organization{Name: name, Type: typ, OwnerUserId: 1}
	require.NoError(t, CreateOrganization(org))
	if wallet > 0 {
		require.NoError(t, PlatformCreditOrg(org.Id, wallet, 1, "test-credit", "seed"))
	}
	fresh, err := GetOrganizationById(org.Id)
	require.NoError(t, err)
	return fresh
}

// The org wallet is the payer for consolidated billing: reserves must be
// atomic and an insufficient balance must never mutate anything.
func TestOrgWalletReserve(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 100)

	ok, err := TryReserveOrgQuota(org.Id, 60)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = TryReserveOrgQuota(org.Id, 41) // only 40 left
	require.NoError(t, err)
	assert.False(t, ok, "insufficient balance must not reserve")

	fresh, _ := GetOrganizationById(org.Id)
	assert.Equal(t, 40, fresh.WalletQuota, "failed reserve must not change the wallet")

	// A suspended org must not reserve even with balance.
	require.NoError(t, UpdateOrganizationFields(org.Id, map[string]interface{}{"status": OrgStatusSuspended}))
	ok, err = TryReserveOrgQuota(org.Id, 1)
	require.NoError(t, err)
	assert.False(t, ok)

	// Negative amounts are rejected outright.
	_, err = TryReserveOrgQuota(org.Id, -5)
	require.Error(t, err)
}

// Budgets gate reserves hard, record settle-overshoot unconditionally, and
// roll over between periods.
func TestOrgAccountBudget(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	require.NoError(t, AttachOrgAccount(&OrgAccount{
		OrgId: org.Id, UserId: 42, Relation: OrgRelationMember, Role: OrgRoleMember, MonthlyBudget: 100,
	}))

	ok, err := AddOrgAccountSpend(org.Id, 42, 80, true)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = AddOrgAccountSpend(org.Id, 42, 30, true) // 80+30 > 100
	require.NoError(t, err)
	assert.False(t, ok, "budget must gate the reserve path")

	// Settle overshoot records past the budget instead of dropping the debt.
	ok, err = AddOrgAccountSpend(org.Id, 42, 30, false)
	require.NoError(t, err)
	assert.True(t, ok)

	var acc OrgAccount
	require.NoError(t, DB.Where("org_id = ? AND user_id = ?", org.Id, 42).First(&acc).Error)
	assert.Equal(t, 110, acc.PeriodSpend)

	// Refund path floors at zero.
	require.NoError(t, ReduceOrgAccountSpend(org.Id, 42, 500))
	require.NoError(t, DB.Where("org_id = ? AND user_id = ?", org.Id, 42).First(&acc).Error)
	assert.Equal(t, 0, acc.PeriodSpend)

	// Period rollover resets the counter and re-opens the budget.
	require.NoError(t, DB.Model(&OrgAccount{}).Where("user_id = ?", 42).
		Updates(map[string]interface{}{"period_key": "200001", "period_spend": 999}).Error)
	ok, err = AddOrgAccountSpend(org.Id, 42, 10, true)
	require.NoError(t, err)
	assert.True(t, ok, "a new period must reset the counter")
	require.NoError(t, DB.Where("org_id = ? AND user_id = ?", org.Id, 42).First(&acc).Error)
	assert.Equal(t, 10, acc.PeriodSpend)
}

// Ledger transfers are all-or-nothing and bounded by the source's unconsumed
// balance; every movement appends exactly one ledger row.
func TestTransferOrgCredit(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "reseller", OrgTypeReseller, 100)
	customer := mustCreateOrg(t, "customer", OrgTypeEnterprise, 0)

	require.NoError(t, TransferOrgCredit(reseller.Id, customer.Id, 60, 1, LedgerTypeAllocate, ""))
	r, _ := GetOrganizationById(reseller.Id)
	c, _ := GetOrganizationById(customer.Id)
	assert.Equal(t, 40, r.WalletQuota)
	assert.Equal(t, 60, c.WalletQuota)

	// Over-allocation fails atomically: neither wallet changes.
	err := TransferOrgCredit(reseller.Id, customer.Id, 41, 1, LedgerTypeAllocate, "")
	require.Error(t, err)
	r, _ = GetOrganizationById(reseller.Id)
	c, _ = GetOrganizationById(customer.Id)
	assert.Equal(t, 40, r.WalletQuota)
	assert.Equal(t, 60, c.WalletQuota)

	// Revoke is bounded by the target's unconsumed balance.
	err = TransferOrgCredit(customer.Id, reseller.Id, 61, 1, LedgerTypeRevoke, "")
	require.Error(t, err, "revoke may only reclaim unconsumed balance")
	require.NoError(t, TransferOrgCredit(customer.Id, reseller.Id, 60, 1, LedgerTypeRevoke, ""))

	// Guards.
	require.Error(t, TransferOrgCredit(reseller.Id, reseller.Id, 1, 1, LedgerTypeAllocate, ""))
	require.Error(t, TransferOrgCredit(reseller.Id, customer.Id, 0, 1, LedgerTypeAllocate, ""))
	require.Error(t, TransferOrgCredit(reseller.Id, customer.Id, 1, 1, "consume", ""))

	var count int64
	DB.Model(&CreditLedger{}).Count(&count)
	assert.Equal(t, int64(3), count, "seed purchase + allocate + revoke")
}

// UNIQUE(user_id): one account has exactly one payer, ever.
func TestOrgAccountUniquePayer(t *testing.T) {
	migrateOrgTables(t)
	a := mustCreateOrg(t, "org-a", OrgTypeEnterprise, 0)
	b := mustCreateOrg(t, "org-b", OrgTypeReseller, 0)

	require.NoError(t, AttachOrgAccount(&OrgAccount{OrgId: a.Id, UserId: 7, Relation: OrgRelationMember, Role: OrgRoleMember}))
	err := AttachOrgAccount(&OrgAccount{OrgId: b.Id, UserId: 7, Relation: OrgRelationCustomer, Role: OrgRoleMember})
	require.Error(t, err, "a second payer for the same user must be rejected by the schema")

	// Payer resolution reflects attach/detach through cache invalidation.
	info, err := GetOrgPayerInfo(7)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, a.Id, info.OrgId)

	require.NoError(t, DetachOrgAccount(a.Id, 7))
	info, err = GetOrgPayerInfo(7)
	require.NoError(t, err)
	assert.Nil(t, info, "detached user pays for itself again")
}

// Workspace budgets mirror member budgets and bind tokens uniquely.
func TestWorkspaceBudgetAndBinding(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	ws := &Workspace{OrgId: org.Id, Name: "prod", MonthlyBudget: 50}
	require.NoError(t, CreateWorkspace(ws))

	ok, err := AddWorkspaceSpend(ws.Id, 50, true)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = AddWorkspaceSpend(ws.Id, 1, true)
	require.NoError(t, err)
	assert.False(t, ok, "workspace budget must gate")

	// Unbound tokens (workspaceId 0) are a no-op gate.
	ok, err = AddWorkspaceSpend(0, 100, true)
	require.NoError(t, err)
	assert.True(t, ok)

	// Token binding is unique; rebinding moves the token.
	require.NoError(t, BindTokenToWorkspace(org.Id, ws.Id, 900))
	ws2 := &Workspace{OrgId: org.Id, Name: "staging"}
	require.NoError(t, CreateWorkspace(ws2))
	require.NoError(t, BindTokenToWorkspace(org.Id, ws2.Id, 900))
	got, err := GetTokenWorkspaceId(900)
	require.NoError(t, err)
	assert.Equal(t, ws2.Id, got)

	require.NoError(t, UnbindTokenFromWorkspace(900))
	got, err = GetTokenWorkspaceId(900)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

// Key-level billing: a request bills the org only when its TOKEN is bound to
// an org workspace; an unbound token bills personally (nil). Suspension of the
// org or workspace surfaces so the billing path can abort.
func TestGetWorkspaceBillingInfo(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 100)
	ws := &Workspace{OrgId: org.Id, Name: "prod"}
	require.NoError(t, CreateWorkspace(ws))

	// Unbound token → personal billing.
	info, err := GetWorkspaceBillingInfo(555)
	require.NoError(t, err)
	assert.Nil(t, info, "an unbound token must bill personally")

	// Bound token → org billing info.
	require.NoError(t, BindTokenToWorkspace(org.Id, ws.Id, 555))
	info, err = GetWorkspaceBillingInfo(555)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, org.Id, info.OrgId)
	assert.Equal(t, ws.Id, info.WorkspaceId)
	assert.Equal(t, OrgStatusActive, info.OrgStatus)

	// Suspending the org surfaces on the billing info (fresh status read).
	require.NoError(t, UpdateOrganizationFields(org.Id, map[string]interface{}{"status": OrgStatusSuspended}))
	info, err = GetWorkspaceBillingInfo(555)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, OrgStatusSuspended, info.OrgStatus)

	// A deleted workspace makes the binding stale → personal billing again.
	require.NoError(t, UpdateOrganizationFields(org.Id, map[string]interface{}{"status": OrgStatusActive}))
	require.NoError(t, DeleteWorkspace(ws.Id))
	info, err = GetWorkspaceBillingInfo(555)
	require.NoError(t, err)
	assert.Nil(t, info, "a stale binding (deleted workspace) falls back to personal")
}

// The per-seat member budget is optional: a token owner who is not a member
// of the workspace's org has no cap and is allowed.
func TestMemberSpendTolerantOfNonMember(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	ok, err := AddOrgAccountSpend(org.Id, 4242, 10, true) // no OrgAccount row
	require.NoError(t, err)
	assert.True(t, ok, "no member row ⇒ no per-seat cap ⇒ allowed")
}

// A reseller provisions and funds customer orgs; the customer relationship is
// ledger-derived and authorization is bounded to actual customers.
func TestResellerCustomers(t *testing.T) {
	migrateOrgTables(t)
	reseller := mustCreateOrg(t, "acme-reseller", OrgTypeReseller, 1000)
	enterprise := mustCreateOrg(t, "not-a-reseller", OrgTypeEnterprise, 1000)

	// Only a reseller can create customers.
	_, err := CreateResellerCustomer(enterprise.Id, "cust-x", "default", 100, 1)
	require.Error(t, err)

	// Initial allocation must be positive and within the reseller's wallet.
	_, err = CreateResellerCustomer(reseller.Id, "cust-x", "retail", 0, 1)
	require.Error(t, err)
	_, err = CreateResellerCustomer(reseller.Id, "cust-x", "retail", 5000, 1)
	require.Error(t, err, "over-wallet allocation must fail")

	// Happy path: customer org created, funded, relationship established.
	cust, err := CreateResellerCustomer(reseller.Id, "customer-one", "retail", 400, 1)
	require.NoError(t, err)
	require.NotNil(t, cust)
	assert.Equal(t, OrgTypeEnterprise, cust.Type)
	assert.Equal(t, "retail", cust.PriceGroup)
	assert.Equal(t, 400, cust.WalletQuota, "customer funded with the initial allocation")

	r, _ := GetOrganizationById(reseller.Id)
	assert.Equal(t, 600, r.WalletQuota, "reseller wallet debited")

	// The customer shows up in the reseller's customer list with net allocation.
	customers, err := ListResellerCustomers(reseller.Id)
	require.NoError(t, err)
	require.Len(t, customers, 1)
	assert.Equal(t, cust.Id, customers[0].Org.Id)
	assert.Equal(t, 400, customers[0].NetAllocated)

	// Authorization: the reseller's own customer is authorized; a stranger org
	// (the enterprise it never allocated to) is not.
	ok, err := IsResellerCustomer(reseller.Id, cust.Id)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = IsResellerCustomer(reseller.Id, enterprise.Id)
	require.NoError(t, err)
	assert.False(t, ok, "an org the reseller never allocated to is not its customer")
}

// SSO JIT provisioning attaches a user to the org mapped to its email domain,
// but must never move an existing payer or claim an inactive org.
func TestAutoProvisionOrgMembership(t *testing.T) {
	migrateOrgTables(t)
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)

	// Domain normalization accepts "@Domain " / rejects non-domains.
	assert.Equal(t, "acme.com", NormalizeSsoDomain("  @Acme.com "))
	assert.Equal(t, "", NormalizeSsoDomain("notadomain"))
	assert.Equal(t, "", NormalizeSsoDomain("has space.com"))

	_, err := AddOrgSsoDomain(org.Id, "acme.com", "oidc")
	require.NoError(t, err)
	// UNIQUE(domain): a domain provisions exactly one org.
	_, err = AddOrgSsoDomain(org.Id, "acme.com", "oidc")
	require.Error(t, err)
	// Provider is mandatory.
	_, err = AddOrgSsoDomain(org.Id, "other.com", "")
	require.Error(t, err)

	// A matching domain from the WRONG provider must NOT provision (self-
	// asserted email via public GitHub can't hijack the org's billing).
	imposter := &User{Id: 90000, Username: "imposter", Email: "imposter@acme.com"}
	joined, err := AutoProvisionOrgMembership(imposter, "github")
	require.NoError(t, err)
	assert.False(t, joined, "provider mismatch must not provision")
	acc0, _ := GetOrgAccountByUser(imposter.Id)
	assert.Nil(t, acc0)

	// Matching domain AND provider → attached as a plain member.
	alice := &User{Id: 90001, Username: "alice", Email: "alice@acme.com"}
	joined, err = AutoProvisionOrgMembership(alice, "oidc")
	require.NoError(t, err)
	assert.True(t, joined)
	acc, err := GetOrgAccountByUser(alice.Id)
	require.NoError(t, err)
	require.NotNil(t, acc)
	assert.Equal(t, org.Id, acc.OrgId)
	assert.Equal(t, OrgRoleMember, acc.Role)

	// Idempotent: a second login does not re-attach.
	joined, err = AutoProvisionOrgMembership(alice, "oidc")
	require.NoError(t, err)
	assert.False(t, joined)

	// Unmapped domain → no membership.
	bob := &User{Id: 90002, Username: "bob", Email: "bob@other.com"}
	joined, err = AutoProvisionOrgMembership(bob, "oidc")
	require.NoError(t, err)
	assert.False(t, joined)
	acc, _ = GetOrgAccountByUser(bob.Id)
	assert.Nil(t, acc)

	// An existing payer is never moved by a domain match (acme.com still maps
	// to org, but carol already belongs to another org).
	other := mustCreateOrg(t, "other", OrgTypeReseller, 0)
	carol := &User{Id: 90003, Username: "carol", Email: "carol@acme.com"}
	require.NoError(t, AttachOrgAccount(&OrgAccount{OrgId: other.Id, UserId: carol.Id, Relation: OrgRelationMember, Role: OrgRoleMember}))
	joined, err = AutoProvisionOrgMembership(carol, "oidc")
	require.NoError(t, err)
	assert.False(t, joined, "a user already in an org must not be moved")
	acc, _ = GetOrgAccountByUser(carol.Id)
	require.NotNil(t, acc)
	assert.Equal(t, other.Id, acc.OrgId)

	// A suspended target org does not provision.
	require.NoError(t, UpdateOrganizationFields(org.Id, map[string]interface{}{"status": OrgStatusSuspended}))
	dave := &User{Id: 90004, Username: "dave", Email: "dave@acme.com"}
	joined, err = AutoProvisionOrgMembership(dave, "oidc")
	require.NoError(t, err)
	assert.False(t, joined)
}

// Usage reporting attributes each consume log to the org via its workspace
// token binding, and never counts logs of tokens outside the org.
func TestGetOrgUsage(t *testing.T) {
	migrateOrgTables(t)
	// No global wipes here: the model tests share one in-memory DB, so this
	// test attributes only its OWN org-bound tokens and never reads foreign
	// rows (GetOrgUsage filters logs to the org's bound token set).
	org := mustCreateOrg(t, "acme", OrgTypeEnterprise, 0)
	ws := &Workspace{OrgId: org.Id, Name: "prod"}
	require.NoError(t, CreateWorkspace(ws))

	// AffCode is UNIQUE; set a distinct one so this row can't collide with
	// other tests' users on the shared in-memory DB.
	bob := &User{Username: "bob", AffCode: "usage-bob-aff"}
	require.NoError(t, DB.Create(bob).Error)
	boundTok := &Token{UserId: bob.Id, Name: "bound", Key: "usage-bound-key"}
	require.NoError(t, DB.Create(boundTok).Error)
	require.NoError(t, BindTokenToWorkspace(org.Id, ws.Id, boundTok.Id))

	// A token NOT bound to the org — its logs must be excluded.
	strayTok := &Token{UserId: bob.Id, Name: "stray", Key: "usage-stray-key"}
	require.NoError(t, DB.Create(strayTok).Error)

	seed := func(tokenId int, model string, quota, prompt, completion int, at int64) {
		require.NoError(t, DB.Create(&Log{
			UserId: bob.Id, Username: "bob", Type: LogTypeConsume,
			TokenId: tokenId, ModelName: model, Quota: quota,
			PromptTokens: prompt, CompletionTokens: completion, CreatedAt: at,
		}).Error)
	}
	seed(boundTok.Id, "gpt-4o", 100, 10, 20, 1000)
	seed(boundTok.Id, "gpt-4o", 50, 5, 10, 2000)
	seed(boundTok.Id, "claude-3", 30, 3, 6, 3000)
	seed(strayTok.Id, "gpt-4o", 999, 99, 99, 1500) // excluded: not org-bound

	report, err := GetOrgUsage(org.Id, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(180), report.TotalQuota, "only org-bound tokens count")
	assert.Equal(t, int64(3), report.TotalRequests)
	assert.Equal(t, int64(18), report.TotalPrompt)

	require.Len(t, report.ByModel, 2)
	assert.Equal(t, "gpt-4o", report.ByModel[0].Key) // highest quota first
	assert.Equal(t, int64(150), report.ByModel[0].Quota)
	require.Len(t, report.ByWorkspace, 1)
	assert.Equal(t, "prod", report.ByWorkspace[0].Key)
	require.Len(t, report.ByMember, 1)
	assert.Equal(t, "bob", report.ByMember[0].Key)

	// Time window excludes out-of-range logs.
	windowed, err := GetOrgUsage(org.Id, 2000, 3000)
	require.NoError(t, err)
	assert.Equal(t, int64(80), windowed.TotalQuota)
}
