// Upstream bill reconciliation: an upstream's exported bill (one billed call
// per row) compared with what quota_data recorded for the channel that routes
// to that upstream, per model, over the bill's own time range.
//
// A bill carries wall-clock timestamps without a zone, while quota_data is
// bucketed by UTC hour. The zone is therefore detected: every whole-hour
// offset is tried and the one whose hourly (model, hour) request counts line
// up best with ours wins; the admin can pin an offset instead.
package model

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/xuri/excelize/v2"
)

// UpstreamBillEntry is one billed call of an upstream bill.
type UpstreamBillEntry struct {
	// Wall is the bill's wall-clock time read as if it were UTC; the zone
	// offset is applied when the bill is compared.
	Wall int64
	// Span is how long after Wall the row's usage extends, in seconds: 0 for
	// a per-call row or a settlement window inside one hour, 86400 for a
	// daily bill row.
	Span             int64
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	Cost             float64 // in the bill's own currency
	// Payable is what the upstream actually charges for the entry after
	// coupons / credits, when the bill tells it apart from the cost.
	Payable float64
	// Requests is how many billed calls the entry stands for: 1 for an
	// exported bill's per-call row, 0 for an aggregated settlement row (an
	// API bill) where the count is unknown.
	Requests int64
	// UsageUnit / UsageAmount carry a non-token billing unit (images, seconds)
	// from an aggregated row; empty for token rows.
	UsageUnit   string
	UsageAmount int64
}

// upstreamBillColumns maps the header names a bill may use to the entry
// field they fill, lower-cased. The cost column is matched by prefix
// separately since bills label it with the currency ("折后价(CNY)"). The
// camel-case names are those of Baidu's model service usage export.
var upstreamBillColumns = map[string]string{
	"时间": "time", "time": "time", "created_at": "time", "日期": "time", "date": "time", "starttime": "time",
	"模型名": "model", "模型": "model", "model": "model", "model_name": "model", "modelname": "model",
	"输入token": "prompt", "输入tokens": "prompt", "prompt_tokens": "prompt", "input_tokens": "prompt", "totalinputtokens": "prompt",
	"输出token": "completion", "输出tokens": "completion", "completion_tokens": "completion", "output_tokens": "completion", "totaloutputtokens": "completion",
	"缓存读取": "cache", "cache_read_tokens": "cache", "cached_tokens": "cache",
	// Baidu counts cached input per kind of content; together they are the
	// cached input.
	"inputtextcachedtokens": "cache", "inputaudiocachedtokens": "cache", "inputimagecachedtokens": "cache",
	"inputvideocachedtokens": "cache", "inputdocumentcachedtokens": "cache",
	// What a call that is not billed by token produced.
	"durationseconds": "seconds", "outputcount": "outputs",
}

// upstreamBillCostPrefixes, in order of preference: what the upstream charged
// after discount, else the undiscounted price.
var upstreamBillCostPrefixes = []string{"折后价", "discountedamount", "费用", "cost", "amount", "原价", "originalamount", "price"}

var upstreamBillTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006/01/02 15:04:05", "2006/1/2 15:04:05",
	"2006-01-02 15:04", "2006/1/2 15:04", "01-02-06 15:04", "1/2/06 15:04", "2006-01-02",
	"2006-01-02 15:04:05.000", "2006-01-02T15:04:05.000",
}

// upstreamBillMaxUnpacked bounds what an archived bill may unpack to.
const upstreamBillMaxUnpacked = 2 << 30

// ErrUpstreamBillAggregated is returned for an export that sums usage per
// time bucket and so names no model.
var ErrUpstreamBillAggregated = common.NewMessage("This export is aggregated by time and names no model; export the per-task detail instead")

