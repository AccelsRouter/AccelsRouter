package controller

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ByteDance publishes its list prices twice: BytePlus ModelArk for the
// international site, in USD under dated model IDs (seed-2-0-pro-260328), and
// Volcengine Ark for mainland China, in CNY under model family names
// (doubao-seed-2.1-pro). Both pages hold a standard online inference table
// with one row per input-length tier. Models of other vendors hosted there are
// priced by their own vendor and left out.
const (
	customVendorBytePlusProvider   = "BytePlus ModelArk pricing page"
	customVendorVolcengineProvider = "Volcengine Ark pricing page"
)

var (
	// "[0, 32]" and "(32, 128]": the upper bound of an input-length tier, in thousand tokens.
	customArkTierBound = regexp.MustCompile(`[\[(]\s*[0-9]+(?:\.[0-9]+)?\s*,\s*([0-9]+(?:\.[0-9]+)?)\s*\]`)
	customArkAmount    = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	// The date a model snapshot's ID ends with, as in doubao-seed-2-1-pro-260715.
	customArkSnapshotDate = regexp.MustCompile(`-[0-9]{6}$`)
)

// customArkTierRow is one row of a pricing table: a model's prices up to upper
// input tokens, 0 when the row has no input-length condition.
type customArkTierRow struct {
	model string
	upper float64
	tier  customVendorTier
	audio *float64
}

