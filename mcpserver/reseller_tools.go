package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Distributor (reseller) tools. They are mounted only when the calling API
// key belongs to an active distributor admin (see callerReseller), so customers
// and ordinary users never see them in tools/list. Every handler re-resolves
// the caller's distributor and scopes each query to that org, so the tools are
// safe even if reached directly. All of them are read-only: anything that moves
// money or changes an offer stays in the console behind its own 2FA.

const (
	maxUsageDays     = 90
	maxResellerLimit = 100
)

// callerReseller resolves the distributor a caller may act for. The key must
// belong to an active distributor admin and be either a personal key or one
// of the distributor's own keys (bound to a workspace of the distributor org,
// which is how the distributor console issues keys). A key bound to a
// customer's workspace is a customer-facing credential and never unlocks
// distributor data, whoever holds it.
func callerReseller(c *gin.Context) (*model.Organization, error) {
	org, err := model.GetResellerAdminOrg(c.GetInt("id"))
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, fmt.Errorf("this API key does not belong to a distributor admin")
	}
	if org.Status != model.OrgStatusActive {
		return nil, fmt.Errorf("distributor %q is suspended", org.Name)
	}
	wsId, err := model.GetTokenWorkspaceId(c.GetInt("token_id"))
	if err != nil {
		return nil, err
	}
	if wsId != 0 {
		ws, wErr := model.GetWorkspaceById(wsId)
		if wErr != nil {
			return nil, wErr
		}
		if ws == nil || ws.OrgId != org.Id {
			return nil, fmt.Errorf("distributor tools require one of the distributor's own keys, not a customer workspace key")
		}
	}
	return org, nil
}

func isResellerCaller(c *gin.Context) bool {
	if c == nil {
		return false
	}
	_, err := callerReseller(c)
	return err == nil
}

// usageWindow turns a "last N days" input into [from, to] unix seconds.
func usageWindow(days int) (from, to int64, effectiveDays int) {
	if days <= 0 {
		days = 7
	}
	if days > maxUsageDays {
		days = maxUsageDays
	}
	to = time.Now().Unix()
	return to - int64(days)*86400, to, days
}

func clampLimit(limit, def int) int {
	if limit <= 0 {
		return def
	}
	if limit > maxResellerLimit {
		return maxResellerLimit
	}
	return limit
}

// ownCustomer verifies customerId is one of the distributor's customers and
// returns the customer org. Foreign ids are rejected with the same message
// as unknown ones so nothing about other organizations leaks.
func ownCustomer(reseller *model.Organization, customerId int) (*model.Organization, error) {
	if customerId <= 0 {
		return nil, fmt.Errorf("customer_id is required")
	}
	ok, err := model.IsResellerCustomer(reseller.Id, customerId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("organization %d is not one of your customers", customerId)
	}
	customer, err := model.GetOrganizationById(customerId)
	if err != nil || customer == nil {
		return nil, fmt.Errorf("customer %d not found", customerId)
	}
	return customer, nil
}

func registerResellerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "reseller-summary", Annotations: readOnly,
		Description: "Your distributor account at a glance: wallet balance, price group, wholesale discounts and customer count. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, resellerSummary, error) {
			return resellerSummaryTool(ctx)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "reseller-customers", Annotations: readOnly,
		Description: "List your customers with status, wallet balance, net credit you have allocated and how many models each may use. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, resellerCustomersOutput, error) {
			return resellerCustomersTool(ctx)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "reseller-usage", Annotations: readOnly,
		Description: "Usage and profit over the last N days: standard price, what customers paid you, your cost and margin, broken down by customer, model and member. Pass customer_id for one customer's statement. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in resellerUsageInput) (*mcp.CallToolResult, resellerUsageOutput, error) {
			return resellerUsageTool(ctx, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "reseller-customer-logs", Annotations: readOnly,
		Description: "Recent call records of one of your customers: model, tokens, standard price, what the customer paid, latency and request id. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in customerLogsInput) (*mcp.CallToolResult, customerLogsOutput, error) {
			return resellerCustomerLogsTool(ctx, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "reseller-customer-offer", Annotations: readOnly,
		Description: "The models one of your customers may call and the per-model-series discounts you granted them. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in customerInput) (*mcp.CallToolResult, customerOffer, error) {
			return resellerCustomerOfferTool(ctx, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "reseller-ledger", Annotations: readOnly,
		Description: "Your distributor wallet ledger: purchases, allocations to customers and revocations, newest first. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in ledgerInput) (*mcp.CallToolResult, ledgerOutput, error) {
			return resellerLedgerTool(ctx, in)
		})
}

