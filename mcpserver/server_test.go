package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newTestEngine builds a gin engine shaped like the real one for the paths the
// MCP tools dispatch to, with a stand-in for TokenAuth that trusts a fixed key.
// It also mounts the MCP endpoint, so a test exercises the full path: MCP
// client → /mcp → tool → in-process dispatch → /v1 handler.
func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	fakeAuth := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer sk-test" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "bad key"}})
			return
		}
		c.Set("id", 7)
		c.Set("token_id", 42)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	}
	v1 := engine.Group("/v1", fakeAuth)
	v1.GET("/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{
			{"id": "gpt-x", "owned_by": "openai", "supported_endpoint_types": []string{"openai"}},
			{"id": "claude-y", "owned_by": "anthropic", "supported_endpoint_types": []string{"anthropic", "openai"}},
		}})
	})
	v1.POST("/chat/completions", func(c *gin.Context) {
		var body map[string]any
		require.NoError(t, c.ShouldBindJSON(&body))
		assert.Equal(t, false, body["stream"], "send-message must never ask for a stream")
		c.Header(common.RequestIdKey, "req-123")
		c.JSON(http.StatusOK, gin.H{
			"model":   body["model"],
			"choices": []gin.H{{"finish_reason": "stop", "message": gin.H{"role": "assistant", "content": "pong"}}},
			"usage":   gin.H{"prompt_tokens": 3, "completion_tokens": 1, "total_tokens": 4},
		})
	})
	mcpGroup := engine.Group("/mcp", fakeAuth)
	mcpGroup.Any("", Handler(engine))
	return engine
}

func newTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.QuotaData{},
		&model.Organization{}, &model.OrgAccount{}, &model.CreditLedger{}, &model.Workspace{}, &model.WorkspaceToken{},
		&model.ResellerCustomerLink{}, &model.ResellerAdmin{}, &model.OrgUsageDaily{}, &model.User{}, &model.Token{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	// The token→workspace binding is cached process-wide; a previous test may
	// have bound key 42, so start every test from the unbound state.
	model.InvalidateTokenWorkspaceCache(42)
	// The fake-auth caller: user 7 holding personal key 42 (see newTestEngine).
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "jayke", Quota: 5_000_000, UsedQuota: 250_000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 42, UserId: 7, Name: "jayke-01", Key: "test", UnlimitedQuota: true, UsedQuota: 25, Status: common.TokenStatusEnabled}).Error)
}

// seedReseller makes user 7 (the fake-auth caller) the admin of a distributor
// with one customer, and returns both orgs.
func seedReseller(t *testing.T) (reseller, customer *model.Organization) {
	t.Helper()
	reseller = &model.Organization{Name: "acme-reseller", Type: model.OrgTypeReseller, Status: model.OrgStatusActive,
		WalletQuota: 2_000_000, WholesaleRatios: `{"claude":0.8}`}
	require.NoError(t, model.DB.Create(reseller).Error)
	require.NoError(t, model.DB.Create(&model.ResellerAdmin{UserId: 7, ResellerOrgId: reseller.Id, Status: model.OrgStatusActive}).Error)
	var err error
	customer, err = model.CreateResellerCustomer(reseller.Id, "customer-one", "retail", 500_000, 7)
	require.NoError(t, err)
	return reseller, customer
}

func toolNames(t *testing.T, session *mcp.ClientSession) map[string]bool {
	t.Helper()
	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

func connect(t *testing.T, engine *gin.Engine, key string) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: headerRoundTripper{key: key, next: http.DefaultTransport}},
		MaxRetries: -1,
	}
	session, err := client.Connect(context.Background(), transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type headerRoundTripper struct {
	key  string
	next http.RoundTripper
}

func (h headerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if h.key != "" {
		r.Header.Set("Authorization", "Bearer "+h.key)
	}
	return h.next.RoundTrip(r)
}

func TestToolsListedAndReadOnlyHints(t *testing.T) {
	newTestDB(t)
	session := connect(t, newTestEngine(t), "sk-test")
	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool.Annotations != nil && tool.Annotations.ReadOnlyHint
	}
	for _, name := range []string{"ping", "list-models", "get-model", "get-model-pricing",
		"get-credits", "get-generation", "list-daily-model-rankings"} {
		assert.True(t, got[name], "%s must be listed and marked read-only", name)
	}
	_, providersListed := got["list-providers"]
	assert.False(t, providersListed, "upstream/vendor listing must not be exposed")
	_, resellerListed := got["reseller-summary"]
	assert.False(t, resellerListed, "a key that is not a distributor admin's must not see reseller tools")
	readOnly, listed := got["send-message"]
	assert.True(t, listed)
	assert.False(t, readOnly, "send-message spends credit and must not claim read-only")
}

