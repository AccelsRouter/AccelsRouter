// Fork-only immutable rollup of org/reseller billing actuals. Unlike the usage
// reports that re-derived retail/cost by applying CURRENT ratios to historical
// standard consumption (which made past reports drift whenever a wholesale or
// retail ratio changed), this table records, at settlement time, the ACTUAL
// amounts for each org-billed request:
//   - standard_quota : the platform standard-price quota
//   - charged_quota  : what the customer actually paid (retail, ratio at call time)
//   - cost_quota     : what it cost the reseller (wholesale, ratio at call time)
// Hour-bucketed (the same grain as new-api's quota_data.created_at, see
// usedata.go) and keyed by org/workspace/model/user so every report dimension
// (by model, by member, by workspace, by customer, by reseller) aggregates fast
// with plain sums, and changing a ratio only affects FUTURE calls. The hour
// grain matters: a UTC *day* bucket made a client's local "today" (e.g. GMT+8)
// also pull in the whole previous UTC day; an hour bucket lines up exactly with
// any whole-hour-offset timezone's day. Lives in the
// main DB (org tables' home); new-api's own logs/quota_data are untouched.
package model

import (
	"strconv"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

type OrgUsageDaily struct {
	Id            int    `json:"id" gorm:"primarykey"`
	// DayBucket holds the bucket START at hour grain (see usageBucketOf). The
	// column keeps its original day_bucket name for schema stability across
	// SQLite/MySQL/PostgreSQL; renaming would need a hand-written migration.
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

// usageBucketOf floors a unix-seconds timestamp to the start of its hour — the
// same grain new-api's quota_data uses (usedata.go), so a whole-hour-offset
// timezone's local day maps onto exactly 24 buckets with no spill-over.
func usageBucketOf(ts int64) int64 {
	if ts <= 0 {
		ts = common.GetTimestamp()
	}
	return ts - (ts % 3600)
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
	day := usageBucketOf(createdAt)

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
		q = q.Where("day_bucket >= ?", usageBucketOf(from))
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
	// Customer rows carry reseller_org_id; the reseller's OWN key rows are
	// billed to the reseller org itself (org_id), so both are its usage.
	rows, err := fetchOrgUsageDaily("reseller_org_id = ? OR org_id = ?", []interface{}{resellerOrgId, resellerOrgId}, from, to)
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
		// The reseller's OWN key rows: it paid its wholesale cost itself, so
		// charged == cost and there is no margin. Rows written before own calls
		// were tagged carry cost 0 with charged = the wholesale amount; read
		// them under the same rule instead of rewriting the immutable rollup.
		if r.OrgId == resellerOrgId {
			if r.CostQuota == 0 {
				r.CostQuota = r.ChargedQuota
			}
			r.ChargedQuota = r.CostQuota
		}
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

// BackfillOrgUsageDaily seeds the rollup from historical consume logs so the
// org/reseller usage reports show the same history the raw call records already
// have (call records read the logs table directly; the rollup only gains rows
// from the settle path, which started when this feature shipped). It maps a log
// to an org via its WorkspaceToken binding (org_id -> token_id), the same link
// the call-record queries use.
//
// The going-forward settle path records exact call-time actuals; this one-time
// seed instead uses the CURRENT retail/wholesale ratios for charged/cost
// (historical wholesale was never stored per call). That approximation is
// acceptable only while there are no production customers — it covers
// pre-rollup test traffic. To stay idempotent and never double-count or clobber
// the accurate rows the settle path already wrote, it backfills only logs from
// days STRICTLY EARLIER than the earliest existing rollup row (all days when the
// table is empty). Re-running is then a no-op.
func BackfillOrgUsageDaily() error {
	var minRow struct{ Min *int64 }
	if err := DB.Model(&OrgUsageDaily{}).
		Select("MIN(day_bucket) as min").Scan(&minRow).Error; err != nil {
		return err
	}
	cutoff := int64(0) // 0 = no existing rows, backfill every log
	if minRow.Min != nil {
		cutoff = *minRow.Min
	}

	var bindings []WorkspaceToken
	if err := DB.Find(&bindings).Error; err != nil {
		return err
	}
	if len(bindings) == 0 {
		return nil
	}
	type owner struct{ orgId, workspaceId int }
	tokenOwner := make(map[int]owner, len(bindings))
	tokenIds := make([]int, 0, len(bindings))
	for _, b := range bindings {
		tokenOwner[b.TokenId] = owner{orgId: b.OrgId, workspaceId: b.WorkspaceId}
		tokenIds = append(tokenIds, b.TokenId)
	}

	var links []ResellerCustomerLink
	if err := DB.Find(&links).Error; err != nil {
		return err
	}
	resellerOf := make(map[int]int, len(links))
	for _, l := range links {
		resellerOf[l.CustomerOrgId] = l.ResellerOrgId
	}
	// A reseller org's own keys are reseller traffic too: the reseller is its
	// own "reseller" and its cost is the wholesale amount it paid.
	var resellerOrgIds []int
	if err := DB.Model(&Organization{}).Where("type = ?", OrgTypeReseller).Pluck("id", &resellerOrgIds).Error; err != nil {
		return err
	}
	for _, id := range resellerOrgIds {
		resellerOf[id] = id
	}

	// Lazily-resolved per-org ratio maps (a handful of orgs, memoized).
	retailByOrg := map[int]map[string]float64{}
	wholesaleByReseller := map[int]map[string]float64{}
	retailFor := func(orgId int) map[string]float64 {
		if m, ok := retailByOrg[orgId]; ok {
			return m
		}
		var m map[string]float64
		if org, err := GetOrganizationById(orgId); err == nil && org != nil {
			m = ParseRetailDiscounts(org.RetailDiscounts)
		}
		retailByOrg[orgId] = m
		return m
	}
	wholesaleFor := func(resellerId int) map[string]float64 {
		if m, ok := wholesaleByReseller[resellerId]; ok {
			return m
		}
		var m map[string]float64
		if org, err := GetOrganizationById(resellerId); err == nil && org != nil {
			m = ParseRetailDiscounts(org.WholesaleRatios)
		}
		wholesaleByReseller[resellerId] = m
		return m
	}

	logQuery := LOG_DB.Model(&Log{}).
		Where("token_id IN ?", tokenIds).
		Where("type = ?", LogTypeConsume)
	if cutoff > 0 {
		logQuery = logQuery.Where("created_at < ?", cutoff)
	}
	var logs []Log
	if err := logQuery.Find(&logs).Error; err != nil {
		return err
	}
	if len(logs) == 0 {
		return nil
	}

	type aggKey struct {
		day         int64
		orgId       int
		workspaceId int
		model       string
		userId      int
	}
	agg := map[aggKey]*OrgUsageDaily{}
	for i := range logs {
		l := &logs[i]
		own, ok := tokenOwner[l.TokenId]
		if !ok || own.orgId <= 0 {
			continue
		}
		modelName := l.ModelName
		if modelName == "" {
			modelName = "unknown"
		}
		day := usageBucketOf(l.CreatedAt)
		resellerId := resellerOf[own.orgId]
		std := int64(l.Quota)
		charged := std
		if r := RetailDiscountFor(modelName, retailFor(own.orgId)); r > 0 && r < 1 {
			charged = int64(common.QuotaRound(float64(l.Quota) * r))
		}
		var cost int64
		if resellerId > 0 {
			cost = std
			if r := WholesaleRatioFor(modelName, wholesaleFor(resellerId)); r > 0 && r < 1 {
				cost = int64(common.QuotaRound(float64(l.Quota) * r))
			}
			if resellerId == own.orgId {
				charged = cost // the reseller's own call: it paid exactly its cost
			}
		}
		k := aggKey{day: day, orgId: own.orgId, workspaceId: own.workspaceId, model: modelName, userId: l.UserId}
		row := agg[k]
		if row == nil {
			row = &OrgUsageDaily{
				DayBucket:     day,
				OrgId:         own.orgId,
				WorkspaceId:   own.workspaceId,
				ModelName:     modelName,
				UserId:        l.UserId,
				ResellerOrgId: resellerId,
			}
			agg[k] = row
		}
		row.StandardQuota += std
		row.ChargedQuota += charged
		row.CostQuota += cost
		row.Requests++
		row.PromptTokens += int64(l.PromptTokens)
		row.CompletionTokens += int64(l.CompletionTokens)
	}
	if len(agg) == 0 {
		return nil
	}
	rows := make([]*OrgUsageDaily, 0, len(agg))
	for _, r := range agg {
		rows = append(rows, r)
	}
	if err := DB.CreateInBatches(rows, 200).Error; err != nil {
		return err
	}
	common.SysLog("org_usage_daily backfill complete: " + strconv.Itoa(len(rows)) + " rows")
	return nil
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
