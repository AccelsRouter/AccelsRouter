package controller

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func reconGranularity(c *gin.Context) string {
	switch c.Query("granularity") {
	case "week":
		return "week"
	case "month":
		return "month"
	default:
		return "day"
	}
}

// reconLocation is the admin's time zone from `tz_offset` (minutes east of
// UTC, as the browser reports it), so day/week/month buckets and CSV dates
// follow the admin's calendar. Absent or out of range → UTC.
func reconLocation(c *gin.Context) *time.Location {
	off, err := strconv.Atoi(c.Query("tz_offset"))
	if err != nil || off < -14*60 || off > 14*60 {
		return time.UTC
	}
	return time.FixedZone("admin", off*60)
}

// AdminGetReconciliation — GET /api/admin/reconciliation
func AdminGetReconciliation(c *gin.Context) {
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetReconciliation(from, to, reconGranularity(c), reconLocation(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// AdminExportReconciliation — GET /api/admin/reconciliation/export (CSV)
func AdminExportReconciliation(c *gin.Context) {
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	loc := reconLocation(c)
	report, err := model.GetReconciliation(from, to, reconGranularity(c), loc)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filename := fmt.Sprintf("reconciliation-%d-%d.csv", from, to)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")

	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	usd := func(q int64) string {
		return strconv.FormatFloat(float64(q)/common.QuotaPerUnit, 'f', 6, 64)
	}

	s := report.Summary
	_ = w.Write([]string{"Summary", "USD"})
	_ = w.Write([]string{"Standard consumption", usd(s.StandardQuota)})
	_ = w.Write([]string{"Wholesale discount to resellers", usd(s.DiscountQuota)})
	_ = w.Write([]string{"Received (standard - discount)", usd(s.ReceivedQuota)})
	_ = w.Write([]string{"Requests", strconv.FormatInt(s.Requests, 10)})
	_ = w.Write([]string{"Tokens", strconv.FormatInt(s.Tokens, 10)})
	_ = w.Write([]string{"Channels", strconv.FormatInt(s.Channels, 10)})
	_ = w.Write(nil)

	writeDim := func(title string, rows []model.ReconRow) {
		_ = w.Write([]string{title, "Standard (USD)", "Requests", "Tokens", "Input tokens", "Output tokens", "Cache read tokens", "Usage unit", "Usage amount"})
		for _, r := range rows {
			_ = w.Write([]string{csvSafe(r.Key), usd(r.Quota), strconv.FormatInt(r.Requests, 10), strconv.FormatInt(r.Tokens, 10), strconv.FormatInt(r.PromptTokens, 10), strconv.FormatInt(r.CompletionTokens, 10), strconv.FormatInt(r.CacheReadTokens, 10), r.UsageUnit, strconv.FormatInt(r.UsageAmount, 10)})
		}
		_ = w.Write(nil)
	}
	writeDim("By model", report.ByModel)
	writeDim("By channel (upstream)", report.ByChannel)
	writeDim("By group", report.ByGroup)
	writeDim("By user", report.ByUser)

	_ = w.Write([]string{"By model x channel", "Channel", "Standard (USD)", "Requests", "Tokens", "Input tokens", "Output tokens", "Cache read tokens", "Usage unit", "Usage amount"})
	for _, r := range report.ByModelChannel {
		_ = w.Write([]string{csvSafe(r.Model), csvSafe(r.Channel), usd(r.Quota), strconv.FormatInt(r.Requests, 10), strconv.FormatInt(r.Tokens, 10), strconv.FormatInt(r.PromptTokens, 10), strconv.FormatInt(r.CompletionTokens, 10), strconv.FormatInt(r.CacheReadTokens, 10), r.UsageUnit, strconv.FormatInt(r.UsageAmount, 10)})
	}
	_ = w.Write(nil)

	_ = w.Write([]string{"Resellers", "Standard (USD)", "Wholesale discount (USD)", "Received (USD)", "Customers paid reseller (USD)", "Requests"})
	for _, r := range report.ByReseller {
		_ = w.Write([]string{csvSafe(r.Name), usd(r.StandardQuota), usd(r.DiscountQuota), usd(r.ReceivedQuota), usd(r.ChargedQuota), strconv.FormatInt(r.Requests, 10)})
	}
	_ = w.Write(nil)

	_ = w.Write([]string{"Time series", "Standard (USD)", "Requests", "Tokens"})
	for _, p := range report.Series {
		_ = w.Write([]string{time.Unix(p.Period, 0).In(loc).Format("2006-01-02"), usd(p.Quota), strconv.FormatInt(p.Requests, 10), strconv.FormatInt(p.Tokens, 10)})
	}
}

// upstreamBillMaxBytes bounds an uploaded bill; a month of per-call rows is a
// few megabytes.
const upstreamBillMaxBytes = 32 << 20

// AdminCompareUpstreamBill — POST /api/admin/reconciliation/upstream-bill
// (multipart). Fields: file (.xlsx or CSV), channel_id, and bill_tz_offset,
// the bill's zone in minutes east of UTC (empty: detect it against our
// hourly data). The query tz_offset is the admin's zone, used when nothing
// in the bill lines up.
func AdminCompareUpstreamBill(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, upstreamBillMaxBytes)
	channelId, err := strconv.Atoi(c.PostForm("channel_id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "Select the channel the bill belongs to")
		return
	}
	var pinned *int
	if s := c.PostForm("bill_tz_offset"); s != "" {
		off, err := strconv.Atoi(s)
		if err != nil || off < -14*60 || off > 14*60 {
			common.ApiErrorMsg(c, "Invalid bill time zone")
			return
		}
		pinned = &off
	}
	header, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "Upload the bill file")
		return
	}
	file, err := header.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()
	entries, err := model.ParseUpstreamBill(header.Filename, file)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_, adminOffset := time.Now().In(reconLocation(c)).Zone()
	report, err := model.CompareUpstreamBill(channelId, entries, pinned, adminOffset/60)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}
