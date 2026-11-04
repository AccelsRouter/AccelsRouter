package controller

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Parsers for the model vendors' own pricing pages. Each takes the raw page and
// returns USD per 1M tokens, keyed by the model name used in the API.

type customVendorPageSource struct {
	Name  string
	URL   string
	Parse func(page []byte) (map[string]customVendorPrice, error)
}

const customVendorPageUserAgent = "Mozilla/5.0 (compatible; price-sync)"

var customVendorPageSources = []customVendorPageSource{
	{Name: "z.ai", URL: "https://docs.z.ai/guides/overview/pricing.md", Parse: parseCustomZaiPricing},
	{Name: "deepseek", URL: "https://api-docs.deepseek.com/quick_start/pricing.md", Parse: parseCustomDeepSeekPricing},
	{Name: "kimi", URL: "https://platform.kimi.ai/docs/pricing/chat", Parse: parseCustomKimiPricing},
	{Name: "anthropic", URL: "https://platform.claude.com/docs/en/about-claude/pricing.md", Parse: parseCustomClaudePricing},
	{Name: "xai", URL: "https://docs.x.ai/developers/pricing.md", Parse: parseCustomXAIPricing},
}

type customMarkdownTable struct {
	Header []string
	Rows   [][]string
}

var (
	customMarkdownSeparator = regexp.MustCompile(`^:?-+:?$`)
	customMarkdownLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	customMarkdownTag       = regexp.MustCompile(`<[^>]+>`)
	customDollarAmount      = regexp.MustCompile(`\$\s*([0-9]+(?:\.[0-9]+)?)`)
	customTrailingParens    = regexp.MustCompile(`\s*\(.*\)\s*$`)
	customNonAlnum          = regexp.MustCompile(`[^a-z0-9]+`)
)

func parseCustomMarkdownTables(md string) []customMarkdownTable {
	var tables []customMarkdownTable
	var block [][]string
	flush := func() {
		if len(block) >= 3 && isCustomMarkdownSeparatorRow(block[1]) {
			tables = append(tables, customMarkdownTable{Header: block[0], Rows: block[2:]})
		}
		block = nil
	}
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			flush()
			continue
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
		cells := strings.Split(line, "|")
		for i := range cells {
			cells[i] = cleanCustomMarkdownCell(cells[i])
		}
		block = append(block, cells)
	}
	flush()
	return tables
}

func isCustomMarkdownSeparatorRow(cells []string) bool {
	for _, cell := range cells {
		if !customMarkdownSeparator.MatchString(cell) {
			return false
		}
	}
	return len(cells) > 0
}

func cleanCustomMarkdownCell(cell string) string {
	cell = customMarkdownLink.ReplaceAllString(cell, "$1")
	cell = customMarkdownTag.ReplaceAllString(cell, "")
	return strings.TrimSpace(cell)
}

// customFirstDollar reads the first dollar amount of a cell; "Free", "-" and other
// text yield false.
func customFirstDollar(cell string) (float64, bool) {
	match := customDollarAmount.FindStringSubmatch(cell)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	return value, err == nil
}

func customColumn(header []string, contains string) int {
	for i, name := range header {
		if strings.Contains(strings.ToLower(name), contains) {
			return i
		}
	}
	return -1
}

func customCell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

func customPagePrice(provider string, row []string, input, output, cache int) (customVendorPrice, bool) {
	in, ok := customFirstDollar(customCell(row, input))
	if !ok || in <= 0 {
		return customVendorPrice{}, false
	}
	price := customVendorPrice{Provider: provider, Input: in}
	if out, ok := customFirstDollar(customCell(row, output)); ok {
		price.Output = &out
	}
	if hit, ok := customFirstDollar(customCell(row, cache)); ok {
		price.CacheRead = &hit
	}
	return price, true
}

// parseCustomPriceTables handles the common layout of one model per row with
// input, output and cached-input columns. Rows of a model repeated for a longer
// prompt tier are ignored, so the first (base) tier wins.
func parseCustomPriceTables(md, provider, inputHeader, outputHeader, cacheHeader string, name func(string) string) map[string]customVendorPrice {
	prices := make(map[string]customVendorPrice)
	for _, table := range parseCustomMarkdownTables(md) {
		input := customColumn(table.Header, inputHeader)
		output := customColumn(table.Header, outputHeader)
		cache := customColumn(table.Header, cacheHeader)
		if input < 0 || output < 0 {
			continue
		}
		for _, row := range table.Rows {
			model := name(customCell(row, 0))
			if model == "" {
				continue
			}
			if _, taken := prices[model]; taken {
				continue
			}
			if price, ok := customPagePrice(provider, row, input, output, cache); ok {
				prices[model] = price
			}
		}
	}
	return prices
}

