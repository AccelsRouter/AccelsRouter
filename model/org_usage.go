package model

// Fork-only organization usage reporting. Key-level billing means the org's
// billable usage is exactly the consume logs of the API tokens bound to its
// workspaces. Because the log database MAY be a physically separate database
// from the main DB (LOG_DB != DB on split deployments), this never joins logs
// against workspace_tokens in SQL: it resolves the org's token set from the
// main DB, then aggregates the logs by token_id from LOG_DB, and folds the
// workspace/member attribution back together in Go.

import (
	"sort"

	"gorm.io/gorm"
)

// OrgUsageBucket is one aggregated line of a usage report.
type OrgUsageBucket struct {
	Key              string `json:"key"`   // workspace name / model name / member label
	Quota            int64  `json:"quota"` // summed quota (billing units)
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	// RetailQuota is the reseller's retail amount for this line = Quota × the
	// customer's matched model-series discount. Populated only on the reseller's
	// customer statement (see ApplyRetailDiscounts); 0/omitted otherwise.
	RetailQuota int64 `json:"retail_quota,omitempty"`
}

// OrgUsageReport is the full breakdown over a time window.
type OrgUsageReport struct {
	OrgId           int              `json:"org_id"`
	From            int64            `json:"from"`
	To              int64            `json:"to"`
	TotalQuota      int64            `json:"total_quota"`
	TotalRequests   int64            `json:"total_requests"`
	TotalPrompt     int64            `json:"total_prompt_tokens"`
	TotalCompletion int64            `json:"total_completion_tokens"`
	ByWorkspace     []OrgUsageBucket `json:"by_workspace"`
	ByModel         []OrgUsageBucket `json:"by_model"`
	ByMember        []OrgUsageBucket `json:"by_member"`
	// TotalRetailQuota is the reseller's retail total = sum of per-model retail
	// (each model's standard quota × its matched discount). Populated only on a
	// reseller customer statement.
	TotalRetailQuota int64 `json:"total_retail_quota,omitempty"`
}

// ApplyRetailDiscounts overlays a reseller's per-model-series retail discount
// onto a usage report: for each model line it computes RetailQuota = Quota ×
// matched ratio, and sums TotalRetailQuota. Pure reporting — it never changes
// what the platform billed. A model line's Key is the model name.
func (r *OrgUsageReport) ApplyRetailDiscounts(discounts map[string]float64) {
	if len(discounts) == 0 {
		return
	}
	var total int64
	for i := range r.ByModel {
		ratio := RetailDiscountFor(r.ByModel[i].Key, discounts)
		retail := int64(float64(r.ByModel[i].Quota) * ratio)
		r.ByModel[i].RetailQuota = retail
		total += retail
	}
	r.TotalRetailQuota = total
}

// logUsageRow is one grouped row from LOG_DB.
type logUsageRow struct {
	TokenId          int
	ModelName        string
	Quota            int64
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
}

