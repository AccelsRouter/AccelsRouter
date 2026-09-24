package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func quotaToUSD(quota int) float64 { return float64(quota) / common.QuotaPerUnit }

// ---- get-credits ---------------------------------------------------------------

type walletCredits struct {
	Name         string   `json:"name,omitempty"`
	RemainingUSD *float64 `json:"remaining_usd,omitempty"`
	UsedUSD      float64  `json:"used_usd"`
	Unlimited    bool     `json:"unlimited,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
}

type creditsOutput struct {
	Key           walletCredits  `json:"key" jsonschema:"Limits of this API key itself"`
	Wallet        *walletCredits `json:"wallet,omitempty" jsonschema:"The balance that actually pays for this key's calls, when the platform exposes it"`
	BillingSource string         `json:"billing_source" jsonschema:"personal (the key owner's balance) or organization (an organization wallet)"`
	Currency      string         `json:"currency"`
}

func getCredits(ctx context.Context) (*mcp.CallToolResult, creditsOutput, error) {
	c := ginFrom(ctx)
	token, err := model.GetTokenById(c.GetInt("token_id"))
	if err != nil {
		return nil, creditsOutput{}, fmt.Errorf("key lookup failed: %w", err)
	}
	out := creditsOutput{Currency: "USD", BillingSource: "personal"}
	out.Key = walletCredits{Name: token.Name, UsedUSD: quotaToUSD(token.UsedQuota), Unlimited: token.UnlimitedQuota}
	if !token.UnlimitedQuota {
		out.Key.RemainingUSD = ptr(quotaToUSD(token.RemainQuota))
	}
	if token.ExpiredTime > 0 {
		out.Key.ExpiresAt = time.Unix(token.ExpiredTime, 0).UTC().Format(time.RFC3339)
	}

	// A workspace-bound key is paid by its organization's wallet; otherwise by
	// the owner's personal balance. The owner-level view honours the same
	// switch as the OpenAI-compatible billing endpoint.
	if info, wErr := model.GetWorkspaceBillingInfo(token.Id); wErr == nil && info != nil {
		out.BillingSource = "organization"
		if org, oErr := model.GetOrganizationById(info.OrgId); oErr == nil && org != nil {
			out.Wallet = &walletCredits{Name: org.Name, RemainingUSD: ptr(quotaToUSD(org.WalletQuota))}
		}
		return nil, out, nil
	}
	if !common.DisplayTokenStatEnabled {
		remaining, qErr := model.GetUserQuota(token.UserId, false)
		used, uErr := model.GetUserUsedQuota(token.UserId)
		if qErr == nil && uErr == nil {
			out.Wallet = &walletCredits{RemainingUSD: ptr(quotaToUSD(remaining)), UsedUSD: quotaToUSD(used)}
		}
	}
	return nil, out, nil
}

// ---- get-generation ------------------------------------------------------------

type generationInput struct {
	RequestId string `json:"request_id" jsonschema:"The request id returned in the X-Oneapi-Request-Id response header or by send-message"`
}

type generation struct {
	RequestId        string         `json:"request_id"`
	Model            string         `json:"model"`
	CreatedAt        string         `json:"created_at"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	CostUSD          float64        `json:"cost_usd"`
	LatencySeconds   int            `json:"latency_seconds"`
	Stream           bool           `json:"stream"`
	Group            string         `json:"group,omitempty"`
	Details          map[string]any `json:"details,omitempty" jsonschema:"Ratios and other billing details recorded with the call"`
}

// lookupGeneration finds one consume log by request id, scoped to the calling
// key so a key never sees another key's traffic. Admin-only diagnostics are
// removed, matching what the non-admin log views show.
func lookupGeneration(tokenId int, requestId string) (*generation, error) {
	requestId = strings.TrimSpace(requestId)
	if requestId == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	var row model.Log
	err := model.LOG_DB.Table("logs").
		Where("request_id = ? AND token_id = ? AND type = ?", requestId, tokenId, model.LogTypeConsume).
		Limit(1).Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.Id == 0 {
		return nil, fmt.Errorf("no generation %q for this API key (it may still be finalizing; retry in a moment)", requestId)
	}
	out := &generation{
		RequestId:        row.RequestId,
		Model:            row.ModelName,
		CreatedAt:        time.Unix(row.CreatedAt, 0).UTC().Format(time.RFC3339),
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		CostUSD:          quotaToUSD(row.Quota),
		LatencySeconds:   row.UseTime,
		Stream:           row.IsStream,
		Group:            row.Group,
	}
	if row.Other != "" {
		details := map[string]any{}
		if common.UnmarshalJsonStr(row.Other, &details) == nil {
			delete(details, "admin_info")
			out.Details = details
		}
	}
	return out, nil
}

func getGeneration(ctx context.Context, in generationInput) (*mcp.CallToolResult, generation, error) {
	c := ginFrom(ctx)
	gen, err := lookupGeneration(c.GetInt("token_id"), in.RequestId)
	if err != nil {
		return nil, generation{}, err
	}
	return nil, *gen, nil
}

// ---- list-daily-model-rankings --------------------------------------------------

type rankingsInput struct {
	Days  int `json:"days,omitempty" jsonschema:"Window in days ending now (default 7, max 30)"`
	Limit int `json:"limit,omitempty" jsonschema:"Maximum number of models (default 20, max 50)"`
}

