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
	"encoding/csv"
	"errors"
	"fmt"
	"io"
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
	Wall             int64
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	Cost             float64 // in the bill's own currency
}

// upstreamBillColumns maps the header names a bill may use to the entry
// field they fill. The cost column is matched by prefix separately since
// bills label it with the currency ("折后价(CNY)").
var upstreamBillColumns = map[string]string{
	"时间": "time", "time": "time", "created_at": "time", "日期": "time", "date": "time",
	"模型名": "model", "模型": "model", "model": "model", "model_name": "model",
	"输入token": "prompt", "输入tokens": "prompt", "prompt_tokens": "prompt", "input_tokens": "prompt",
	"输出token": "completion", "输出tokens": "completion", "completion_tokens": "completion", "output_tokens": "completion",
	"缓存读取": "cache", "cache_read_tokens": "cache", "cached_tokens": "cache",
}

// upstreamBillCostPrefixes, in order of preference: what the upstream charged
// after discount, else the undiscounted price.
var upstreamBillCostPrefixes = []string{"折后价", "费用", "cost", "amount", "原价", "price"}

var upstreamBillTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006/01/02 15:04:05", "2006/1/2 15:04:05",
	"2006-01-02 15:04", "2006/1/2 15:04", "01-02-06 15:04", "1/2/06 15:04", "2006-01-02",
}

// ParseUpstreamBill reads an exported bill (.xlsx or CSV) into entries. The
// first sheet whose header row names a time and a model column is the bill;
// token and cost columns are optional. Totals rows (no time or no model) are
// skipped.
func ParseUpstreamBill(filename string, r io.Reader) ([]UpstreamBillEntry, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".xlsx") {
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
			entries, err := parseUpstreamBillRows(rows)
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
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(rows) > 0 && len(rows[0]) > 0 {
		rows[0][0] = strings.TrimPrefix(rows[0][0], "\ufeff")
	}
	return parseUpstreamBillRows(rows)
}