// ListOrgLogs returns the org's individual consume call records (newest first,
// paginated) for [from, to]. Cross-DB safe: the org's token ids are resolved
// from the main DB, and the logs are read from LOG_DB by token_id (no join),
// mirroring GetOrgUsage.
func ListOrgLogs(orgId int, from, to int64, startIdx, num int) ([]*Log, int64, error) {
	var bindings []WorkspaceToken
	if err := DB.Where("org_id = ?", orgId).Find(&bindings).Error; err != nil {
		return nil, 0, err
	}
	if len(bindings) == 0 {
		return []*Log{}, 0, nil
	}
	tokenIds := make([]int, 0, len(bindings))
	for _, b := range bindings {
		tokenIds = append(tokenIds, b.TokenId)
	}
	tx := LOG_DB.Model(&Log{}).Where("token_id IN ?", tokenIds).Where("type = ?", LogTypeConsume)
	if from > 0 {
		tx = tx.Where("created_at >= ?", from)
	}
	if to > 0 {
		tx = tx.Where("created_at <= ?", to)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []*Log
	if err := tx.Order("id desc").Limit(num).Offset(startIdx).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	// Strip admin-only fields (Other.admin_info/audit_info, ChannelName) — these
	// viewers (reseller admin, org owner/admin) are not platform admins.
	formatUserLogs(logs, startIdx)
	return logs, total, nil
}

// GetOrgUsage aggregates the org's billed usage between [from, to] (unix
// seconds; a zero bound is treated as open). The result is deterministic:
// each breakdown is sorted by descending quota then key.
func GetOrgUsage(orgId int, from, to int64) (*OrgUsageReport, error) {
	report := &OrgUsageReport{OrgId: orgId, From: from, To: to}

	// 1. Resolve the org's bound tokens (main DB).
	var bindings []WorkspaceToken
	if err := DB.Where("org_id = ?", orgId).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return report, nil
	}
	tokenIds := make([]int, 0, len(bindings))
	tokenToWorkspace := make(map[int]int, len(bindings))
	for _, b := range bindings {
		tokenIds = append(tokenIds, b.TokenId)
		tokenToWorkspace[b.TokenId] = b.WorkspaceId
	}

	// Workspace id -> display name.
	var workspaces []Workspace
	if err := DB.Where("org_id = ?", orgId).Find(&workspaces).Error; err != nil {
		return nil, err
	}
	workspaceName := make(map[int]string, len(workspaces))
	for _, ws := range workspaces {
		workspaceName[ws.Id] = ws.Name
	}

	// Token id -> owner user id (for member attribution).
	var tokens []Token
	if err := DB.Select("id", "user_id").Where("id IN ?", tokenIds).Find(&tokens).Error; err != nil {
		return nil, err
	}
	tokenToUser := make(map[int]int, len(tokens))
	userIds := make(map[int]struct{})
	for _, t := range tokens {
		tokenToUser[t.Id] = t.UserId
		userIds[t.UserId] = struct{}{}
	}
	memberLabel := make(map[int]string, len(userIds))
	if len(userIds) > 0 {
		ids := make([]int, 0, len(userIds))
		for id := range userIds {
			ids = append(ids, id)
		}
		var users []User
		if err := DB.Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, u := range users {
			memberLabel[u.Id] = u.Username
		}
	}

	// 2. Aggregate logs grouped by (token_id, model_name) from LOG_DB.
	rows, err := queryOrgUsageLogs(tokenIds, from, to)
	if err != nil {
		return nil, err
	}

	// 3. Fold into the three breakdowns.
	byWorkspace := map[string]*OrgUsageBucket{}
	byModel := map[string]*OrgUsageBucket{}
	byMember := map[string]*OrgUsageBucket{}
	accumulate := func(m map[string]*OrgUsageBucket, key string, r logUsageRow) {
		b := m[key]
		if b == nil {
			b = &OrgUsageBucket{Key: key}
			m[key] = b
		}
		b.Quota += r.Quota
		b.Requests += r.Requests
		b.PromptTokens += r.PromptTokens
		b.CompletionTokens += r.CompletionTokens
	}
	for _, r := range rows {
		report.TotalQuota += r.Quota
		report.TotalRequests += r.Requests
		report.TotalPrompt += r.PromptTokens
		report.TotalCompletion += r.CompletionTokens

		wsName := workspaceName[tokenToWorkspace[r.TokenId]]
		if wsName == "" {
			wsName = "(unassigned)"
		}
		accumulate(byWorkspace, wsName, r)

		model := r.ModelName
		if model == "" {
			model = "(unknown)"
		}
		accumulate(byModel, model, r)

		member := memberLabel[tokenToUser[r.TokenId]]
		if member == "" {
			member = "(unknown)"
		}
		accumulate(byMember, member, r)
	}

	report.ByWorkspace = sortedBuckets(byWorkspace)
	report.ByModel = sortedBuckets(byModel)
	report.ByMember = sortedBuckets(byMember)
	return report, nil
}

// queryOrgUsageLogs runs the grouped aggregation against LOG_DB. Split out so
// the token-set batching stays in one place; token sets are bounded per org.
func queryOrgUsageLogs(tokenIds []int, from, to int64) ([]logUsageRow, error) {
	tx := LOG_DB.Table("logs").
		Select("token_id, model_name, "+
			"COALESCE(SUM(quota),0) as quota, "+
			"COUNT(*) as requests, "+
			"COALESCE(SUM(prompt_tokens),0) as prompt_tokens, "+
			"COALESCE(SUM(completion_tokens),0) as completion_tokens").
		Where("type = ?", LogTypeConsume).
		Where("token_id IN ?", tokenIds)
	if from > 0 {
		tx = tx.Where("created_at >= ?", from)
	}
	if to > 0 {
		tx = tx.Where("created_at <= ?", to)
	}
	tx = tx.Group("token_id, model_name")

	var rows []logUsageRow
	if err := tx.Scan(&rows).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return rows, nil
}

func sortedBuckets(m map[string]*OrgUsageBucket) []OrgUsageBucket {
	out := make([]OrgUsageBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Quota != out[j].Quota {
			return out[i].Quota > out[j].Quota
		}
		return out[i].Key < out[j].Key
	})
	return out
}