type rankingRow struct {
	Rank     int    `json:"rank"`
	Model    string `json:"model"`
	Tokens   int64  `json:"tokens"`
	Requests int64  `json:"requests"`
}

type rankingsOutput struct {
	Days     int          `json:"days"`
	Since    string       `json:"since"`
	Rankings []rankingRow `json:"rankings"`
}

func listRankings(in rankingsInput) (*mcp.CallToolResult, rankingsOutput, error) {
	days := in.Days
	if days <= 0 {
		days = 7
	}
	if days > maxRankingDays {
		days = maxRankingDays
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > maxRankingLimit {
		limit = maxRankingLimit
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	type row struct {
		ModelName string
		TokenUsed int64
		Count     int64
	}
	var rows []row
	err := model.DB.Table("quota_data").
		Select("model_name, COALESCE(SUM(token_used), 0) AS token_used, COALESCE(SUM(count), 0) AS count").
		Where("created_at >= ? AND model_name <> ''", since).
		Group("model_name").
		Order("token_used DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, rankingsOutput{}, err
	}
	out := rankingsOutput{Days: days, Since: time.Unix(since, 0).UTC().Format(time.RFC3339), Rankings: []rankingRow{}}
	for i, r := range rows {
		out.Rankings = append(out.Rankings, rankingRow{Rank: i + 1, Model: r.ModelName, Tokens: r.TokenUsed, Requests: r.Count})
	}
	return nil, out, nil
}

// ---- send-message --------------------------------------------------------------

type chatMessage struct {
	Role    string `json:"role" jsonschema:"system, user or assistant"`
	Content string `json:"content"`
}

type sendMessageInput struct {
	Model       string        `json:"model" jsonschema:"Model id from list-models"`
	Messages    []chatMessage `json:"messages,omitempty" jsonschema:"Conversation so far; use this or prompt"`
	Prompt      string        `json:"prompt,omitempty" jsonschema:"Shortcut for a single user message"`
	System      string        `json:"system,omitempty" jsonschema:"Optional system prompt prepended to the conversation"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
}

type sendMessageOutput struct {
	RequestId    string   `json:"request_id" jsonschema:"Pass to get-generation for the final billed cost"`
	Model        string   `json:"model" jsonschema:"Model reported by the upstream response"`
	Content      string   `json:"content"`
	FinishReason string   `json:"finish_reason,omitempty"`
	Usage        *usage   `json:"usage,omitempty"`
	CostUSD      *float64 `json:"cost_usd,omitempty" jsonschema:"Present when the charge was already recorded; otherwise use get-generation"`
	LatencyMs    int64    `json:"latency_ms"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func sendMessage(ctx context.Context, engine *gin.Engine, in sendMessageInput) (*mcp.CallToolResult, sendMessageOutput, error) {
	c := ginFrom(ctx)
	if strings.TrimSpace(in.Model) == "" {
		return nil, sendMessageOutput{}, fmt.Errorf("model is required")
	}
	messages := make([]map[string]string, 0, len(in.Messages)+2)
	if in.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": in.System})
	}
	for _, m := range in.Messages {
		if m.Role == "" || m.Content == "" {
			return nil, sendMessageOutput{}, fmt.Errorf("every message needs a role and content")
		}
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	if in.Prompt != "" {
		messages = append(messages, map[string]string{"role": "user", "content": in.Prompt})
	}
	if len(messages) == 0 {
		return nil, sendMessageOutput{}, fmt.Errorf("provide prompt or messages")
	}
	if len(messages) > maxChatMessages {
		return nil, sendMessageOutput{}, fmt.Errorf("at most %d messages per call", maxChatMessages)
	}
	body := map[string]any{"model": in.Model, "messages": messages, "stream": false}
	if in.MaxTokens != nil {
		body["max_tokens"] = *in.MaxTokens
	}
	if in.Temperature != nil {
		body["temperature"] = *in.Temperature
	}
	payload, err := common.Marshal(body)
	if err != nil {
		return nil, sendMessageOutput{}, err
	}

	started := time.Now()
	res, err := dispatch(ctx, engine, http.MethodPost, "/v1/chat/completions", payload)
	if err != nil {
		return nil, sendMessageOutput{}, err
	}
	if res.Status != http.StatusOK {
		return nil, sendMessageOutput{}, fmt.Errorf("chat request failed: %s", apiErrorMessage(res))
	}
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *usage `json:"usage"`
	}
	if err := common.Unmarshal(res.Body, &resp); err != nil {
		return nil, sendMessageOutput{}, fmt.Errorf("chat response unreadable: %w", err)
	}
	out := sendMessageOutput{
		RequestId: res.Header.Get(common.RequestIdKey),
		Model:     resp.Model,
		Usage:     resp.Usage,
		LatencyMs: time.Since(started).Milliseconds(),
	}
	if out.Model == "" {
		out.Model = in.Model
	}
	if len(resp.Choices) > 0 {
		out.FinishReason = resp.Choices[0].FinishReason
		out.Content = contentText(resp.Choices[0].Message.Content)
	}
	// The consume log is written asynchronously; report the cost when it is
	// already there and otherwise point the agent at get-generation.
	if out.RequestId != "" {
		if gen, gErr := lookupGeneration(c.GetInt("token_id"), out.RequestId); gErr == nil {
			out.CostUSD = ptr(gen.CostUSD)
		}
	}
	return nil, out, nil
}

// contentText flattens an OpenAI message content (string or content parts).
func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		}
		return sb.String()
	default:
		return ""
	}
}