func parseUpstreamBillRows(rows [][]string) ([]UpstreamBillEntry, error) {
	// The header is the first row naming both a time and a model column.
	cols := map[string]int{}
	headerAt := -1
	for i, row := range rows {
		cols = map[string]int{}
		for j, cell := range row {
			name := strings.ToLower(strings.TrimSpace(cell))
			if field, ok := upstreamBillColumns[name]; ok {
				if _, taken := cols[field]; !taken {
					cols[field] = j
				}
				continue
			}
			for _, prefix := range upstreamBillCostPrefixes {
				if !strings.HasPrefix(name, prefix) {
					continue
				}
				// An earlier column only loses to a preferred prefix.
				if at, taken := cols["cost"]; taken && upstreamBillCostRank(strings.ToLower(strings.TrimSpace(row[at]))) <= upstreamBillCostRank(name) {
					break
				}
				cols["cost"] = j
				break
			}
		}
		if _, ok := cols["time"]; ok {
			if _, ok := cols["model"]; ok {
				headerAt = i
				break
			}
		}
	}
	if headerAt < 0 {
		return nil, errors.New("unrecognized bill format: no header row with a time and a model column")
	}
	cell := func(row []string, field string) string {
		at, ok := cols[field]
		if !ok || at >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[at])
	}
	number := func(row []string, field string) (float64, error) {
		s := strings.ReplaceAll(cell(row, field), ",", "")
		if s == "" || s == "-" {
			return 0, nil
		}
		return strconv.ParseFloat(s, 64)
	}
	entries := make([]UpstreamBillEntry, 0, len(rows)-headerAt-1)
	for i, row := range rows[headerAt+1:] {
		when := cell(row, "time")
		if when == "" {
			continue
		}
		var wall time.Time
		var err error
		for _, layout := range upstreamBillTimeLayouts {
			if wall, err = time.ParseInLocation(layout, when, time.UTC); err == nil {
				break
			}
		}
		model := cell(row, "model")
		if err != nil {
			// A totals row ("合计") names no model.
			if model == "" {
				continue
			}
			return nil, fmt.Errorf("row %d: unrecognized time %q", headerAt+i+2, when)
		}
		entry := UpstreamBillEntry{Wall: wall.Unix(), Model: model}
		fields := []struct {
			name string
			dst  *int64
		}{{"prompt", &entry.PromptTokens}, {"completion", &entry.CompletionTokens}, {"cache", &entry.CacheReadTokens}}
		for _, f := range fields {
			v, err := number(row, f.name)
			if err != nil {
				return nil, fmt.Errorf("row %d: %s: %w", headerAt+i+2, f.name, err)
			}
			*f.dst = int64(v)
		}
		if entry.Cost, err = number(row, "cost"); err != nil {
			return nil, fmt.Errorf("row %d: cost: %w", headerAt+i+2, err)
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
}

// UpstreamBillRow is one model as the bill names it, with our rows mapped
// through the channel's model mapping to that name.
type UpstreamBillRow struct {
	Model string           `json:"model"`
	Ours  UpstreamBillSide `json:"ours"`
	Bill  UpstreamBillSide `json:"bill"`
}

type UpstreamBillReport struct {
	ChannelId int    `json:"channel_id"`
	Channel   string `json:"channel"`
	// From / To is the bill's range as UTC hour buckets once the offset is applied.
	From           int64 `json:"from"`
	To             int64 `json:"to"`
	OffsetMinutes  int   `json:"offset_minutes"`
	OffsetDetected bool  `json:"offset_detected"`
	// MatchedRequests is how many bill requests fell in an hour where we
	// recorded a request for the same model: the evidence behind the offset.
	MatchedRequests int64             `json:"matched_requests"`
	Rows            []UpstreamBillRow `json:"rows"`
	Ours            UpstreamBillSide  `json:"ours"`
	Bill            UpstreamBillSide  `json:"bill"`
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

// CompareUpstreamBill compares the bill with the channel's quota_data. A nil
// offset asks for detection; fallbackOffset (minutes east of UTC) is used when
// nothing lines up and breaks ties between equally good offsets.
func CompareUpstreamBill(channelId int, entries []UpstreamBillEntry, offsetMinutes *int, fallbackOffset int) (*UpstreamBillReport, error) {
	if len(entries) == 0 {
		return nil, errors.New("the bill has no rows")
	}
	channel, err := GetChannelById(channelId, false)
	if err != nil {
		return nil, fmt.Errorf("channel %d: %w", channelId, err)
	}
	mapping := map[string]string{}
	if m := channel.GetModelMapping(); m != "" && m != "{}" {
		if err := common.UnmarshalJsonStr(m, &mapping); err != nil {
			return nil, fmt.Errorf("channel %d model mapping: %w", channelId, err)
		}
	}

	minWall, maxWall := entries[0].Wall, entries[0].Wall
	for _, e := range entries[1:] {
		minWall, maxWall = min(minWall, e.Wall), max(maxWall, e.Wall)
	}
	// Wide enough for any offset; narrowed once the offset is known.
	var scans []upstreamBillHourScan
	if err := DB.Table("quota_data").
		Select("created_at, model_name, usage_unit, "+reconAggregates).
		Where("channel_id = ? and created_at >= ? and created_at <= ?", channelId,
			minWall-upstreamBillMaxOffsetHours*3600-3600, maxWall-upstreamBillMinOffsetHours*3600).
		Group("created_at, model_name, usage_unit").
		Scan(&scans).Error; err != nil {
		return nil, err
	}
	ourHourly := map[upstreamBillHourKey]int64{}
	for i := range scans {
		if mapped := mapping[scans[i].ModelName]; mapped != "" {
			scans[i].ModelName = mapped
		}
		ourHourly[upstreamBillHourKey{scans[i].CreatedAt, scans[i].ModelName}] += scans[i].Requests
	}
	matched := func(offset int) int64 {
		billHourly := map[upstreamBillHourKey]int64{}
		for _, e := range entries {
			at := e.Wall - int64(offset)*60
			billHourly[upstreamBillHourKey{at - at%3600, e.Model}]++
		}
		var score int64
		for key, n := range billHourly {
			score += min(n, ourHourly[key])
		}
		return score
	}

	report := &UpstreamBillReport{ChannelId: channelId, Channel: channel.Name, OffsetMinutes: fallbackOffset}
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
	var ours []reconUnitScan
	for _, s := range scans {
		if s.CreatedAt < report.From || s.CreatedAt > report.To {
			continue
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
		b.Requests++
		b.PromptTokens += e.PromptTokens
		b.CompletionTokens += e.CompletionTokens
		b.CacheReadTokens += e.CacheReadTokens
		b.Tokens += e.PromptTokens + e.CompletionTokens
		b.Cost += e.Cost
	}
	report.Rows = make([]UpstreamBillRow, 0, len(rows))
	for _, r := range rows {
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

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
