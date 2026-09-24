package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultListLimit = 50
	maxListLimit     = 500
	maxRankingDays   = 30
	maxRankingLimit  = 50
	maxChatMessages  = 64
)

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}

func ptr[T any](v T) *T { return &v }

func registerTools(s *mcp.Server, engine *gin.Engine) {
	mcp.AddTool(s, &mcp.Tool{Name: "ping", Description: "Health check. Returns ok and the server time.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, pingOutput, error) {
			return nil, pingOutput{Ok: true, ServerTime: time.Now().UTC().Format(time.RFC3339)}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list-models", Annotations: readOnly,
		Description: "List the models this API key can call, with provider, supported endpoints and the effective price for this key. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listModelsInput) (*mcp.CallToolResult, listModelsOutput, error) {
			return listModels(ctx, engine, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get-model", Annotations: readOnly,
		Description: "Full details for one model available to this API key: description, tags, provider, endpoints, billing type and effective price. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in modelInput) (*mcp.CallToolResult, modelDetail, error) {
			return getModel(ctx, engine, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get-model-pricing", Annotations: readOnly,
		Description: "Effective price of one model for this API key, in USD per 1M tokens (or per call), including the group ratio applied to this key. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in modelInput) (*mcp.CallToolResult, modelPricing, error) {
			return getModelPricing(ctx, engine, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get-credits", Annotations: readOnly,
		Description: "Remaining and used credit for this API key (and the wallet that pays for it). Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, creditsOutput, error) {
			return getCredits(ctx)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get-generation", Annotations: readOnly,
		Description: "Cost, token counts and timing of one past request made with this API key, by request id (the X-Oneapi-Request-Id response header, also returned by send-message). Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in generationInput) (*mcp.CallToolResult, generation, error) {
			return getGeneration(ctx, in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list-daily-model-rankings", Annotations: readOnly,
		Description: "Most-used models on this platform over the last N days, ranked by token volume. Free."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in rankingsInput) (*mcp.CallToolResult, rankingsOutput, error) {
			return listRankings(in)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "send-message",
		Description: "Send a chat message to a model through this gateway and return the reply, token usage and cost. This is the only tool that consumes credit.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: ptr(false), IdempotentHint: false, OpenWorldHint: ptr(false)}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendMessageInput) (*mcp.CallToolResult, sendMessageOutput, error) {
			return sendMessage(ctx, engine, in)
		})
}

// ---- shared catalog helpers -------------------------------------------------

// visibleModel is one entry of the caller's /v1/models list.
type visibleModel struct {
	Id                     string   `json:"id"`
	SupportedEndpointTypes []string `json:"supported_endpoint_types"`
}

// visibleModels returns the models the caller's key may use, in the platform's
// own order, by dispatching GET /v1/models as the caller.
func visibleModels(ctx context.Context, engine *gin.Engine) ([]visibleModel, error) {
	res, err := dispatch(ctx, engine, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("model list unavailable: %s", apiErrorMessage(res))
	}
	var payload struct {
		Data []visibleModel `json:"data"`
	}
	if err := common.Unmarshal(res.Body, &payload); err != nil {
		return nil, fmt.Errorf("model list unreadable: %w", err)
	}
	return payload.Data, nil
}

// apiErrorMessage extracts a human-readable message from a REST error body.
func apiErrorMessage(res *dispatchResult) string {
	var body struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = common.Unmarshal(res.Body, &body)
	switch {
	case body.Error.Message != "":
		return body.Error.Message
	case body.Message != "":
		return body.Message
	default:
		return fmt.Sprintf("HTTP %d", res.Status)
	}
}

func pricingByName() map[string]model.Pricing {
	out := map[string]model.Pricing{}
	for _, p := range model.GetPricing() {
		out[p.ModelName] = p
	}
	return out
}

func vendorNames() map[int]string {
	out := map[int]string{}
	for _, v := range model.GetVendors() {
		out[v.ID] = v.Name
	}
	return out
}

// callerGroup resolves the group whose ratio prices this caller's requests:
// the token group when set, else the user group; an "auto" token is priced at
// its first auto group, which is the one the distributor tries first.
func callerGroup(c *gin.Context) (group string, ratio float64) {
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	using := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if using == "" {
		using = userGroup
	}
	if using == "auto" {
		if autoGroups := service.GetRequestAutoGroups(c, userGroup); len(autoGroups) > 0 {
			using = autoGroups[0]
		}
	}
	return using, service.GetUserGroupRatio(userGroup, using)
}

// modelPricing is the effective price of one model for the caller.
type modelPricing struct {
	Model           string   `json:"model"`
	Group           string   `json:"group" jsonschema:"The group whose ratio is applied to this key"`
	GroupRatio      float64  `json:"group_ratio"`
	BillingType     string   `json:"billing_type" jsonschema:"per_token, per_call, tiered_expression or unknown"`
	InputUSDPer1M   *float64 `json:"input_usd_per_1m_tokens,omitempty"`
	OutputUSDPer1M  *float64 `json:"output_usd_per_1m_tokens,omitempty"`
	CachedUSDPer1M  *float64 `json:"cached_input_usd_per_1m_tokens,omitempty"`
	USDPerCall      *float64 `json:"usd_per_call,omitempty"`
	ModelRatio      *float64 `json:"model_ratio,omitempty"`
	CompletionRatio *float64 `json:"completion_ratio,omitempty"`
	Note            string   `json:"note,omitempty"`
}

// usdPerMillionTokens converts a platform ratio to USD per 1M tokens: ratio 1
// = $0.002 per 1K tokens (common.QuotaPerUnit quota per USD).
func usdPerMillionTokens(ratio, groupRatio float64) float64 {
	return ratio * groupRatio * 1_000_000 / common.QuotaPerUnit
}

func effectivePricing(name string, p *model.Pricing, group string, groupRatio float64) modelPricing {
	out := modelPricing{Model: name, Group: group, GroupRatio: groupRatio, BillingType: "unknown"}
	if billing_setting.GetBillingMode(name) == billing_setting.BillingModeTieredExpr {
		out.BillingType = "tiered_expression"
		out.Note = "Priced by a tiered expression; the per-request cost depends on token counts. Use get-generation after a call to see the actual charge."
		return out
	}
	if price, ok := ratio_setting.GetModelPrice(name, false); ok {
		out.BillingType = "per_call"
		out.USDPerCall = ptr(price * groupRatio)
		return out
	}
	ratio, ok, _ := ratio_setting.GetModelRatio(name)
	if !ok && p == nil {
		out.Note = "No price configured for this model."
		return out
	}
	completion := ratio_setting.GetCompletionRatio(name)
	out.BillingType = "per_token"
	out.ModelRatio = ptr(ratio)
	out.CompletionRatio = ptr(completion)
	out.InputUSDPer1M = ptr(usdPerMillionTokens(ratio, groupRatio))
	out.OutputUSDPer1M = ptr(usdPerMillionTokens(ratio*completion, groupRatio))
	if p != nil && p.CacheRatio != nil {
		out.CachedUSDPer1M = ptr(usdPerMillionTokens(ratio**p.CacheRatio, groupRatio))
	}
	return out
}

// ---- ping --------------------------------------------------------------------

type pingOutput struct {
	Ok         bool   `json:"ok"`
	ServerTime string `json:"server_time"`
}

// ---- list-models / get-model ----------------------------------------------------

type listModelsInput struct {
	Search   string `json:"search,omitempty" jsonschema:"Case-insensitive substring matched against the model id"`
	Provider string `json:"provider,omitempty" jsonschema:"Only models from this provider (vendor) name, case-insensitive"`
	Endpoint string `json:"endpoint,omitempty" jsonschema:"Only models supporting this endpoint type, e.g. openai, anthropic, gemini, image-generation, embeddings"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum number of models to return (default 50, max 500)"`
}

type modelSummary struct {
	Id             string   `json:"id"`
	Provider       string   `json:"provider,omitempty"`
	Endpoints      []string `json:"endpoints,omitempty"`
	BillingType    string   `json:"billing_type"`
	InputUSDPer1M  *float64 `json:"input_usd_per_1m_tokens,omitempty"`
	OutputUSDPer1M *float64 `json:"output_usd_per_1m_tokens,omitempty"`
	USDPerCall     *float64 `json:"usd_per_call,omitempty"`
}

type listModelsOutput struct {
	Group   string         `json:"group" jsonschema:"The group whose prices are shown"`
	Total   int            `json:"total" jsonschema:"Number of matching models before the limit"`
	Models  []modelSummary `json:"models"`
	Hint    string         `json:"hint,omitempty"`
	HasMore bool           `json:"has_more"`
}

// providerOf is the vendor configured on the model's metadata, or empty. The
// REST catalog's owned_by is deliberately NOT used as a fallback: it is derived
// from the serving channel's type, and upstream channels stay invisible here.
func providerOf(p *model.Pricing, vendors map[int]string) string {
	if p != nil && p.VendorID > 0 {
		return vendors[p.VendorID]
	}
	return ""
}

func listModels(ctx context.Context, engine *gin.Engine, in listModelsInput) (*mcp.CallToolResult, listModelsOutput, error) {
	c := ginFrom(ctx)
	visible, err := visibleModels(ctx, engine)
	if err != nil {
		return nil, listModelsOutput{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	pricing := pricingByName()
	vendors := vendorNames()
	group, groupRatio := callerGroup(c)
	search := strings.ToLower(strings.TrimSpace(in.Search))
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	endpoint := strings.ToLower(strings.TrimSpace(in.Endpoint))

	out := listModelsOutput{Group: group, Models: []modelSummary{}}
	for _, vm := range visible {
		if search != "" && !strings.Contains(strings.ToLower(vm.Id), search) {
			continue
		}
		var p *model.Pricing
		if entry, ok := pricing[vm.Id]; ok {
			p = &entry
		}
		prov := providerOf(p, vendors)
		if provider != "" && strings.ToLower(prov) != provider {
			continue
		}
		if endpoint != "" && !containsFold(vm.SupportedEndpointTypes, endpoint) {
			continue
		}
		out.Total++
		if len(out.Models) >= limit {
			out.HasMore = true
			continue
		}
		price := effectivePricing(vm.Id, p, group, groupRatio)
		out.Models = append(out.Models, modelSummary{
			Id: vm.Id, Provider: prov, Endpoints: vm.SupportedEndpointTypes,
			BillingType: price.BillingType, InputUSDPer1M: price.InputUSDPer1M,
			OutputUSDPer1M: price.OutputUSDPer1M, USDPerCall: price.USDPerCall,
		})
	}
	if out.HasMore {
		out.Hint = "Narrow with search/provider/endpoint or raise limit to see the rest."
	}
	return nil, out, nil
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

type modelInput struct {
	Model string `json:"model" jsonschema:"Exact model id as listed by list-models"`
}

type modelDetail struct {
	Id          string       `json:"id"`
	Provider    string       `json:"provider,omitempty"`
	Description string       `json:"description,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Endpoints   []string     `json:"endpoints,omitempty"`
	Pricing     modelPricing `json:"pricing"`
}

// findVisible returns the caller-visible entry for a model id, or an error the
// agent can act on. Models outside the key's catalog are indistinguishable
// from unknown ones on purpose.
func findVisible(ctx context.Context, engine *gin.Engine, name string) (*visibleModel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("model is required")
	}
	visible, err := visibleModels(ctx, engine)
	if err != nil {
		return nil, err
	}
	for i := range visible {
		if visible[i].Id == name {
			return &visible[i], nil
		}
	}
	return nil, fmt.Errorf("model %q is not available to this API key; call list-models to see what is", name)
}

func getModel(ctx context.Context, engine *gin.Engine, in modelInput) (*mcp.CallToolResult, modelDetail, error) {
	c := ginFrom(ctx)
	vm, err := findVisible(ctx, engine, in.Model)
	if err != nil {
		return nil, modelDetail{}, err
	}
	var p *model.Pricing
	if entry, ok := pricingByName()[vm.Id]; ok {
		p = &entry
	}
	group, groupRatio := callerGroup(c)
	out := modelDetail{
		Id:        vm.Id,
		Provider:  providerOf(p, vendorNames()),
		Endpoints: vm.SupportedEndpointTypes,
		Pricing:   effectivePricing(vm.Id, p, group, groupRatio),
	}
	if p != nil {
		out.Description = p.Description
		for _, tag := range strings.Split(p.Tags, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				out.Tags = append(out.Tags, tag)
			}
		}
	}
	return nil, out, nil
}

func getModelPricing(ctx context.Context, engine *gin.Engine, in modelInput) (*mcp.CallToolResult, modelPricing, error) {
	c := ginFrom(ctx)
	vm, err := findVisible(ctx, engine, in.Model)
	if err != nil {
		return nil, modelPricing{}, err
	}
	var p *model.Pricing
	if entry, ok := pricingByName()[vm.Id]; ok {
		p = &entry
	}
	group, groupRatio := callerGroup(c)
	return nil, effectivePricing(vm.Id, p, group, groupRatio), nil
}