func TestUnauthenticatedRequestIsRefusedBeforeTools(t *testing.T) {
	engine := newTestEngine(t)
	srv := httptest.NewServer(engine)
	defer srv.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	_, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", MaxRetries: -1}, nil)
	require.Error(t, err, "no bearer key must fail at initialize")
}

func TestListModelsUsesCallerCatalogAndFilters(t *testing.T) {
	newTestDB(t)
	session := connect(t, newTestEngine(t), "sk-test")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list-models", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var out listModelsOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	assert.Equal(t, 2, out.Total)
	assert.Equal(t, "default", out.Group)
	require.Len(t, out.Models, 2)
	assert.Equal(t, "gpt-x", out.Models[0].Id)
	assert.Empty(t, out.Models[0].Provider, "owned_by reflects the serving channel type and must never leak; only a configured vendor is shown")

	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list-models",
		Arguments: map[string]any{"endpoint": "anthropic"}})
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	require.Len(t, out.Models, 1)
	assert.Equal(t, "claude-y", out.Models[0].Id)

	// A model outside the caller's catalog is reported as unavailable, not leaked.
	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get-model",
		Arguments: map[string]any{"model": "secret-internal-model"}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestSendMessageDispatchesAsCallerAndReturnsRequestId(t *testing.T) {
	newTestDB(t)
	// A consume log already present for the request id is surfaced as cost.
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId: 7, TokenId: 42, Type: model.LogTypeConsume, RequestId: "req-123", ModelName: "gpt-x",
		Quota: 500000, PromptTokens: 3, CompletionTokens: 1, Other: `{"group_ratio":1,"admin_info":{"channel":"hidden"}}`,
	}).Error)
	session := connect(t, newTestEngine(t), "sk-test")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "send-message",
		Arguments: map[string]any{"model": "gpt-x", "prompt": "ping"}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var out sendMessageOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	assert.Equal(t, "req-123", out.RequestId)
	assert.Equal(t, "pong", out.Content)
	assert.Equal(t, "stop", out.FinishReason)
	require.NotNil(t, out.Usage)
	assert.Equal(t, 4, out.Usage.TotalTokens)
	require.NotNil(t, out.CostUSD)
	assert.InDelta(t, 1.0, *out.CostUSD, 1e-9, "500000 quota = $1")

	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "send-message",
		Arguments: map[string]any{"model": "gpt-x"}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "no prompt and no messages is a tool error")
}

func TestGetGenerationIsScopedToCallingKeyAndHidesAdminInfo(t *testing.T) {
	newTestDB(t)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId: 7, TokenId: 42, Type: model.LogTypeConsume, RequestId: "mine", ModelName: "gpt-x", Quota: 250000,
		Other: `{"group_ratio":1,"admin_info":{"channel":"hidden"}}`,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId: 7, TokenId: 99, Type: model.LogTypeConsume, RequestId: "other-key", ModelName: "gpt-x", Quota: 250000,
	}).Error)
	session := connect(t, newTestEngine(t), "sk-test")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get-generation",
		Arguments: map[string]any{"request_id": "mine"}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var out generation
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	assert.InDelta(t, 0.5, out.CostUSD, 1e-9)
	assert.Equal(t, float64(1), out.Details["group_ratio"])
	_, leaked := out.Details["admin_info"]
	assert.False(t, leaked, "admin_info must be stripped for API-key callers")

	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get-generation",
		Arguments: map[string]any{"request_id": "other-key"}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "another key's generation must not be readable")
}