// ParseUpstreamBill reads an exported bill into entries: an .xlsx workbook, a
// CSV file, or a .zip of CSV files (Baidu's model service packs one file per
// kind of billing). The first sheet whose header row names a time and a model
// column is the bill; token and cost columns are optional. Totals rows (no
// time or no model) are skipped.
func ParseUpstreamBill(filename string, r io.Reader) ([]UpstreamBillEntry, error) {
	name := strings.ToLower(filename)
	if strings.HasSuffix(name, ".xlsx") {
		f, err := excelize.OpenReader(r)
		if err != nil {
			return nil, fmt.Errorf("read xlsx: %w", err)
		}
		defer f.Close()
		var lastErr error
		for _, sheet := range f.GetSheetList() {
			rows, err := f.GetRows(sheet)
			if err != nil {
				return nil, fmt.Errorf("read sheet %q: %w", sheet, err)
			}
			at := 0
			entries, err := parseUpstreamBillRows(func() ([]string, error) {
				if at >= len(rows) {
					return nil, io.EOF
				}
				at++
				return rows[at-1], nil
			})
			if err == nil {
				return entries, nil
			}
			lastErr = fmt.Errorf("sheet %q: %w", sheet, err)
		}
		if lastErr == nil {
			lastErr = errors.New("the workbook has no sheets")
		}
		return nil, lastErr
	}
	if !strings.HasSuffix(name, ".zip") {
		return parseUpstreamBillCSV(r)
	}
	packed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(packed), int64(len(packed)))
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	var entries []UpstreamBillEntry
	var unpacked uint64
	for _, file := range archive.File {
		base := file.Name[strings.LastIndex(file.Name, "/")+1:]
		if file.FileInfo().IsDir() || strings.HasPrefix(base, ".") || !strings.HasSuffix(strings.ToLower(base), ".csv") {
			continue
		}
		if unpacked += file.UncompressedSize64; unpacked > upstreamBillMaxUnpacked {
			return nil, errors.New("the archive unpacks to more than 2 GB; export a shorter period")
		}
		content, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, err)
		}
		// The declared size is the archive's own claim; the read is bounded too.
		rows, err := parseUpstreamBillCSV(io.LimitReader(content, upstreamBillMaxUnpacked))
		content.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, err)
		}
		entries = append(entries, rows...)
	}
	if len(entries) == 0 {
		return nil, errors.New("the archive holds no CSV bill")
	}
	return entries, nil
}

// parseUpstreamBillCSV reads one CSV bill row by row, so a file of a million
// calls is never held whole.
func parseUpstreamBillCSV(r io.Reader) ([]UpstreamBillEntry, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = true
	first := true
	return parseUpstreamBillRows(func() ([]string, error) {
		row, err := cr.Read()
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("read csv: %w", err)
		}
		if first && len(row) > 0 {
			row[0] = strings.TrimPrefix(row[0], "\ufeff")
		}
		first = false
		return row, nil
	})
}