func customLowerName(value string) string {
	return strings.ToLower(strings.TrimSpace(customTrailingParens.ReplaceAllString(value, "")))
}

func customPageResult(prices map[string]customVendorPrice) (map[string]customVendorPrice, error) {
	if len(prices) == 0 {
		return nil, fmt.Errorf("no prices found on the page")
	}
	return prices, nil
}

func parseCustomZaiPricing(page []byte) (map[string]customVendorPrice, error) {
	return customPageResult(parseCustomPriceTables(string(page), "z.ai pricing page", "input", "output", "cached input", customLowerName))
}

func parseCustomXAIPricing(page []byte) (map[string]customVendorPrice, error) {
	return customPageResult(parseCustomPriceTables(string(page), "x.ai pricing page", "input /", "output /", "cached input", customLowerName))
}

func parseCustomClaudePricing(page []byte) (map[string]customVendorPrice, error) {
	name := func(value string) string {
		model := customNonAlnum.ReplaceAllString(customLowerName(value), "-")
		model = strings.Trim(model, "-")
		if !strings.HasPrefix(model, "claude-") {
			return ""
		}
		return model
	}
	return customPageResult(parseCustomPriceTables(string(page), "Anthropic pricing page", "base input", "output", "cache hits", name))
}

// parseCustomDeepSeekPricing reads the model-per-column table. DeepSeek bills
// peak and off-peak hours differently; the peak price is used, which is the
// higher one. Model versions listed in the table are registered as aliases.
func parseCustomDeepSeekPricing(page []byte) (map[string]customVendorPrice, error) {
	const provider = "DeepSeek pricing page (peak)"
	prices := make(map[string]customVendorPrice)
	for _, table := range parseCustomMarkdownTables(string(page)) {
		if len(table.Header) == 0 || !strings.EqualFold(table.Header[0], "model") {
			continue
		}
		type column struct {
			names []string
			price customVendorPrice
		}
		columns := make(map[int]*column)
		for i, cell := range table.Header {
			if i == 0 || strings.EqualFold(cell, "model") || cell == "" {
				continue
			}
			columns[i] = &column{names: []string{customLowerName(cell)}, price: customVendorPrice{Provider: provider}}
		}
		for _, row := range table.Rows {
			label := strings.ToLower(customCell(row, 0))
			if strings.HasPrefix(label, "model version") {
				for i, col := range columns {
					if version := strings.ToLower(customCell(row, i)); version != "" {
						col.names = append(col.names, version)
					}
				}
				continue
			}
			if !strings.HasPrefix(label, "pricing") || !strings.EqualFold(customCell(row, 2), "peak") {
				continue
			}
			kind := strings.ToLower(customCell(row, 1))
			for i, col := range columns {
				value, ok := customFirstDollar(customCell(row, i))
				if !ok {
					continue
				}
				switch {
				case strings.Contains(kind, "cache miss"):
					col.price.Input = value
				case strings.Contains(kind, "cache hit"):
					hit := value
					col.price.CacheRead = &hit
				case strings.Contains(kind, "output"):
					out := value
					col.price.Output = &out
				}
			}
		}
		for _, col := range columns {
			if col.price.Input <= 0 {
				continue
			}
			for _, name := range col.names {
				prices[name] = col.price
			}
		}
	}
	// The page documents deepseek-v4-flash as a legacy name billed at the flash price.
	if flash, ok := prices["deepseek-flash"]; ok {
		prices["deepseek-v4-flash"] = flash
	}
	return customPageResult(prices)
}

// The Kimi pricing tables are compiled into the page script as
// `children:`<model>`}),`$<in> / MTok`,`$<out> / MTok`[,`$<cached> / MTok`]`.
var customKimiRow = regexp.MustCompile("children:`(kimi-[a-z0-9.-]+)`\\}\\),`\\$([0-9.]+) / MTok`,`\\$([0-9.]+) / MTok`(?:,`\\$([0-9.]+) / MTok`)?")

func parseCustomKimiPricing(page []byte) (map[string]customVendorPrice, error) {
	prices := make(map[string]customVendorPrice)
	for _, match := range customKimiRow.FindAllStringSubmatch(string(page), -1) {
		input, err1 := strconv.ParseFloat(match[2], 64)
		output, err2 := strconv.ParseFloat(match[3], 64)
		if err1 != nil || err2 != nil || input <= 0 {
			continue
		}
		price := customVendorPrice{Provider: "Kimi pricing page", Input: input, Output: &output}
		if match[4] != "" {
			if hit, err := strconv.ParseFloat(match[4], 64); err == nil {
				price.CacheRead = &hit
			}
		}
		if _, taken := prices[match[1]]; !taken {
			prices[match[1]] = price
		}
	}
	return customPageResult(prices)
}