func TestRankingsAggregateByModelWithinWindow(t *testing.T) {
	newTestDB(t)
	now := common.GetTimestamp()
	rows := []model.QuotaData{
		{ModelName: "gpt-x", CreatedAt: now - 3600, TokenUsed: 100, Count: 2},
		{ModelName: "gpt-x", CreatedAt: now - 7200, TokenUsed: 50, Count: 1},
		{ModelName: "claude-y", CreatedAt: now - 3600, TokenUsed: 120, Count: 1},
		{ModelName: "stale", CreatedAt: now - 40*24*3600, TokenUsed: 999, Count: 9},
	}
	for i := range rows {
		require.NoError(t, model.DB.Create(&rows[i]).Error)
	}
	session := connect(t, newTestEngine(t), "sk-test")
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list-daily-model-rankings",
		Arguments: map[string]any{"days": 7}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var out rankingsOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	require.Len(t, out.Rankings, 2, "rows outside the window are excluded")
	assert.Equal(t, rankingRow{Rank: 1, Model: "gpt-x", Tokens: 150, Requests: 3}, out.Rankings[0])
	assert.Equal(t, rankingRow{Rank: 2, Model: "claude-y", Tokens: 120, Requests: 1}, out.Rankings[1])
}

func TestEffectivePricingConvertsRatiosToUSD(t *testing.T) {
	// ratio 1 = $2 per 1M tokens; completion ratio and group ratio multiply in.
	assert.InDelta(t, 2.0, usdPerMillionTokens(1, 1), 1e-9)
	assert.InDelta(t, 30.0, usdPerMillionTokens(15, 1), 1e-9)
	assert.InDelta(t, 1.6, usdPerMillionTokens(1, 0.8), 1e-9, "group discount applies")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := common.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestGetCreditsSeparatesKeyPersonalAndDistributorBalances(t *testing.T) {
	newTestDB(t)
	reseller, customer := seedReseller(t)
	engine := newTestEngine(t)

	session := connect(t, engine, "sk-test")
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get-credits", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var out creditsOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	assert.Equal(t, "personal_balance", out.PaidBy)
	assert.True(t, out.Key.Unlimited)
	require.NotNil(t, out.PersonalBalance, "a personal key must show the balance it actually spends")
	assert.InDelta(t, 10.0, *out.PersonalBalance.RemainingUSD, 1e-9)
	assert.InDelta(t, 0.5, *out.PersonalBalance.UsedUSD, 1e-9)
	require.NotNil(t, out.DistributorWallet, "a distributor admin sees the distributor wallet too")
	assert.Equal(t, reseller.Name, out.DistributorWallet.Name)
	assert.InDelta(t, 4.0, *out.DistributorWallet.RemainingUSD, 1e-9, "the wallet is the distributor's own cost balance; granting a customer a cap does not debit it")
	assert.Nil(t, out.OrganizationWallet)
	assert.Contains(t, out.Note, "does not draw from it")

	// Bound to a customer workspace, the same key is paid by that org's wallet
	// and the distributor wallet disappears from the answer.
	ws := &model.Workspace{OrgId: customer.Id, Name: "prod"}
	require.NoError(t, model.CreateWorkspace(ws))
	require.NoError(t, model.BindTokenToWorkspace(customer.Id, ws.Id, 42))
	res, err = connect(t, engine, "sk-test").CallTool(context.Background(), &mcp.CallToolParams{Name: "get-credits", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var bound creditsOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &bound))
	assert.Equal(t, "organization_wallet", bound.PaidBy)
	require.NotNil(t, bound.OrganizationWallet)
	assert.Equal(t, customer.Name, bound.OrganizationWallet.Name)
	assert.InDelta(t, 1.0, *bound.OrganizationWallet.RemainingUSD, 1e-9, "the customer's initial allocation")
	assert.Nil(t, bound.PersonalBalance)
	assert.Nil(t, bound.DistributorWallet)
}

func TestResellerToolsOnlyForDistributorAdminPersonalKey(t *testing.T) {
	newTestDB(t)
	reseller, customer := seedReseller(t)
	engine := newTestEngine(t)

	// Distributor admin's personal key: reseller-* tools appear and are scoped.
	session := connect(t, engine, "sk-test")
	names := toolNames(t, session)
	for _, name := range []string{"reseller-summary", "reseller-customers", "reseller-usage",
		"reseller-customer-logs", "reseller-customer-offer", "reseller-ledger"} {
		assert.True(t, names[name], "%s must be listed for a distributor admin", name)
	}
	assert.True(t, names["list-models"], "base tools stay available")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reseller-summary", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var summary resellerSummary
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &summary))
	assert.Equal(t, reseller.Id, summary.Id)
	assert.Equal(t, 1, summary.CustomerCount)
	assert.InDelta(t, 4.0, summary.WalletUSD, 1e-9, "2,000,000 quota = $4; a customer cap grant does not debit the wallet")
	assert.Equal(t, 0.8, summary.WholesaleDiscounts["claude"])

	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reseller-customers", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var customers resellerCustomersOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &customers))
	require.Len(t, customers.Customers, 1)
	assert.Equal(t, customer.Id, customers.Customers[0].Id)
	assert.InDelta(t, 1.0, customers.Customers[0].NetAllocatedUSD, 1e-9)

	// A foreign org id is refused exactly like an unknown one.
	stranger := &model.Organization{Name: "someone-else", Type: model.OrgTypeEnterprise, Status: model.OrgStatusActive}
	require.NoError(t, model.DB.Create(stranger).Error)
	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reseller-customer-offer",
		Arguments: map[string]any{"customer_id": stranger.Id}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "another organization must not be readable through the distributor tools")

	// The same user's workspace-bound key is a customer credential: no reseller tools.
	ws := &model.Workspace{OrgId: customer.Id, Name: "prod"}
	require.NoError(t, model.CreateWorkspace(ws))
	require.NoError(t, model.BindTokenToWorkspace(customer.Id, ws.Id, 42))
	wsSession := connect(t, engine, "sk-test")
	wsNames := toolNames(t, wsSession)
	assert.False(t, wsNames["reseller-summary"], "workspace keys never unlock distributor data")
	_, err = wsSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "reseller-summary", Arguments: map[string]any{}})
	require.Error(t, err, "a hidden distributor tool is unknown to this caller, not merely failing")
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestResellerUsageReportsProfitAcrossCustomers(t *testing.T) {
	newTestDB(t)
	reseller, customer := seedReseller(t)
	now := common.GetTimestamp()
	// standard 1,000,000 ($2), customer paid 900,000 ($1.80), cost 800,000 ($1.60)
	require.NoError(t, model.RecordOrgUsageDaily(now-3600, customer.Id, 0, reseller.Id, 9, "claude-opus", 1_000_000, 900_000, 800_000, 100, 50))
	// Outside a 7-day window: must not count.
	require.NoError(t, model.RecordOrgUsageDaily(now-10*86400, customer.Id, 0, reseller.Id, 9, "claude-opus", 5_000_000, 5_000_000, 5_000_000, 1, 1))
	session := connect(t, newTestEngine(t), "sk-test")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reseller-usage", Arguments: map[string]any{"days": 7}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var out resellerUsageOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	assert.Equal(t, "all-customers", out.Scope)
	assert.InDelta(t, 2.0, out.StandardUSD, 1e-9)
	assert.InDelta(t, 1.8, out.CustomerPaidUSD, 1e-9)
	assert.InDelta(t, 1.6, out.CostUSD, 1e-9)
	assert.InDelta(t, 0.2, out.MarginUSD, 1e-9, "margin = customer paid - cost")
	require.Len(t, out.ByCustomer, 1)
	assert.Equal(t, "customer-one", out.ByCustomer[0].Key)
	require.Len(t, out.ByModel, 1)
	assert.Equal(t, "claude-opus", out.ByModel[0].Key)

	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reseller-usage",
		Arguments: map[string]any{"days": 7, "customer_id": customer.Id}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	var single resellerUsageOutput
	require.NoError(t, common.Unmarshal(mustJSON(t, res.StructuredContent), &single))
	assert.Equal(t, "customer-one", single.Scope)
	assert.InDelta(t, 1.8, single.CustomerPaidUSD, 1e-9)
	assert.Empty(t, single.ByCustomer, "a single customer statement has no per-customer split")
}