// ---- reseller-summary ---------------------------------------------------------

type resellerSummary struct {
	Id                 int                `json:"id"`
	Name               string             `json:"name"`
	Status             string             `json:"status"`
	WalletUSD          float64            `json:"wallet_usd"`
	PriceGroup         string             `json:"price_group,omitempty"`
	WholesaleDiscounts map[string]float64 `json:"wholesale_discounts" jsonschema:"model or model-series token -> ratio of standard price you pay"`
	CustomerCount      int                `json:"customer_count"`
	BrandName          string             `json:"brand_name,omitempty"`
}

func resellerSummaryTool(ctx context.Context) (*mcp.CallToolResult, resellerSummary, error) {
	reseller, err := callerReseller(ginFrom(ctx))
	if err != nil {
		return nil, resellerSummary{}, err
	}
	customers, err := model.ListResellerCustomers(reseller.Id)
	if err != nil {
		return nil, resellerSummary{}, err
	}
	return nil, resellerSummary{
		Id:                 reseller.Id,
		Name:               reseller.Name,
		Status:             reseller.Status,
		WalletUSD:          quotaToUSD(reseller.WalletQuota),
		PriceGroup:         reseller.PriceGroup,
		WholesaleDiscounts: model.ParseRetailDiscounts(reseller.WholesaleRatios),
		CustomerCount:      len(customers),
		BrandName:          reseller.BrandName,
	}, nil
}

// ---- reseller-customers ----------------------------------------------------------

type customerSummary struct {
	Id              int     `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	WalletUSD       float64 `json:"wallet_usd"`
	NetAllocatedUSD float64 `json:"net_allocated_usd" jsonschema:"credit you allocated minus credit you revoked"`
	AllowedModels   int     `json:"allowed_models" jsonschema:"number of models assigned; 0 = unrestricted"`
	OwnerEmail      string  `json:"owner_email,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type resellerCustomersOutput struct {
	Customers []customerSummary `json:"customers"`
}

func resellerCustomersTool(ctx context.Context) (*mcp.CallToolResult, resellerCustomersOutput, error) {
	reseller, err := callerReseller(ginFrom(ctx))
	if err != nil {
		return nil, resellerCustomersOutput{}, err
	}
	customers, err := model.ListResellerCustomers(reseller.Id)
	if err != nil {
		return nil, resellerCustomersOutput{}, err
	}
	out := resellerCustomersOutput{Customers: []customerSummary{}}
	for _, cu := range customers {
		if cu.Org == nil {
			continue
		}
		out.Customers = append(out.Customers, customerSummary{
			Id:              cu.Org.Id,
			Name:            cu.Org.Name,
			Status:          cu.Org.Status,
			WalletUSD:       quotaToUSD(cu.Org.WalletQuota),
			NetAllocatedUSD: quotaToUSD(cu.NetAllocated),
			AllowedModels:   len(cu.Org.AllowedModelList()),
			OwnerEmail:      cu.OwnerEmail,
			CreatedAt:       time.Unix(cu.Org.CreatedTime, 0).UTC().Format(time.RFC3339),
		})
	}
	return nil, out, nil
}

// ---- reseller-usage ---------------------------------------------------------------

type resellerUsageInput struct {
	Days       int `json:"days,omitempty" jsonschema:"Window in days ending now (default 7, max 90)"`
	CustomerId int `json:"customer_id,omitempty" jsonschema:"Restrict to one customer; omit for all customers"`
}

type usageLine struct {
	Key              string  `json:"key"`
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	StandardUSD      float64 `json:"standard_usd" jsonschema:"platform list price"`
	CustomerPaidUSD  float64 `json:"customer_paid_usd" jsonschema:"what the customer paid you after your discounts"`
	CostUSD          float64 `json:"cost_usd,omitempty" jsonschema:"your wholesale cost; only on the all-customers view"`
}

