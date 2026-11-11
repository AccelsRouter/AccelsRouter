package controller

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsers for the model vendors' own pricing pages. Each takes the raw page and
// returns USD per 1M tokens, keyed by the model name used in the API.

type customVendorPageSource struct {
	Name  string
	URL   string
	Parse func(page []byte) (map[string]customVendorPrice, error)
}

const customVendorPageUserAgent = "Mozilla/5.0 (compatible; price-sync)"

// DeepSeek prices peak and off-peak hours differently; an entry stored before
// both were read carries this name with a "(peak)" suffix.
const customVendorDeepSeekProvider = "DeepSeek pricing page"

// The peak window the DeepSeek page states; the price sync's expression for it
// is customVendorDeepSeekPeak. Off-peak prices are only kept while the page
// still states this window.
const customDeepSeekPeakWindow = "Peak hours are 01:00 - 04:00 and 06:00 - 10:00 UTC, Monday through Friday"

var customVendorPageSources = []customVendorPageSource{
	{Name: "z.ai", URL: "https://docs.z.ai/guides/overview/pricing.md", Parse: parseCustomZaiPricing},
	{Name: "deepseek", URL: "https://api-docs.deepseek.com/quick_start/pricing.md", Parse: parseCustomDeepSeekPricing},
	{Name: "kimi", URL: "https://platform.kimi.ai/docs/pricing/chat", Parse: parseCustomKimiPricing},
	{Name: "anthropic", URL: "https://platform.claude.com/docs/en/about-claude/pricing.md", Parse: parseCustomClaudePricing},
	{Name: "xai", URL: "https://docs.x.ai/developers/pricing.md", Parse: parseCustomXAIPricing},
	{Name: "google", URL: "https://ai.google.dev/gemini-api/docs/pricing.md.txt", Parse: func(page []byte) (map[string]customVendorPrice, error) {
		return parseCustomGooglePricing(page, time.Now())
	}},
	// OpenAI's own pricing pages refuse automated clients, so its list prices
	// are read from OpenRouter's public model list, which carries each
	// provider's own price.
	{Name: "openai", URL: "https://openrouter.ai/api/v1/models", Parse: parseCustomOpenAIPricing},
	// ByteDance: the international site in USD, the mainland China site in CNY.
	{Name: "byteplus", URL: "https://docs.byteplus.com/docs/en/modelark/model-pricing.md", Parse: parseCustomBytePlusVendorPricing},
	{Name: "volcengine", URL: "https://docs.volcengine.com/docs/zh/ark/model-pricing.md", Parse: parseCustomVolcenginePricing},
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

var (
	customTierBelow = regexp.MustCompile(`(?i)(?:<=?|≤|up to)\s*([0-9][0-9,.]*)\s*([km]?)`)
	customTierAbove = regexp.MustCompile(`(?i)(?:>=?|≥|over)\s*([0-9][0-9,.]*)\s*([km]?)`)
)

func customTierTokens(match []string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	switch strings.ToLower(match[2]) {
	case "k":
		value *= 1000
	case "m":
		value *= 1000000
	}
	return value, true
}

// customTierMarker reads the prompt-length condition from a model cell such as
// "grok-4.7 (< 200k prompt tokens)" or "Claude Haiku 5.5 (for prompts over 100,000
// tokens)". It returns the tier's upper bound (0 for the open-ended top tier).
func customTierMarker(cell string) (upper float64, ok bool) {
	open := strings.Index(cell, "(")
	if open < 0 {
		return 0, false
	}
	note := cell[open:]
	if match := customTierBelow.FindStringSubmatch(note); match != nil {
		return customTierTokens(match)
	}
	if match := customTierAbove.FindStringSubmatch(note); match != nil {
		if _, valid := customTierTokens(match); valid {
			return 0, true
		}
	}
	return 0, false
}

// parseCustomPriceTables handles the common layout of one model per row with
// input, output and cached-input columns. A model listed again for a longer prompt
// becomes a tier of the first (base) row; any other repeat is ignored.
func parseCustomPriceTables(md, provider, inputHeader, outputHeader, cacheHeader string, name func(string) string) map[string]customVendorPrice {
	prices := make(map[string]customVendorPrice)
	firstTable := make(map[string]int)
	for tableIndex, table := range parseCustomMarkdownTables(md) {
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
			price, ok := customPagePrice(provider, row, input, output, cache)
			if !ok {
				continue
			}
			existing, taken := prices[model]
			if taken && firstTable[model] != tableIndex {
				continue
			}
			upper, tiered := customTierMarker(customCell(row, 0))
			tier := customVendorTier{Upper: upper, Input: price.Input, Output: price.Output, CacheRead: price.CacheRead}
			if !taken {
				if tiered {
					price.Tiers = []customVendorTier{tier}
				}
				prices[model] = price
				firstTable[model] = tableIndex
				continue
			}
			if tiered && len(existing.Tiers) > 0 {
				existing.Tiers = append(existing.Tiers, tier)
				prices[model] = existing
			}
		}
	}
	// A tier list is only kept when it has a bounded first tier and a top tier.
	for model, price := range prices {
		if len(price.Tiers) == 0 {
			continue
		}
		if len(price.Tiers) < 2 || price.Tiers[0].Upper <= 0 || price.Tiers[len(price.Tiers)-1].Upper != 0 {
			price.Tiers = nil
			prices[model] = price
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
// peak and off-peak hours differently: the peak price is the base price, and
// both are kept as time bands while the page states the known peak window.
// Model versions listed in the table are registered as aliases.
func parseCustomDeepSeekPricing(page []byte) (map[string]customVendorPrice, error) {
	const provider = customVendorDeepSeekProvider
	knownWindow := strings.Contains(string(page), customDeepSeekPeakWindow)
	prices := make(map[string]customVendorPrice)
	for _, table := range parseCustomMarkdownTables(string(page)) {
		if len(table.Header) == 0 || !strings.EqualFold(table.Header[0], "model") {
			continue
		}
		type column struct {
			names   []string
			peak    customVendorTier
			offPeak customVendorTier
		}
		columns := make(map[int]*column)
		for i, cell := range table.Header {
			if i == 0 || strings.EqualFold(cell, "model") || cell == "" {
				continue
			}
			columns[i] = &column{names: []string{customLowerName(cell)}, peak: customVendorTier{Band: "peak"}, offPeak: customVendorTier{Band: "off_peak"}}
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
			if !strings.HasPrefix(label, "pricing") {
				continue
			}
			band := strings.ToLower(customCell(row, 2))
			if band != "peak" && band != "off-peak" {
				continue
			}
			kind := strings.ToLower(customCell(row, 1))
			for i, col := range columns {
				value, ok := customFirstDollar(customCell(row, i))
				if !ok {
					continue
				}
				tier := &col.peak
				if band == "off-peak" {
					tier = &col.offPeak
				}
				switch {
				case strings.Contains(kind, "cache miss"):
					tier.Input = value
				case strings.Contains(kind, "cache hit"):
					tier.CacheRead = &value
				case strings.Contains(kind, "output"):
					tier.Output = &value
				}
			}
		}
		for _, col := range columns {
			if col.peak.Input <= 0 {
				continue
			}
			price := customVendorPrice{Provider: provider, Input: col.peak.Input, Output: col.peak.Output, CacheRead: col.peak.CacheRead}
			if knownWindow && col.peak.Output != nil && col.offPeak.Input > 0 && col.offPeak.Output != nil {
				price.Tiers = []customVendorTier{col.peak, col.offPeak}
			}
			for _, name := range col.names {
				prices[name] = price
			}
		}
	}
	// The page documents these legacy names as billed at the flash price.
	if flash, ok := prices["deepseek-flash"]; ok {
		prices["deepseek-v4-flash"] = flash
		prices["deepseek-v4-flash-vision-exp"] = flash
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