// parseUpstreamBillRows turns the rows next yields, until io.EOF, into
// entries. A row is only read while next's following call has not been made.
func parseUpstreamBillRows(next func() ([]string, error)) ([]UpstreamBillEntry, error) {
	// The header is the first row naming both a time and a model column.
	// Several columns may fill one field (Baidu's cached input per kind).
	var cols map[string][]int
	line := 0
	aggregated := false
	for cols == nil {
		row, err := next()
		if err == io.EOF {
			if aggregated {
				return nil, ErrUpstreamBillAggregated
			}
			return nil, errors.New("unrecognized bill format: no header row with a time and a model column")
		}
		if err != nil {
			return nil, err
		}
		line++
		found := map[string][]int{}
		costRank := len(upstreamBillCostPrefixes)
		for j, cell := range row {
			name := strings.ToLower(strings.TrimSpace(cell))
			aggregated = aggregated || name == "time_bucket"
			if field, ok := upstreamBillColumns[name]; ok {
				if field == "cache" || len(found[field]) == 0 {
					found[field] = append(found[field], j)
				}
				continue
			}
			// An earlier column only loses to a preferred prefix.
			if rank := upstreamBillCostRank(name); rank < costRank {
				found["cost"], costRank = []int{j}, rank
			}
		}
		if len(found["time"]) > 0 && len(found["model"]) > 0 {
			cols = found
		}
	}
	cell := func(row []string, at int) string {
		if at >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[at])
	}
	number := func(row []string, field string) (float64, error) {
		var sum float64
		for _, at := range cols[field] {
			s := strings.ReplaceAll(cell(row, at), ",", "")
			if s == "" || s == "-" {
				continue
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, err
			}
			sum += v
		}
		return sum, nil
	}
	var entries []UpstreamBillEntry
	for {
		row, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		line++
		when := cell(row, cols["time"][0])
		if when == "" {
			continue
		}
		var wall time.Time
		for _, layout := range upstreamBillTimeLayouts {
			if wall, err = time.ParseInLocation(layout, when, time.UTC); err == nil {
				break
			}
		}
		model := cell(row, cols["model"][0])
		if err != nil {
			// A totals row ("合计") names no model.
			if model == "" {
				continue
			}
			return nil, fmt.Errorf("row %d: unrecognized time %q", line, when)
		}
		entry := UpstreamBillEntry{Wall: wall.Unix(), Model: model, Requests: 1}
		fields := []struct {
			name string
			dst  *int64
		}{{"prompt", &entry.PromptTokens}, {"completion", &entry.CompletionTokens}, {"cache", &entry.CacheReadTokens}}
		for _, f := range fields {
			v, err := number(row, f.name)
			if err != nil {
				return nil, fmt.Errorf("row %d: %s: %w", line, f.name, err)
			}
			*f.dst = int64(v)
		}
		if entry.Cost, err = number(row, "cost"); err != nil {
			return nil, fmt.Errorf("row %d: cost: %w", line, err)
		}
		// A call billed by what it produced: seconds of video, else a count of
		// outputs. A malformed value leaves the usage out, not the call.
		if seconds, _ := number(row, "seconds"); seconds > 0 {
			entry.UsageUnit, entry.UsageAmount = UsageUnitSecond, int64(math.Round(seconds))
		} else if outputs, _ := number(row, "outputs"); outputs > 0 {
			entry.UsageUnit, entry.UsageAmount = UsageUnitImage, int64(math.Round(outputs))
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("the bill has no rows")
	}
	return entries, nil
}

func upstreamBillCostRank(name string) int {
	for i, prefix := range upstreamBillCostPrefixes {
		if strings.HasPrefix(name, prefix) {
			return i
		}
	}
	return len(upstreamBillCostPrefixes)
}

