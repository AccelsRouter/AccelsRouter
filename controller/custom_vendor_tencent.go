package controller

import (
	"errors"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Tencent publishes the list prices of TokenHub on its mainland China docs, in
// CNY per 1M tokens; the international pricing page answers automated clients
// with a browser challenge. The page prices every vendor TokenHub hosts: only
// Tencent's own language models (model IDs hy… and hunyuan…) are kept, the
// others are priced by their own vendor.
const (
	customVendorTencentProvider = "Tencent Cloud TokenHub pricing page"
	customVendorTencentURL      = "https://cloud.tencent.com/document/product/1823/130055"
)

var (
	customTencentRow  = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	customTencentCell = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	customTencentTag  = regexp.MustCompile(`(?s)<[^>]*>`)
	// "至 2026 年 12 月 31 日 23:59:59（北京时间），Hy4 模型调用费用按目录价格的 50% 结算":
	// until that Beijing time the named model is charged that share of its list price.
	customTencentDiscount = regexp.MustCompile(`至\s*([0-9]{4})\s*年\s*([0-9]{1,2})\s*月\s*([0-9]{1,2})\s*日\s*([0-9]{1,2}:[0-9]{2}:[0-9]{2})（北京时间），\s*([^，。]+?)\s*模型调用费用按目录价格的\s*([0-9]+(?:\.[0-9]+)?)%\s*结算`)
)

// customTencentText is the text of a piece of the page, without its markup.
func customTencentText(markup string) string {
	text := html.UnescapeString(customTencentTag.ReplaceAllString(markup, " "))
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "\ufeff", "")), " ")
}

// parseCustomTencentPricing reads the language model tables of the TokenHub
// pricing page and converts their CNY at the rate set for vendor prices. The
// page holds one table per region; a model takes the price of the first one
// that lists it (Guangzhou). A model priced by input length or by time of day
// is left out, since only an unconditional row is read exactly. A limited-time
// discount the page announces for a model applies until the time it states.
func parseCustomTencentPricing(page []byte, now time.Time) (map[string]customVendorPrice, error) {
	rate, ok := customVendorCNYPerUSD()
	if !ok {
		return nil, errors.New("Tencent lists prices in CNY: set the CNY per USD rate for vendor prices under Billing > Upstream pricing config")
	}
	currency := "CNY at " + strconv.FormatFloat(rate, 'f', -1, 64) + " per USD"

	type discount struct {
		share float64
		note  string
	}
	discounts := make(map[string]discount)
	beijing := time.FixedZone("UTC+8", 8*3600)
	for _, match := range customTencentDiscount.FindAllStringSubmatch(customTencentText(string(page)), -1) {
		until, err := time.ParseInLocation("2006-1-2 15:04:05", match[1]+"-"+match[2]+"-"+match[3]+" "+match[4], beijing)
		share, _ := strconv.ParseFloat(match[6], 64)
		if err != nil || share <= 0 || share >= 100 || !now.Before(until) {
			continue
		}
		discounts[strings.ToLower(match[5])] = discount{share: share / 100, note: fmt.Sprintf("%s%% of the list price until %s", match[6], until.Format("2006-01-02 15:04 UTC+8"))}
	}

	prices := make(map[string]customVendorPrice)
	for _, table := range strings.Split(string(page), "<table")[1:] {
		var rows [][]string
		for _, row := range customTencentRow.FindAllStringSubmatch(table, -1) {
			var cells []string
			for _, cell := range customTencentCell.FindAllStringSubmatch(row[1], -1) {
				cells = append(cells, customTencentText(cell[1]))
			}
			rows = append(rows, cells)
		}
		if len(rows) == 0 {
			continue
		}
		header := rows[0]
		id, input, output := customColumn(header, "调用参数"), customColumn(header, "推理输入"), customColumn(header, "推理输出")
		if id < 0 || input < 0 || output < 0 {
			continue
		}
		for _, cells := range rows[1:] {
			if len(cells) != len(header) {
				continue
			}
			name := strings.ToLower(cells[id])
			if _, listed := prices[name]; listed || strings.Contains(name, " ") || (!strings.HasPrefix(name, "hy") && !strings.HasPrefix(name, "hunyuan")) {
				continue
			}
			if condition := customCell(cells, customColumn(header, "条件")); condition != "" && condition != "-" {
				continue
			}
			if band := customCell(cells, customColumn(header, "峰谷")); band != "" && band != "-" {
				continue
			}
			provider, share := customVendorTencentProvider+" ("+currency+")", 1.0
			if reduced, found := discounts[strings.ToLower(customCell(cells, customColumn(header, "模型名称")))]; found {
				provider, share = customVendorTencentProvider+" ("+currency+", "+reduced.note+")", reduced.share
			}
			usd := func(cell string) *float64 {
				value, err := strconv.ParseFloat(cell, 64)
				if err != nil || value <= 0 {
					return nil
				}
				value = math.Round(value*share/rate*1e6) / 1e6
				return &value
			}
			in, out := usd(cells[input]), usd(cells[output])
			if in == nil || out == nil {
				continue
			}
			prices[name] = customVendorPrice{Provider: provider, Input: *in, Output: out, CacheRead: usd(customCell(cells, customColumn(header, "缓存命中")))}
		}
	}
	return customPageResult(prices)
}
