// Fork-only immutable rollup of org/reseller billing actuals. Unlike the usage
// reports that re-derived retail/cost by applying CURRENT ratios to historical
// standard consumption (which made past reports drift whenever a wholesale or
// retail ratio changed), this table records, at settlement time, the ACTUAL
// amounts for each org-billed request:
//   - standard_quota : the platform standard-price quota
//   - charged_quota  : what the customer actually paid (retail, ratio at call time)
//   - cost_quota     : what it cost the reseller (wholesale, ratio at call time)
// Day-bucketed and keyed by org/workspace/model/user so every report dimension
// (by model, by member, by workspace, by customer, by reseller) aggregates fast
// with plain sums, and changing a ratio only affects FUTURE calls. Lives in the
// main DB (org tables' home); new-api's own logs/quota_data are untouched.
package model

import (
	"strconv"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

type OrgUsageDaily struct {
	Id            int    `json:"id" gorm:"primarykey"`
	DayBucket     int64  `json:"day_bucket" gorm:"uniqueIndex:idx_oud_key,priority:1;not null"`
	OrgId         int    `json:"org_id" gorm:"uniqueIndex:idx_oud_key,priority:2;index;not null"`
	WorkspaceId   int    `json:"workspace_id" gorm:"uniqueIndex:idx_oud_key,priority:3;not null"`
	ModelName     string `json:"model_name" gorm:"type:varchar(255);uniqueIndex:idx_oud_key,priority:4;not null"`
	UserId        int    `json:"user_id" gorm:"uniqueIndex:idx_oud_key,priority:5;not null"`
	// ResellerOrgId is denormalized (functionally determined by OrgId) so the
	// admin/reseller aggregation can group by it directly. 0 = not a reseller
	// customer.
	ResellerOrgId    int   `json:"reseller_org_id" gorm:"index"`
	StandardQuota    int64 `json:"standard_quota"`
	ChargedQuota     int64 `json:"charged_quota"`
	CostQuota        int64 `json:"cost_quota"`
	Requests         int64 `json:"requests"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

func (OrgUsageDaily) TableName() string { return "org_usage_daily" }

// dayBucketOf floors a unix-seconds timestamp to the start of its UTC day.
func dayBucketOf(ts int64) int64 {
	if ts <= 0 {
		ts = common.GetTimestamp()
	}
	return ts - (ts % 86400)
}

// RecordOrgUsageDaily upserts (increments) one day/org/workspace/model/user row
// with the actual amounts for a settled org-billed request. Cross-DB safe:
// UPDATE-then-INSERT with a single retry to absorb the first-write race.
func RecordOrgUsageDaily(createdAt int64, orgId, workspaceId, resellerOrgId, userId int, modelName string, standard, charged, cost, prompt, completion int64) error {
	if orgId <= 0 {
		return nil
	}
	if modelName == "" {
		modelName = "unknown"
	}
	day := dayBucketOf(createdAt)

	increment := func() (int64, error) {
		res := DB.Model(&OrgUsageDaily{}).
			Where("day_bucket = ? AND org_id = ? AND workspace_id = ? AND model_name = ? AND user_id = ?",
				day, orgId, workspaceId, modelName, userId).
			Updates(map[string]interface{}{
				"standard_quota":    gorm.Expr("standard_quota + ?", standard),
				"charged_quota":     gorm.Expr("charged_quota + ?", charged),
				"cost_quota":        gorm.Expr("cost_quota + ?", cost),
				"requests":          gorm.Expr("requests + 1"),
				"prompt_tokens":     gorm.Expr("prompt_tokens + ?", prompt),
				"completion_tokens": gorm.Expr("completion_tokens + ?", completion),
				"reseller_org_id":   resellerOrgId,
			})
		return res.RowsAffected, res.Error
	}

	affected, err := increment()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	// No existing row: insert. On a unique-conflict race, retry the increment.
	createErr := DB.Create(&OrgUsageDaily{
		DayBucket:        day,
		OrgId:            orgId,
		WorkspaceId:      workspaceId,
		ModelName:        modelName,
		UserId:           userId,
		ResellerOrgId:    resellerOrgId,
		StandardQuota:    standard,
		ChargedQuota:     charged,
		CostQuota:        cost,
		Requests:         1,
		PromptTokens:     prompt,
		CompletionTokens: completion,
	}).Error
	if createErr == nil {
		return nil
	}
	if affected2, err2 := increment(); err2 == nil && affected2 > 0 {
		return nil
	}
	return createErr
}

// oudRow is a fetched daily row for in-Go aggregation (bounded: daily grain).
type oudRow struct {
	OrgId            int
	WorkspaceId      int
	ModelName        string
	UserId           int
	ResellerOrgId    int
	StandardQuota    int64
	ChargedQuota     int64
	CostQuota        int64
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
}

func fetchOrgUsageDaily(where string, args []interface{}, from, to int64) ([]oudRow, error) {
	q := DB.Model(&OrgUsageDaily{}).Where(where, args...)
	if from > 0 {
		q = q.Where("day_bucket >= ?", dayBucketOf(from))
	}
	if to > 0 {
		q = q.Where("day_bucket <= ?", to)
	}
	var rows []oudRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func oudAccumulate(m map[string]*OrgUsageBucket, key string, r oudRow) {
	b, ok := m[key]
	if !ok {
		b = &OrgUsageBucket{Key: key}
		m[key] = b
	}
	b.Quota += r.StandardQuota
	b.RetailQuota += r.ChargedQuota
	b.CostQuota += r.CostQuota
	b.Requests += r.Requests
	b.PromptTokens += r.PromptTokens
	b.CompletionTokens += r.CompletionTokens
}

// GetOrgUsageFromDaily builds an org's usage report from the immutable rollup:
// by workspace, by model, by member — with standard / charged (retail) / cost.
func GetOrgUsageFromDaily(orgId int, from, to int64) (*OrgUsageReport, error) {
	report := &OrgUsageReport{
		OrgId:       orgId,
		From:        from,
		To:          to,
		ByWorkspace: []OrgUsageBucket{},
		ByModel:     []OrgUsageBucket{},
		ByMember:    []OrgUsageBucket{},
	}
	rows, err := fetchOrgUsageDaily("org_id = ?", []interface{}{orgId}, from, to)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return report, nil
	}
	byWs := map[string]*OrgUsageBucket{}
	byModel := map[string]*OrgUsageBucket{}
	byMember := map[string]*OrgUsageBucket{}
	wsIds := map[int]struct{}{}
	userIds := map[int]struct{}{}
	for _, r := range rows {
		wsIds[r.WorkspaceId] = struct{}{}
		userIds[r.UserId] = struct{}{}
	}
	wsName := workspaceNames(wsIds)
	userName := userNames(userIds)
	for _, r := range rows {
		report.TotalQuota += r.StandardQuota
		report.TotalRetailQuota += r.ChargedQuota
		report.TotalCostQuota += r.CostQuota
		report.TotalRequests += r.Requests
		report.TotalPrompt += r.PromptTokens
		report.TotalCompletion += r.CompletionTokens
		oudAccumulate(byWs, wsName[r.WorkspaceId], r)
		oudAccumulate(byModel, r.ModelName, r)
		oudAccumulate(byMember, userName[r.UserId], r)
	}
	report.ByWorkspace = sortedBuckets(byWs)
	report.ByModel = sortedBuckets(byModel)
	report.ByMember = sortedBuckets(byMember)
	return report, nil
}

// GetResellerUsageFromDaily builds a reseller's aggregated report from the
// rollup: by model, by member, and by CUSTOMER (in ByWorkspace) — with cost.
func GetResellerUsageFromDaily(resellerOrgId int, from, to int64) (*OrgUsageReport, error) {
	report := &OrgUsageReport{
		OrgId:       resellerOrgId,
		From:        from,
		To:          to,
		ByWorkspace: []OrgUsageBucket{},
		ByModel:     []OrgUsageBucket{},
		ByMember:    []OrgUsageBucket{},
	}
	rows, err := fetchOrgUsageDaily("reseller_org_id = ?", []interface{}{resellerOrgId}, from, to)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return report, nil
	}
	byCustomer := map[string]*OrgUsageBucket{}
	byModel := map[string]*OrgUsageBucket{}
	byMember := map[string]*OrgUsageBucket{}
	orgIds := map[int]struct{}{}
	userIds := map[int]struct{}{}
	for _, r := range rows {
		orgIds[r.OrgId] = struct{}{}
		userIds[r.UserId] = struct{}{}
	}
	orgName := orgNames(orgIds)
	userName := userNames(userIds)
	for _, r := range rows {
		report.TotalQuota += r.StandardQuota
		report.TotalRetailQuota += r.ChargedQuota
		report.TotalCostQuota += r.CostQuota
		report.TotalRequests += r.Requests
		report.TotalPrompt += r.PromptTokens
		report.TotalCompletion += r.CompletionTokens
		oudAccumulate(byCustomer, orgName[r.OrgId], r)
		oudAccumulate(byModel, r.ModelName, r)
		oudAccumulate(byMember, userName[r.UserId], r)
	}
	report.ByWorkspace = sortedBuckets(byCustomer)
	report.ByModel = sortedBuckets(byModel)
	report.ByMember = sortedBuckets(byMember)
	return report, nil
}

// --- name resolution helpers (small, bounded lookups) ---

func workspaceNames(ids map[int]struct{}) map[int]string {
	out := map[int]string{}
	if len(ids) == 0 {
		return out
	}
	list := make([]int, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var ws []Workspace
	if err := DB.Select("id", "name").Where("id IN ?", list).Find(&ws).Error; err == nil {
		for _, w := range ws {
			out[w.Id] = w.Name
		}
	}
	for id := range ids {
		if out[id] == "" {
			out[id] = "Default"
		}
	}
	return out
}

func userNames(ids map[int]struct{}) map[int]string {
	out := map[int]string{}
	if len(ids) == 0 {
		return out
	}
	list := make([]int, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	type row struct {
		Id       int
		Username string
	}
	var rows []row
	if err := DB.Table("users").Select("id, username").Where("id IN ?", list).Scan(&rows).Error; err == nil {
		for _, r := range rows {
			out[r.Id] = r.Username
		}
	}
	for id := range ids {
		if out[id] == "" {
			out[id] = "#" + strconv.Itoa(id)
		}
	}
	return out
}

func orgNames(ids map[int]struct{}) map[int]string {
	out := map[int]string{}
	if len(ids) == 0 {
		return out
	}
	list := make([]int, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var orgs []Organization
	if err := DB.Select("id", "name").Where("id IN ?", list).Find(&orgs).Error; err == nil {
		for _, o := range orgs {
			out[o.Id] = o.Name
		}
	}
	for id := range ids {
		if out[id] == "" {
			out[id] = "#" + strconv.Itoa(id)
		}
	}
	return out
}