// UpstreamBillSide is one side of a comparison: ours (quota_data: standard
// price, usage unit) or the bill's (cost in its currency).
type UpstreamBillSide struct {
	Requests         int64   `json:"requests"`
	Tokens           int64   `json:"tokens"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	Quota            int64   `json:"quota"`
	UsageUnit        string  `json:"usage_unit"`
	UsageAmount      int64   `json:"usage_amount"`
	Cost             float64 `json:"cost"`
	Payable          float64 `json:"payable"`
	// Rows is how many bill rows named the model (bill side only): a row
	// with nothing on it still says the upstream saw the model.
	Rows int64 `json:"rows"`
}

// UpstreamBillExpected is what the channel's recorded prices make of our
// usage, in USD: the cost the upstream should have billed. Lowest and Highest
// differ where a price depends on something the hourly usage does not keep
// (the input length of each call, a video's resolution).
type UpstreamBillExpected struct {
	Lowest  float64 `json:"lowest"`
	Highest float64 `json:"highest"`
	// LaterPrice is set when usage older than the first recorded price was
	// priced at that first record.
	LaterPrice bool `json:"later_price"`
	// UnpricedRequests counts our requests in hours no recorded price covers.
	UnpricedRequests int64 `json:"unpriced_requests"`
	// Prices are the recorded prices that were applied, oldest first.
	Prices []CustomChannelPrice `json:"prices,omitempty"`
}

// UpstreamBillRow is one model as the bill names it, with our rows mapped
// through the channel's model mapping to that name. Expected is nil when no
// recorded price of the channel covers any of our usage of the model.
type UpstreamBillRow struct {
	Model    string                `json:"model"`
	Ours     UpstreamBillSide      `json:"ours"`
	Bill     UpstreamBillSide      `json:"bill"`
	Expected *UpstreamBillExpected `json:"expected,omitempty"`
}

type UpstreamBillReport struct {
	ChannelId int    `json:"channel_id"`
	Channel   string `json:"channel"`
	// AlsoChannels names the other channels of the same upstream account whose
	// usage the bill was compared with too.
	AlsoChannels []string `json:"also_channels,omitempty"`
	// From / To is the bill's range as UTC hour buckets once the offset is applied.
	From           int64 `json:"from"`
	To             int64 `json:"to"`
	OffsetMinutes  int   `json:"offset_minutes"`
	OffsetDetected bool  `json:"offset_detected"`
	// MatchedRequests is how many bill requests fell in an hour where we
	// recorded a request for the same model: the evidence behind the offset.
	MatchedRequests int64 `json:"matched_requests"`
	// BillRequestsKnown is false for an aggregated (API) bill, whose rows are
	// settlement windows rather than calls: the bill's request counts are
	// then unknown and MatchedRequests counts windows.
	BillRequestsKnown bool `json:"bill_requests_known"`
	// BillRequestsFromUsage is true when the bill side's request counts were
	// filled from the upstream's usage statistics instead (SetBillRequests);
	// UsageError says why they could not be.
	BillRequestsFromUsage bool   `json:"bill_requests_from_usage"`
	UsageError            string `json:"usage_error"`
	// BillPayableKnown is true when the bill tells the payable amount (after
	// coupons / credits) apart from the billed cost.
	BillPayableKnown bool `json:"bill_payable_known"`
	// BillScope says what an API bill was filtered to: "channel_key" (the
	// usage attributed to the channel's own API keys, listed in
	// BillSplitItems) or "account" (everything the account was billed for).
	// Empty for an uploaded bill.
	BillScope      string   `json:"bill_scope"`
	BillSplitItems []string `json:"bill_split_items"`
	// BillSource names the API the bill was pulled from ("byteplus",
	// "alibaba"); empty for an uploaded bill.
	BillSource string            `json:"bill_source"`
	Rows       []UpstreamBillRow `json:"rows"`
	Ours       UpstreamBillSide  `json:"ours"`
	Bill       UpstreamBillSide  `json:"bill"`
	// Expected totals the rows' expected cost; nil when no row has one.
	Expected *UpstreamBillExpected `json:"expected,omitempty"`
}

const (
	upstreamBillMinOffsetHours = -12
	upstreamBillMaxOffsetHours = 14
)

type upstreamBillHourScan struct {
	CreatedAt        int64
	ModelName        string
	UsageUnit        string
	UsageAmount      int64
	Quota            int64
	Requests         int64
	Tokens           int64
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
}

type upstreamBillHourKey struct {
	hour  int64
	model string
}

// UpstreamAccountChannels lists the other platform channels that reach the
// same upstream account as the given one: the same host with the same key.
// One account's bill covers them all, so it is compared with them together
// (an upstream serving several protocols takes a channel for each).
func UpstreamAccountChannels(channelId int) ([]int, error) {
	channels, err := GetPlatformChannels()
	if err != nil {
		return nil, err
	}
	host := func(c *Channel) string {
		parsed, err := url.Parse(c.GetBaseURL())
		if err != nil {
			return ""
		}
		return strings.ToLower(parsed.Hostname())
	}
	var self *Channel
	for _, c := range channels {
		if c.Id == channelId {
			self = c
		}
	}
	if self == nil || self.Key == "" || host(self) == "" {
		return nil, nil
	}
	var same []int
	for _, c := range channels {
		if c.Id != channelId && c.Key == self.Key && host(c) == host(self) {
			same = append(same, c.Id)
		}
	}
	return same, nil
}

// CompareUpstreamBill compares the bill with the channel's quota_data, and
// with that of the channels in also, which share its upstream account. A nil
// offset asks for detection; fallbackOffset (minutes east of UTC) is used when
// nothing lines up and breaks ties between equally good offsets. modelKey,
// when set, normalizes model names on both sides before they are matched (an
// API bill names models its own way, e.g. "DeepSeek-V4.1-flash").
func CompareUpstreamBill(channelId int, entries []UpstreamBillEntry, offsetMinutes *int, fallbackOffset int, modelKey func(string) string, also ...int) (*UpstreamBillReport, error) {
	if len(entries) == 0 {
		return nil, errors.New("the bill has no rows")
	}
	channel, err := GetChannelById(channelId, false)
	if err != nil {
		return nil, fmt.Errorf("channel %d: %w", channelId, err)
	}

	// maxWall is the last second a row covers: a spanned row (a daily bill)
	// reaches to the end of its span.
	minWall, maxWall := entries[0].Wall, entries[0].Wall
	for _, e := range entries {
		minWall, maxWall = min(minWall, e.Wall), max(maxWall, e.Wall+max(e.Span-1, 0))
	}
	if modelKey == nil {
		modelKey = func(s string) string { return s }
	}
	for i := range entries {
		entries[i].Model = modelKey(entries[i].Model)
	}
	report := &UpstreamBillReport{ChannelId: channelId, Channel: channel.Name, OffsetMinutes: fallbackOffset}
	// Each channel's hours, scanned wide enough for any offset and narrowed
	// once the offset is known, with the names our records and the upstream
	// know each scanned model by before the bill's naming folds them, and the
	// channel's own recorded prices.
	var scans []upstreamBillHourScan
	var served, upstream []string
	var prices []map[string][]CustomChannelPrice
	ourHourly := map[upstreamBillHourKey]int64{}
	for _, id := range append([]int{channelId}, also...) {
		compared := channel
		if id != channelId {
			if compared, err = GetChannelById(id, false); err != nil {
				return nil, fmt.Errorf("channel %d: %w", id, err)
			}
			report.AlsoChannels = append(report.AlsoChannels, compared.Name)
		}
		mapping := map[string]string{}
		if m := compared.GetModelMapping(); m != "" && m != "{}" {
			if err := common.UnmarshalJsonStr(m, &mapping); err != nil {
				return nil, fmt.Errorf("channel %d model mapping: %w", id, err)
			}
		}
		history, err := customChannelPriceHistory(id)
		if err != nil {
			return nil, err
		}
		var hours []upstreamBillHourScan
		if err := DB.Table("quota_data").
			Select("created_at, model_name, usage_unit, "+reconAggregates).
			Where("channel_id = ? and created_at >= ? and created_at <= ?", id,
				minWall-upstreamBillMaxOffsetHours*3600-3600, maxWall-upstreamBillMinOffsetHours*3600).
			Group("created_at, model_name, usage_unit").
			Scan(&hours).Error; err != nil {
			return nil, err
		}
		for _, hour := range hours {
			served = append(served, hour.ModelName)
			if mapped := mapping[hour.ModelName]; mapped != "" {
				hour.ModelName = mapped
			}
			upstream = append(upstream, hour.ModelName)
			hour.ModelName = modelKey(hour.ModelName)
			ourHourly[upstreamBillHourKey{hour.CreatedAt, hour.ModelName}] += hour.Requests
			scans = append(scans, hour)
			prices = append(prices, history)
		}
	}
	// Offset detection scores hours where the bill and we both saw the model;
	// an aggregated row counts as presence (1), a per-call row as one call. A
	// row spanning several hours (a daily bill) counts once when we saw the
	// model in any hour it covers.
	matched := func(offset int) int64 {
		var score int64
		billHourly := map[upstreamBillHourKey]int64{}
		for _, e := range entries {
			at := e.Wall - int64(offset)*60
			if e.Span <= 3600 {
				billHourly[upstreamBillHourKey{at - at%3600, e.Model}] += max(e.Requests, 1)
				continue
			}
			for h := at - at%3600; h < at+e.Span; h += 3600 {
				if ourHourly[upstreamBillHourKey{h, e.Model}] > 0 {
					score++
					break
				}
			}
		}
		for key, n := range billHourly {
			score += min(n, ourHourly[key])
		}
		return score
	}

	for _, e := range entries {
		if e.Requests > 0 {
			report.BillRequestsKnown = true
			break
		}
	}
	if offsetMinutes != nil {
		report.OffsetMinutes = *offsetMinutes
		report.MatchedRequests = matched(*offsetMinutes)
	} else {
		for h := upstreamBillMinOffsetHours; h <= upstreamBillMaxOffsetHours; h++ {
			offset := h * 60
			score := matched(offset)
			better := score > report.MatchedRequests
			tie := score == report.MatchedRequests && score > 0 && abs(offset-fallbackOffset) < abs(report.OffsetMinutes-fallbackOffset)
			if better || tie {
				report.OffsetMinutes, report.MatchedRequests, report.OffsetDetected = offset, score, true
			}
		}
	}

	shift := int64(report.OffsetMinutes) * 60
	report.From = (minWall - shift) - (minWall-shift)%3600
	report.To = (maxWall - shift) - (maxWall-shift)%3600
	expected := map[string]*UpstreamBillExpected{}
	applied := map[string]map[int]CustomChannelPrice{}
	var ours []reconUnitScan
	for i, s := range scans {
		if s.CreatedAt < report.From || s.CreatedAt > report.To {
			continue
		}
		if expected[s.ModelName] == nil {
			expected[s.ModelName], applied[s.ModelName] = &UpstreamBillExpected{}, map[int]CustomChannelPrice{}
		}
		price, later, found := customChannelPriceAt(prices[i][upstream[i]], s.CreatedAt)
		var pricing map[string]any
		priced := found && common.UnmarshalJsonStr(price.Pricing, &pricing) == nil
		var lowest, highest float64
		if priced {
			lowest, highest, priced = customChannelCost(pricing, customChannelUsage{
				Requests: s.Requests, PromptTokens: s.PromptTokens, CompletionTokens: s.CompletionTokens,
				CacheReadTokens: s.CacheReadTokens, UsageUnit: s.UsageUnit, UsageAmount: s.UsageAmount,
			}, served[i])
		}
		if priced {
			expected[s.ModelName].Lowest += lowest
			expected[s.ModelName].Highest += highest
			expected[s.ModelName].LaterPrice = expected[s.ModelName].LaterPrice || later
			applied[s.ModelName][price.ID] = price
		} else {
			expected[s.ModelName].UnpricedRequests += s.Requests
		}
		ours = append(ours, reconUnitScan{Dim: s.ModelName, UsageUnit: s.UsageUnit, UsageAmount: s.UsageAmount, Quota: s.Quota, Requests: s.Requests, Tokens: s.Tokens, PromptTokens: s.PromptTokens, CompletionTokens: s.CompletionTokens, CacheReadTokens: s.CacheReadTokens})
	}
	rows := map[string]*UpstreamBillRow{}
	row := func(model string) *UpstreamBillRow {
		r := rows[model]
		if r == nil {
			r = &UpstreamBillRow{Model: model}
			rows[model] = r
		}
		return r
	}
	for _, o := range reconMergeUnits(ours) {
		row(o.Key).Ours = UpstreamBillSide{Requests: o.Requests, Tokens: o.Tokens, PromptTokens: o.PromptTokens, CompletionTokens: o.CompletionTokens, CacheReadTokens: o.CacheReadTokens, Quota: o.Quota, UsageUnit: o.UsageUnit, UsageAmount: o.UsageAmount}
	}
	for _, e := range entries {
		b := &row(e.Model).Bill
		b.Rows++
		b.Requests += e.Requests
		b.PromptTokens += e.PromptTokens
		b.CompletionTokens += e.CompletionTokens
		b.CacheReadTokens += e.CacheReadTokens
		b.Tokens += e.PromptTokens + e.CompletionTokens
		b.Cost += e.Cost
		b.Payable += e.Payable
		if e.UsageUnit != "" {
			b.UsageUnit = e.UsageUnit
			b.UsageAmount += e.UsageAmount
		}
	}
	report.Rows = make([]UpstreamBillRow, 0, len(rows))
	for _, r := range rows {
		if cost := expected[r.Model]; cost != nil && len(applied[r.Model]) > 0 {
			for _, price := range applied[r.Model] {
				cost.Prices = append(cost.Prices, price)
			}
			sort.Slice(cost.Prices, func(i, j int) bool {
				a, b := cost.Prices[i], cost.Prices[j]
				return a.EffectiveAt < b.EffectiveAt || (a.EffectiveAt == b.EffectiveAt && a.ID < b.ID)
			})
			r.Expected = cost
			if report.Expected == nil {
				report.Expected = &UpstreamBillExpected{}
			}
			report.Expected.Lowest += cost.Lowest
			report.Expected.Highest += cost.Highest
			report.Expected.LaterPrice = report.Expected.LaterPrice || cost.LaterPrice
			report.Expected.UnpricedRequests += cost.UnpricedRequests
		}
		report.Rows = append(report.Rows, *r)
		report.Ours.Requests += r.Ours.Requests
		report.Ours.Tokens += r.Ours.Tokens
		report.Ours.PromptTokens += r.Ours.PromptTokens
		report.Ours.CompletionTokens += r.Ours.CompletionTokens
		report.Ours.CacheReadTokens += r.Ours.CacheReadTokens
		report.Ours.Quota += r.Ours.Quota
		report.Bill.Requests += r.Bill.Requests
		report.Bill.Tokens += r.Bill.Tokens
		report.Bill.PromptTokens += r.Bill.PromptTokens
		report.Bill.CompletionTokens += r.Bill.CompletionTokens
		report.Bill.CacheReadTokens += r.Bill.CacheReadTokens
		report.Bill.Cost += r.Bill.Cost
		report.Bill.Payable += r.Bill.Payable
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		a, b := report.Rows[i], report.Rows[j]
		if a.Bill.Cost != b.Bill.Cost {
			return a.Bill.Cost > b.Bill.Cost
		}
		if a.Ours.Quota != b.Ours.Quota {
			return a.Ours.Quota > b.Ours.Quota
		}
		return a.Model < b.Model
	})
	return report, nil
}

// SetBillRequests fills the bill side's request counts from the upstream's
// usage statistics, which count calls where an aggregated bill does not:
// requests per model as the usage names it, matched to the rows through
// modelKey. A model only the usage saw gets a row of its own, after the
// sorted rows in name order.
func (r *UpstreamBillReport) SetBillRequests(requests map[string]int64, modelKey func(string) string) {
	byModel := map[string]int64{}
	for name, n := range requests {
		byModel[modelKey(name)] += n
	}
	r.Bill.Requests = 0
	for i := range r.Rows {
		n := byModel[r.Rows[i].Model]
		r.Rows[i].Bill.Requests = n
		r.Bill.Requests += n
		delete(byModel, r.Rows[i].Model)
	}
	extra := make([]string, 0, len(byModel))
	for model, n := range byModel {
		if n > 0 {
			extra = append(extra, model)
		}
	}
	slices.Sort(extra)
	for _, model := range extra {
		r.Rows = append(r.Rows, UpstreamBillRow{Model: model, Bill: UpstreamBillSide{Requests: byModel[model]}})
		r.Bill.Requests += byModel[model]
	}
	r.BillRequestsFromUsage = true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