type resellerUsageOutput struct {
	Days             int         `json:"days"`
	From             string      `json:"from"`
	To               string      `json:"to"`
	Scope            string      `json:"scope" jsonschema:"all-customers or the customer's name"`
	Requests         int64       `json:"requests"`
	PromptTokens     int64       `json:"prompt_tokens"`
	CompletionTokens int64       `json:"completion_tokens"`
	StandardUSD      float64     `json:"standard_usd"`
	CustomerPaidUSD  float64     `json:"customer_paid_usd"`
	CostUSD          float64     `json:"cost_usd,omitempty"`
	MarginUSD        float64     `json:"margin_usd,omitempty" jsonschema:"customer_paid_usd minus cost_usd; only on the all-customers view"`
	ByCustomer       []usageLine `json:"by_customer,omitempty"`
	ByModel          []usageLine `json:"by_model"`
	ByMember         []usageLine `json:"by_member,omitempty"`
}

func usageLines(buckets []model.OrgUsageBucket) []usageLine {
	out := make([]usageLine, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, usageLine{
			Key: b.Key, Requests: b.Requests, PromptTokens: b.PromptTokens, CompletionTokens: b.CompletionTokens,
			StandardUSD: quotaToUSD(int(b.Quota)), CustomerPaidUSD: quotaToUSD(int(b.RetailQuota)), CostUSD: quotaToUSD(int(b.CostQuota)),
		})
	}
	return out
}

func resellerUsageTool(ctx context.Context, in resellerUsageInput) (*mcp.CallToolResult, resellerUsageOutput, error) {
	reseller, err := callerReseller(ginFrom(ctx))
	if err != nil {
		return nil, resellerUsageOutput{}, err
	}
	from, to, days := usageWindow(in.Days)
	var report *model.OrgUsageReport
	scope := "all-customers"
	if in.CustomerId > 0 {
		customer, cErr := ownCustomer(reseller, in.CustomerId)
		if cErr != nil {
			return nil, resellerUsageOutput{}, cErr
		}
		scope = customer.Name
		report, err = model.GetOrgUsageFromDaily(customer.Id, from, to)
	} else {
		report, err = model.GetResellerUsageFromDaily(reseller.Id, from, to)
	}
	if err != nil {
		return nil, resellerUsageOutput{}, err
	}
	out := resellerUsageOutput{
		Days: days, From: time.Unix(from, 0).UTC().Format(time.RFC3339), To: time.Unix(to, 0).UTC().Format(time.RFC3339),
		Scope:            scope,
		Requests:         report.TotalRequests,
		PromptTokens:     report.TotalPrompt,
		CompletionTokens: report.TotalCompletion,
		StandardUSD:      quotaToUSD(int(report.TotalQuota)),
		CustomerPaidUSD:  quotaToUSD(int(report.TotalRetailQuota)),
		CostUSD:          quotaToUSD(int(report.TotalCostQuota)),
		ByModel:          usageLines(report.ByModel),
		ByMember:         usageLines(report.ByMember),
	}
	if in.CustomerId == 0 {
		// On the aggregate view ByWorkspace holds one line per customer.
		out.ByCustomer = usageLines(report.ByWorkspace)
		out.MarginUSD = out.CustomerPaidUSD - out.CostUSD
	}
	return nil, out, nil
}

// ---- reseller-customer-logs --------------------------------------------------------

type customerInput struct {
	CustomerId int `json:"customer_id" jsonschema:"Customer organization id from reseller-customers"`
}

type customerLogsInput struct {
	CustomerId int `json:"customer_id" jsonschema:"Customer organization id from reseller-customers"`
	Days       int `json:"days,omitempty" jsonschema:"Window in days ending now (default 7, max 90)"`
	Limit      int `json:"limit,omitempty" jsonschema:"Maximum rows, newest first (default 50, max 100)"`
}