// readCustomArkTable reads the rows of one pricing table. columns names a
// header fragment per field; a model or provider cell left empty continues the
// row above. Rows conditioned on anything but the input length are dropped
// together with their model.
func readCustomArkTable(table customMarkdownTable, columns map[string]string) []customArkTierRow {
	index := map[string]int{"provider": -1, "model": -1, "condition": -1, "input": -1, "audio": -1, "cache": -1, "output": -1}
	for field, fragment := range columns {
		index[field] = customColumn(table.Header, fragment)
	}
	amount := func(row []string, field string) *float64 {
		match := customArkAmount.FindString(customCell(row, index[field]))
		value, err := strconv.ParseFloat(match, 64)
		if match == "" || err != nil {
			return nil
		}
		return &value
	}
	var rows []customArkTierRow
	dropped := make(map[string]bool)
	var model, provider string
	for _, row := range table.Rows {
		if name := strings.Trim(strings.ReplaceAll(customCell(row, index["model"]), `\`, ""), "` "); name != "" {
			model = strings.ToLower(name)
		}
		if owner := customCell(row, index["provider"]); owner != "" {
			provider = owner
		}
		if model == "" || (index["provider"] >= 0 && !strings.EqualFold(provider, "ByteDance")) {
			continue
		}
		entry := customArkTierRow{model: model, audio: amount(row, "audio")}
		condition := strings.ReplaceAll(customCell(row, index["condition"]), `\`, "")
		if bound := customArkTierBound.FindStringSubmatch(condition); bound != nil {
			thousands, _ := strconv.ParseFloat(bound[1], 64)
			entry.upper = thousands * 1000
		} else if condition != "" && condition != "-" && !strings.EqualFold(condition, "Not applicable") {
			dropped[model] = true
			continue
		}
		input := amount(row, "input")
		if input == nil {
			// The audio input table carries no ordinary input price.
			if entry.audio == nil {
				dropped[model] = true
				continue
			}
			rows = append(rows, entry)
			continue
		}
		entry.tier = customVendorTier{Input: *input, Output: amount(row, "output"), CacheRead: amount(row, "cache")}
		rows = append(rows, entry)
	}
	kept := rows[:0]
	for _, row := range rows {
		if !dropped[row.model] {
			kept = append(kept, row)
		}
	}
	return kept
}

// customArkVendorPrices turns table rows into list prices. perUSD is how many
// units of the page's currency one US dollar buys. The last tier of a model is
// open-ended, whatever context size the page gives as its bound.
func customArkVendorPrices(provider string, rows []customArkTierRow, perUSD float64) map[string]customVendorPrice {
	convert := func(value *float64) *float64 {
		if value == nil {
			return nil
		}
		converted := math.Round(*value/perUSD*1e6) / 1e6
		return &converted
	}
	tiers := make(map[string][]customVendorTier)
	for _, row := range rows {
		tier := row.tier
		tier.Upper = row.upper
		tier.Input = *convert(&tier.Input)
		tier.Output, tier.CacheRead, tier.AudioInput = convert(tier.Output), convert(tier.CacheRead), convert(row.audio)
		tiers[row.model] = append(tiers[row.model], tier)
	}
	prices := make(map[string]customVendorPrice, len(tiers))
	for model, list := range tiers {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Upper < list[j].Upper })
		// A model without length tiers has one row; several need a bound each.
		if len(list) > 1 && list[0].Upper == 0 {
			continue
		}
		list[len(list)-1].Upper = 0
		first := list[0]
		if first.Input <= 0 || first.Output == nil {
			continue
		}
		price := customVendorPrice{Provider: provider, Input: first.Input, Output: first.Output, CacheRead: first.CacheRead}
		if len(list) > 1 || first.AudioInput != nil {
			price.Tiers = list
		}
		prices[model] = price
	}
	return prices
}

// parseCustomBytePlusVendorPricing reads the Standard online inference table
// and the Standard audio input table beside it. A model ID also answers to its
// name without the snapshot date while every snapshot of it costs the same.
func parseCustomBytePlusVendorPricing(page []byte) (map[string]customVendorPrice, error) {
	var rows []customArkTierRow
	var audio []customArkTierRow
	for _, table := range parseCustomMarkdownTables(string(page)) {
		hasProvider := customColumn(table.Header, "provider") >= 0
		hasOutput := customColumn(table.Header, "output") >= 0
		switch {
		case rows == nil && hasProvider && hasOutput:
			rows = readCustomArkTable(table, map[string]string{"provider": "provider", "model": "model id", "condition": "prompt length", "input": "input", "cache": "cached input", "output": "output"})
		case rows != nil && audio == nil && !hasProvider && !hasOutput && customColumn(table.Header, "model id") >= 0:
			audio = readCustomArkTable(table, map[string]string{"model": "model id", "condition": "prompt length", "audio": "input"})
		}
	}
	for i := range rows {
		for _, extra := range audio {
			if extra.model == rows[i].model && extra.upper == rows[i].upper {
				rows[i].audio = extra.audio
			}
		}
	}
	prices := customArkVendorPrices(customVendorBytePlusProvider, rows, 1)
	undated := make(map[string]customVendorPrice)
	conflicting := make(map[string]bool)
	for model, price := range prices {
		family := customArkSnapshotDate.ReplaceAllString(model, "")
		if family == model {
			continue
		}
		if known, ok := undated[family]; ok && !reflect.DeepEqual(known, price) {
			conflicting[family] = true
		}
		undated[family] = price
	}
	for family, price := range undated {
		if _, listed := prices[family]; !listed && !conflicting[family] {
			prices[family] = price
		}
	}
	return customPageResult(prices)
}

// parseCustomVolcenginePricing reads the first table of the page, standard
// online inference, and converts its CNY prices at the gateway's exchange rate
// (CNY per USD). Only doubao models are kept. The page writes a version as
// "2.1" while model IDs write "2-1", so each model answers to both.
func parseCustomVolcenginePricing(page []byte) (map[string]customVendorPrice, error) {
	rate := operation_setting.USDExchangeRate
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return nil, errors.New("the USD exchange rate is not set, so CNY prices cannot be converted")
	}
	for _, table := range parseCustomMarkdownTables(string(page)) {
		if customColumn(table.Header, "模型名称") < 0 || customColumn(table.Header, "输出") < 0 {
			continue
		}
		rows := readCustomArkTable(table, map[string]string{"model": "模型名称", "condition": "条件", "input": "输入(非音频)", "audio": "输入(音频)", "cache": "缓存命中(非音频)", "output": "输出"})
		provider := fmt.Sprintf("%s (CNY at %s per USD)", customVendorVolcengineProvider, strconv.FormatFloat(rate, 'f', -1, 64))
		prices := make(map[string]customVendorPrice)
		for model, price := range customArkVendorPrices(provider, rows, rate) {
			if !strings.HasPrefix(model, "doubao-") || strings.ContainsAny(model, " >") {
				continue
			}
			prices[model] = price
			prices[strings.ReplaceAll(model, ".", "-")] = price
		}
		return customPageResult(prices)
	}
	return nil, errors.New("no standard online inference table found on the page")
}
