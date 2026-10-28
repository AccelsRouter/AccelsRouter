// Admin financial reconciliation: multi-dimensional consumption aggregation for
// a time window, plus what the platform actually received. Built on the
// pre-aggregated quota_data rollup (hour-bucketed, keyed by
// model/channel/group/user) so it stays fast without scanning raw logs.
//
// Money model. quota_data and the consume log always carry the STANDARD price.
// Reseller traffic (a reseller's customers and the reseller's own keys) is
// paid to the platform from the reseller wallet at WHOLESALE (org_usage_daily
// .cost_quota), so for the platform:
//
//	standard = wholesale discount + received
//
// The retail price a customer pays its reseller (org_usage_daily.charged_quota)
// is the reseller's business and is shown per reseller for reference only.
//
// "Upstream" here means per-channel consumption (which upstream channel routed
// how much) — the platform does not track a separate upstream USD cost.
package model

import (
	"sort"
	"strconv"
	"time"
)

// ReconRow is one dimension bucket (model / channel / group / user).
type ReconRow struct {
	Key      string `json:"key" gorm:"column:dim"`
	Quota    int64  `json:"quota"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
}

// ReconMatrixRow is one model on one channel: which upstream served how much
// of a model, so a vendor bill can be checked model by model.
type ReconMatrixRow struct {
	Model     string `json:"model"`
	ChannelId int    `json:"channel_id"`
	Channel   string `json:"channel"`
	Quota     int64  `json:"quota"`
	Requests  int64  `json:"requests"`
	Tokens    int64  `json:"tokens"`
}

// ReconResellerRow is one reseller's reconciliation, aggregated over all of
// that reseller's traffic (its customers plus its own keys).
type ReconResellerRow struct {
	OrgId         int    `json:"org_id"`
	Name          string `json:"name"`
	StandardQuota int64  `json:"standard_quota"`
	DiscountQuota int64  `json:"discount_quota"` // wholesale discount: standard - received
	ReceivedQuota int64  `json:"received_quota"` // what the platform received (wholesale)
	ChargedQuota  int64  `json:"charged_quota"`  // what customers paid the reseller (retail), reference
	Requests      int64  `json:"requests"`
}

// ReconSeriesPoint is one time bucket of total consumption.
type ReconSeriesPoint struct {
	Period   int64 `json:"period"` // unix seconds at bucket start
	Quota    int64 `json:"quota"`
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
}

// ReconSummary is the headline reconciliation for the window.
type ReconSummary struct {
	StandardQuota int64 `json:"standard_quota"` // platform standard-price consumption
	DiscountQuota int64 `json:"discount_quota"` // wholesale discount given to resellers
	ReceivedQuota int64 `json:"received_quota"` // standard - discount: what the platform received
	Requests      int64 `json:"requests"`
	Tokens        int64 `json:"tokens"`
	Channels      int64 `json:"channels"`
}

// ReconReport is the full reconciliation payload.
type ReconReport struct {
	From      int64        `json:"from"`
	To        int64        `json:"to"`
	Summary   ReconSummary `json:"summary"`
	ByModel   []ReconRow   `json:"by_model"`
	ByChannel []ReconRow   `json:"by_channel"`
	// Every (model, channel) pair of the window, largest first.
	ByModelChannel []ReconMatrixRow   `json:"by_model_channel"`
	ByGroup        []ReconRow         `json:"by_group"`
	ByUser         []ReconRow         `json:"by_user"`
	ByReseller     []ReconResellerRow `json:"by_reseller"`
	Series         []ReconSeriesPoint `json:"series"`
}

const reconDimLimit = 200

// reconMatrixLimit caps the model × channel pairs; a window rarely has more
// than a few hundred, and the summary stays exact regardless.
const reconMatrixLimit = 2000

// reconTotals returns the full-window totals (no grouping/limit) so the summary
// is exact even when a dimension breakdown is capped at reconDimLimit rows.
func reconTotals(from, to int64) (quota, requests, tokens, channels int64, err error) {
	type totalRow struct {
		Quota    int64
		Requests int64
		Tokens   int64
		Channels int64
	}
	var tr totalRow
	err = DB.Table("quota_data").
		// Rows written before channel_id existed carry 0; don't count that as a channel.
		Select("COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens, COUNT(DISTINCT CASE WHEN channel_id > 0 THEN channel_id END) as channels").
		Where("created_at >= ? and created_at <= ?", from, to).
		Scan(&tr).Error
	return tr.Quota, tr.Requests, tr.Tokens, tr.Channels, err
}

// reconByColumn aggregates quota_data over [from,to] grouped by keyCol, top rows
// by quota. keyCol must be a trusted column name (never user input).
func reconByColumn(from, to int64, keyCol string) ([]ReconRow, error) {
	rows := make([]ReconRow, 0)
	err := DB.Table("quota_data").
		Select(keyCol+" as dim, COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens").
		Where("created_at >= ? and created_at <= ?", from, to).
		Group(keyCol).
		Order("quota desc").
		Limit(reconDimLimit).
		Scan(&rows).Error
	return rows, err
}

// reconChannelRows aggregates by channel_id then resolves channel display names.
func reconChannelRows(from, to int64) ([]ReconRow, error) {
	type chanScan struct {
		ChannelId int64
		Quota     int64
		Requests  int64
		Tokens    int64
	}
	var scans []chanScan
	if err := DB.Table("quota_data").
		Select("channel_id, COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens").
		Where("created_at >= ? and created_at <= ?", from, to).
		Group("channel_id").
		Order("quota desc").
		Limit(reconDimLimit).
		Scan(&scans).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(scans))
	for _, s := range scans {
		ids = append(ids, int(s.ChannelId))
	}
	names := reconChannelNames(ids)
	rows := make([]ReconRow, 0, len(scans))
	for _, s := range scans {
		rows = append(rows, ReconRow{Key: names[int(s.ChannelId)], Quota: s.Quota, Requests: s.Requests, Tokens: s.Tokens})
	}
	return rows, nil
}

// reconChannelNames resolves channel display names; a deleted channel (or the
// 0 of rows written before channel_id existed) shows as "#<id>".
func reconChannelNames(ids []int) map[int]string {
	names := map[int]string{}
	lookup := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			lookup = append(lookup, id)
		}
	}
	if len(lookup) > 0 {
		type nameRow struct {
			Id   int
			Name string
		}
		var nrs []nameRow
		if err := DB.Table("channels").Select("id, name").Where("id IN ?", lookup).Scan(&nrs).Error; err == nil {
			for _, n := range nrs {
				names[n.Id] = n.Name
			}
		}
	}
	for _, id := range ids {
		if names[id] == "" {
			names[id] = "#" + strconv.Itoa(id)
		}
	}
	return names
}

// reconMatrixRows aggregates quota_data by (model, channel) over [from,to].
func reconMatrixRows(from, to int64) ([]ReconMatrixRow, error) {
	type scan struct {
		ModelName string
		ChannelId int64
		Quota     int64
		Requests  int64
		Tokens    int64
	}
	var scans []scan
	if err := DB.Table("quota_data").
		Select("model_name, channel_id, COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens").
		Where("created_at >= ? and created_at <= ?", from, to).
		Group("model_name, channel_id").
		Order("quota desc, model_name asc, channel_id asc").
		Limit(reconMatrixLimit).
		Scan(&scans).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(scans))
	for _, s := range scans {
		ids = append(ids, int(s.ChannelId))
	}
	names := reconChannelNames(ids)
	rows := make([]ReconMatrixRow, 0, len(scans))
	for _, s := range scans {
		rows = append(rows, ReconMatrixRow{Model: s.ModelName, ChannelId: int(s.ChannelId), Channel: names[int(s.ChannelId)], Quota: s.Quota, Requests: s.Requests, Tokens: s.Tokens})
	}
	return rows, nil
}

// reconSeries returns the consumption series at the requested granularity.
// quota_data.created_at is an HOUR bucket, so every granularity (day included)
// is re-aggregated in Go for cross-DB safety, in the caller's time zone `loc`
// so a "day" is the admin's calendar day, not the server's.
func reconSeries(from, to int64, granularity string, loc *time.Location) ([]ReconSeriesPoint, error) {
	var hourly []ReconSeriesPoint
	if err := DB.Table("quota_data").
		Select("created_at as period, COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens").
		Where("created_at >= ? and created_at <= ?", from, to).
		Group("created_at").
		Order("created_at asc").
		Scan(&hourly).Error; err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.UTC
	}
	bucketStart := func(unix int64) int64 {
		t := time.Unix(unix, 0).In(loc)
		switch granularity {
		case "week":
			// ISO-ish: back up to Monday.
			offset := (int(t.Weekday()) + 6) % 7
			d := t.AddDate(0, 0, -offset)
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc).Unix()
		case "month":
			return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).Unix()
		default:
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).Unix()
		}
	}
	byBucket := map[int64]*ReconSeriesPoint{}
	order := make([]int64, 0)
	for _, p := range hourly {
		b := bucketStart(p.Period)
		agg := byBucket[b]
		if agg == nil {
			agg = &ReconSeriesPoint{Period: b}
			byBucket[b] = agg
			order = append(order, b)
		}
		agg.Quota += p.Quota
		agg.Requests += p.Requests
		agg.Tokens += p.Tokens
	}
	out := make([]ReconSeriesPoint, 0, len(order))
	for _, b := range order {
		out = append(out, *byBucket[b])
	}
	return out, nil
}

// reconResellerDiscounts computes, per RESELLER over the window, the wholesale
// discount the platform gave (standard − what the reseller wallet actually
// paid) from the immutable org_usage_daily rollup — so it reflects the ratios
// in force at call time and never drifts when a ratio is later changed. The
// retail amount customers paid the reseller rides along for reference.
func reconResellerDiscounts(from, to int64) ([]ReconResellerRow, int64, int64, error) {
	dailyRows, err := fetchOrgUsageDaily("reseller_org_id > 0", nil, from, to)
	if err != nil {
		return nil, 0, 0, err
	}
	type agg struct{ std, charged, cost, req int64 }
	byReseller := map[int]*agg{}
	for _, r := range dailyRows {
		a := byReseller[r.ResellerOrgId]
		if a == nil {
			a = &agg{}
			byReseller[r.ResellerOrgId] = a
		}
		a.std += r.StandardQuota
		a.charged += r.ChargedQuota
		a.cost += r.CostQuota
		a.req += r.Requests
	}
	rows := make([]ReconResellerRow, 0, len(byReseller))
	var totalDiscount, totalStandard int64
	for resellerId, a := range byReseller {
		discount := a.std - a.cost
		totalDiscount += discount
		totalStandard += a.std
		if a.req == 0 && a.std == 0 {
			continue
		}
		name := "#" + strconv.Itoa(resellerId)
		if org, err := GetOrganizationById(resellerId); err == nil && org != nil && org.Name != "" {
			name = org.Name
		}
		rows = append(rows, ReconResellerRow{
			OrgId:         resellerId,
			Name:          name,
			StandardQuota: a.std,
			DiscountQuota: discount,
			ReceivedQuota: a.cost,
			ChargedQuota:  a.charged,
			Requests:      a.req,
		})
	}
	// Deterministic order: largest standard consumption first, then name.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StandardQuota != rows[j].StandardQuota {
			return rows[i].StandardQuota > rows[j].StandardQuota
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, totalDiscount, totalStandard, nil
}

// GetReconciliation builds the full admin reconciliation report. `loc` is the
// admin's time zone for the series buckets.
func GetReconciliation(from, to int64, granularity string, loc *time.Location) (*ReconReport, error) {
	report := &ReconReport{From: from, To: to}

	byModel, err := reconByColumn(from, to, "model_name")
	if err != nil {
		return nil, err
	}
	report.ByModel = byModel

	byGroup, err := reconByColumn(from, to, "use_group")
	if err != nil {
		return nil, err
	}
	report.ByGroup = byGroup

	byUser, err := reconByColumn(from, to, "username")
	if err != nil {
		return nil, err
	}
	report.ByUser = byUser

	byChannel, err := reconChannelRows(from, to)
	if err != nil {
		return nil, err
	}
	report.ByChannel = byChannel

	matrix, err := reconMatrixRows(from, to)
	if err != nil {
		return nil, err
	}
	report.ByModelChannel = matrix

	series, err := reconSeries(from, to, granularity, loc)
	if err != nil {
		return nil, err
	}
	report.Series = series

	resellers, totalDiscount, _, err := reconResellerDiscounts(from, to)
	if err != nil {
		return nil, err
	}
	report.ByReseller = resellers

	standard, requests, tokens, channels, err := reconTotals(from, to)
	if err != nil {
		return nil, err
	}
	report.Summary = ReconSummary{
		StandardQuota: standard,
		DiscountQuota: totalDiscount,
		ReceivedQuota: standard - totalDiscount,
		Requests:      requests,
		Tokens:        tokens,
		Channels:      channels,
	}
	return report, nil
}
