// Admin financial reconciliation: multi-dimensional consumption aggregation for
// a time window, plus the reseller retail discount given (let-give) per customer
// org. Built on the pre-aggregated quota_data rollup (day-bucketed, keyed by
// model/channel/group/user) so it stays fast without scanning raw logs; the
// discount side reuses per-org usage (GetOrgUsage) over the customer orgs only.
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

// ReconResellerRow is one reseller's discount reconciliation, aggregated over
// all of that reseller's customers (the admin view groups by reseller, not by
// individual customer).
type ReconResellerRow struct {
	OrgId         int    `json:"org_id"`
	Name          string `json:"name"`
	StandardQuota int64  `json:"standard_quota"`
	ChargedQuota  int64  `json:"charged_quota"`
	DiscountQuota int64  `json:"discount_quota"`
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
	DiscountQuota int64 `json:"discount_quota"` // reseller retail let-give
	ChargedQuota  int64 `json:"charged_quota"`  // standard - discount (actually charged)
	Requests      int64 `json:"requests"`
	Tokens        int64 `json:"tokens"`
	Channels      int64 `json:"channels"`
}

// ReconReport is the full reconciliation payload.
type ReconReport struct {
	From       int64              `json:"from"`
	To         int64              `json:"to"`
	Summary    ReconSummary       `json:"summary"`
	ByModel    []ReconRow         `json:"by_model"`
	ByChannel  []ReconRow         `json:"by_channel"`
	ByGroup    []ReconRow         `json:"by_group"`
	ByUser     []ReconRow         `json:"by_user"`
	ByReseller []ReconResellerRow `json:"by_reseller"`
	Series     []ReconSeriesPoint `json:"series"`
}

const reconDimLimit = 200

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
		Select("COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens, COUNT(DISTINCT channel_id) as channels").
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
		if s.ChannelId > 0 {
			ids = append(ids, int(s.ChannelId))
		}
	}
	names := map[int]string{}
	if len(ids) > 0 {
		type nameRow struct {
			Id   int
			Name string
		}
		var nrs []nameRow
		if err := DB.Table("channels").Select("id, name").Where("id IN ?", ids).Scan(&nrs).Error; err == nil {
			for _, n := range nrs {
				names[n.Id] = n.Name
			}
		}
	}
	rows := make([]ReconRow, 0, len(scans))
	for _, s := range scans {
		name := names[int(s.ChannelId)]
		if name == "" {
			name = "#" + strconv.Itoa(int(s.ChannelId))
		}
		rows = append(rows, ReconRow{Key: name, Quota: s.Quota, Requests: s.Requests, Tokens: s.Tokens})
	}
	return rows, nil
}

// reconSeries returns daily buckets (quota_data.created_at is already a day
// bucket), re-aggregated to the requested granularity in Go for cross-DB safety.
func reconSeries(from, to int64, granularity string) ([]ReconSeriesPoint, error) {
	var daily []ReconSeriesPoint
	if err := DB.Table("quota_data").
		Select("created_at as period, COALESCE(SUM(quota),0) as quota, COALESCE(SUM(count),0) as requests, COALESCE(SUM(token_used),0) as tokens").
		Where("created_at >= ? and created_at <= ?", from, to).
		Group("created_at").
		Order("created_at asc").
		Scan(&daily).Error; err != nil {
		return nil, err
	}
	if granularity == "day" || granularity == "" {
		return daily, nil
	}
	bucketStart := func(unix int64) int64 {
		t := time.Unix(unix, 0).UTC()
		switch granularity {
		case "week":
			// ISO-ish: back up to Monday.
			offset := (int(t.Weekday()) + 6) % 7
			d := t.AddDate(0, 0, -offset)
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).Unix()
		case "month":
			return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
		default:
			return unix
		}
	}
	byBucket := map[int64]*ReconSeriesPoint{}
	order := make([]int64, 0)
	for _, p := range daily {
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

// reconResellerDiscounts computes the reseller retail let-give aggregated per
// RESELLER over the window (standard vs actually-charged), from the immutable
// org_usage_daily rollup — so it reflects what customers actually paid at call
// time and never drifts when a ratio is later changed.
func reconResellerDiscounts(from, to int64) ([]ReconResellerRow, int64, int64, error) {
	dailyRows, err := fetchOrgUsageDaily("reseller_org_id > 0", nil, from, to)
	if err != nil {
		return nil, 0, 0, err
	}
	type agg struct{ std, charged, req int64 }
	byReseller := map[int]*agg{}
	for _, r := range dailyRows {
		a := byReseller[r.ResellerOrgId]
		if a == nil {
			a = &agg{}
			byReseller[r.ResellerOrgId] = a
		}
		a.std += r.StandardQuota
		a.charged += r.ChargedQuota
		a.req += r.Requests
	}
	rows := make([]ReconResellerRow, 0, len(byReseller))
	var totalDiscount, totalStandard int64
	for resellerId, a := range byReseller {
		discount := a.std - a.charged
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
			ChargedQuota:  a.charged,
			DiscountQuota: discount,
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

// GetReconciliation builds the full admin reconciliation report.
func GetReconciliation(from, to int64, granularity string) (*ReconReport, error) {
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

	series, err := reconSeries(from, to, granularity)
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
		ChargedQuota:  standard - totalDiscount,
		Requests:      requests,
		Tokens:        tokens,
		Channels:      channels,
	}
	return report, nil
}
