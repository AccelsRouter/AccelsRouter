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

// ReconCustomerRow is one reseller customer's discount reconciliation.
type ReconCustomerRow struct {
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
	ByCustomer []ReconCustomerRow `json:"by_customer"`
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

// reconCustomerDiscounts computes the reseller retail let-give per customer org
// over the window (standard vs discounted charge). Iterates the customer orgs
// only (bounded set), reusing per-org usage.
func reconCustomerDiscounts(from, to int64) ([]ReconCustomerRow, int64, int64, error) {
	ids, err := customerOrgIds()
	if err != nil {
		return nil, 0, 0, err
	}
	rows := make([]ReconCustomerRow, 0, len(ids))
	var totalDiscount, totalStandard int64
	for _, orgId := range ids {
		report, err := GetOrgUsage(orgId, from, to)
		if err != nil || report == nil {
			continue
		}
		std := report.TotalQuota
		charged := std
		org, err := GetOrganizationById(orgId)
		name := "#" + strconv.Itoa(orgId)
		if err == nil && org != nil {
			if org.Name != "" {
				name = org.Name
			}
			if org.RetailDiscounts != "" {
				report.ApplyRetailDiscounts(ParseRetailDiscounts(org.RetailDiscounts))
				charged = report.TotalRetailQuota
			}
		}
		discount := std - charged
		totalDiscount += discount
		totalStandard += std
		if report.TotalRequests == 0 && std == 0 {
			continue
		}
		rows = append(rows, ReconCustomerRow{
			OrgId:         orgId,
			Name:          name,
			StandardQuota: std,
			ChargedQuota:  charged,
			DiscountQuota: discount,
			Requests:      report.TotalRequests,
		})
	}
	return rows, totalDiscount, totalStandard, nil
}

// customerOrgIds returns the reseller-provisioned customer org ids.
func customerOrgIds() ([]int, error) {
	var ids []int
	if err := DB.Model(&ResellerCustomerLink{}).Pluck("customer_org_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
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

	customers, totalDiscount, _, err := reconCustomerDiscounts(from, to)
	if err != nil {
		return nil, err
	}
	report.ByCustomer = customers

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