type customerLogRow struct {
	RequestId        string  `json:"request_id,omitempty"`
	CreatedAt        string  `json:"created_at"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	StandardUSD      float64 `json:"standard_usd"`
	CustomerPaidUSD  float64 `json:"customer_paid_usd"`
	LatencySeconds   int     `json:"latency_seconds"`
	Stream           bool    `json:"stream"`
}

type customerLogsOutput struct {
	Customer string           `json:"customer"`
	Total    int64            `json:"total" jsonschema:"rows in the window before the limit"`
	Logs     []customerLogRow `json:"logs"`
}

func resellerCustomerLogsTool(ctx context.Context, in customerLogsInput) (*mcp.CallToolResult, customerLogsOutput, error) {
	reseller, err := callerReseller(ginFrom(ctx))
	if err != nil {
		return nil, customerLogsOutput{}, err
	}
	customer, err := ownCustomer(reseller, in.CustomerId)
	if err != nil {
		return nil, customerLogsOutput{}, err
	}
	from, to, _ := usageWindow(in.Days)
	logs, total, err := model.ListOrgLogs(customer.Id, from, to, 0, clampLimit(in.Limit, 50))
	if err != nil {
		return nil, customerLogsOutput{}, err
	}
	out := customerLogsOutput{Customer: customer.Name, Total: total, Logs: []customerLogRow{}}
	for _, l := range logs {
		// End-user IPs and admin diagnostics of the customer are never shown to
		// the distributor; only the billing facts are.
		out.Logs = append(out.Logs, customerLogRow{
			RequestId:        l.RequestId,
			CreatedAt:        time.Unix(l.CreatedAt, 0).UTC().Format(time.RFC3339),
			Model:            l.ModelName,
			PromptTokens:     l.PromptTokens,
			CompletionTokens: l.CompletionTokens,
			StandardUSD:      quotaToUSD(l.Quota),
			CustomerPaidUSD:  quotaToUSD(l.RetailQuota),
			LatencySeconds:   l.UseTime,
			Stream:           l.IsStream,
		})
	}
	return nil, out, nil
}

// ---- reseller-customer-offer -------------------------------------------------------

type customerOffer struct {
	Customer      string             `json:"customer"`
	AllowedModels []string           `json:"allowed_models" jsonschema:"exact names or series prefixes; empty = unrestricted"`
	Discounts     map[string]float64 `json:"discounts" jsonschema:"model or series token -> ratio of standard price the customer pays"`
}

func resellerCustomerOfferTool(ctx context.Context, in customerInput) (*mcp.CallToolResult, customerOffer, error) {
	reseller, err := callerReseller(ginFrom(ctx))
	if err != nil {
		return nil, customerOffer{}, err
	}
	customer, err := ownCustomer(reseller, in.CustomerId)
	if err != nil {
		return nil, customerOffer{}, err
	}
	allowed := customer.AllowedModelList()
	if allowed == nil {
		allowed = []string{}
	}
	return nil, customerOffer{
		Customer:      customer.Name,
		AllowedModels: allowed,
		Discounts:     model.ParseRetailDiscounts(customer.RetailDiscounts),
	}, nil
}

// ---- reseller-ledger -------------------------------------------------------------------

type ledgerInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum rows, newest first (default 50, max 100)"`
}

type ledgerRow struct {
	Type      string  `json:"type" jsonschema:"purchase, allocate or revoke"`
	USD       float64 `json:"usd"`
	FromOrgId int     `json:"from_org_id" jsonschema:"0 = platform"`
	ToOrgId   int     `json:"to_org_id"`
	Remark    string  `json:"remark,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type ledgerOutput struct {
	Total int64       `json:"total"`
	Rows  []ledgerRow `json:"rows"`
}

func resellerLedgerTool(ctx context.Context, in ledgerInput) (*mcp.CallToolResult, ledgerOutput, error) {
	reseller, err := callerReseller(ginFrom(ctx))
	if err != nil {
		return nil, ledgerOutput{}, err
	}
	rows, total, err := model.ListOrgLedger(reseller.Id, 0, clampLimit(in.Limit, 50))
	if err != nil {
		return nil, ledgerOutput{}, err
	}
	out := ledgerOutput{Total: total, Rows: []ledgerRow{}}
	for _, r := range rows {
		out.Rows = append(out.Rows, ledgerRow{
			Type: r.Type, USD: quotaToUSD(r.Quota), FromOrgId: r.FromOrgId, ToOrgId: r.ToOrgId,
			Remark: r.Remark, CreatedAt: time.Unix(r.CreatedTime, 0).UTC().Format(time.RFC3339),
		})
	}
	return nil, out, nil
}
