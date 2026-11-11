package controller

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// The vendor list prices as a price-sync source, so a model's price can be set
// to what its own vendor publishes. The sync never fetches the base URL: the
// source is served from the stored catalog.
const (
	customVendorPresetID      = -102
	customVendorPresetName    = "厂商标价"
	customVendorPresetBaseURL = "https://vendor-list-prices.invalid"
)

func customVendorPriceText(value float64) string {
	return strconv.FormatFloat(math.Round(value*1e6)/1e6, 'f', -1, 64)
}

// customVendorDeepSeekPeak is DeepSeek's peak window, Monday to Friday
// 01:00-04:00 and 06:00-10:00 UTC, in the form the built-in DeepSeek
// expressions use (Beijing time).
const customVendorDeepSeekPeak = `weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))`

// customVendorTierBody renders one tier's USD per 1M tokens. A cache read the
// vendor does not price stays in p at the input price. A vendor's own cache
// write price is cc; without one, names containing "claude" carry Anthropic's
// published multiples of the base input price (1.25x for 5 minutes, 2x for 1
// hour), which the billing engine separates for those names. A separate audio
// input price is ai.
func customVendorTierBody(name string, tier customVendorTier) (string, bool) {
	if tier.Output == nil || tier.Input <= 0 || !validCustomVendorNumber(tier.Input) || !validCustomVendorNumber(*tier.Output) {
		return "", false
	}
	body := fmt.Sprintf("p * %s + c * %s", customVendorPriceText(tier.Input), customVendorPriceText(*tier.Output))
	if tier.CacheRead != nil && validCustomVendorNumber(*tier.CacheRead) {
		body += " + cr * " + customVendorPriceText(*tier.CacheRead)
	}
	switch {
	case tier.CacheWrite != nil && validCustomVendorNumber(*tier.CacheWrite):
		body += " + cc * " + customVendorPriceText(*tier.CacheWrite)
	case strings.Contains(strings.ToLower(name), "claude"):
		body += fmt.Sprintf(" + cc * %s + cc1h * %s", customVendorPriceText(tier.Input*1.25), customVendorPriceText(tier.Input*2))
	}
	if tier.AudioInput != nil && validCustomVendorNumber(*tier.AudioInput) {
		body += " + ai * " + customVendorPriceText(*tier.AudioInput)
	}
	if tier.ImageInput != nil && validCustomVendorNumber(*tier.ImageInput) {
		body += " + img * " + customVendorPriceText(*tier.ImageInput)
	}
	return body, true
}

// customVendorUsageExpr renders a task model's list price in the canonical
// task shape: one tier per combination of the conditions the vendor prices by,
// over the values the model's plugin declares for them. It yields nothing when
// the plugin lacks a condition or the vendor leaves a combination unpriced.
func customVendorUsageExpr(schema map[string]jsplugin.UsageFieldSchema, tiers []customVendorTier) (string, bool) {
	conditions := make(map[string]bool)
	for _, tier := range tiers {
		for key := range tier.When {
			conditions[key] = true
		}
	}
	keys := slices.Sorted(maps.Keys(conditions))
	combinations := [][]string{{}}
	for _, key := range keys {
		field, declared := schema[key]
		values := field.Enum
		if field.Type == "boolean" {
			values = []string{"true", "false"}
		}
		if !declared || len(values) == 0 {
			return "", false
		}
		var next [][]string
		for _, combination := range combinations {
			for _, value := range values {
				next = append(next, append(slices.Clone(combination), value))
			}
		}
		combinations = next
	}
	var expr strings.Builder
	for index, combination := range combinations {
		var usage map[string]float64
		for _, tier := range tiers {
			matches := len(tier.When) == len(keys)
			for i, key := range keys {
				matches = matches && tier.When[key] == combination[i]
			}
			if matches {
				usage = tier.Usage
			}
		}
		var terms, tests, names []string
		for _, key := range slices.Sorted(maps.Keys(usage)) {
			if !validCustomVendorNumber(usage[key]) {
				return "", false
			}
			price := customVendorPriceText(usage[key])
			field, declared := schema[key]
			switch {
			case key == "input_images_after_first":
				if _, counted := schema["input_images"]; counted {
					terms = append(terms, `max(u("input_images") - 1, 0) * `+price)
				}
			case !declared:
				// A fact the model never reports, such as a size it cannot output.
			case field.Unit == "token":
				terms = append(terms, fmt.Sprintf(`u("%s") * %s / 1000000`, key, price))
			default:
				terms = append(terms, fmt.Sprintf(`u("%s") * %s`, key, price))
			}
		}
		if len(terms) == 0 {
			return "", false
		}
		for i, key := range keys {
			switch {
			case schema[key].Type != "boolean":
				tests = append(tests, fmt.Sprintf(`u("%s") == "%s"`, key, combination[i]))
				names = append(names, combination[i])
			case combination[i] == "true":
				tests = append(tests, fmt.Sprintf(`u("%s") == true`, key))
				names = append(names, key)
			default:
				tests = append(tests, fmt.Sprintf(`u("%s") == false`, key))
			}
		}
		name := strings.Join(names, "·")
		if name == "" {
			name = "base"
		}
		if index < len(combinations)-1 {
			expr.WriteString(strings.Join(tests, " && ") + " ? ")
		}
		fmt.Fprintf(&expr, `tier("%s", %s)`, name, strings.Join(terms, " + "))
		if index < len(combinations)-1 {
			expr.WriteString(" : ")
		}
	}
	return expr.String(), true
}

// customVendorTimeBandExpr renders DeepSeek's peak and off-peak prices in the
// shape of the built-in DeepSeek expressions.
func customVendorTimeBandExpr(price customVendorPrice) (string, bool) {
	if !strings.HasPrefix(price.Provider, customVendorDeepSeekProvider) || len(price.Tiers) != 2 ||
		price.Tiers[0].Band != "peak" || price.Tiers[1].Band != "off_peak" {
		return "", false
	}
	leaves := make([]string, 0, 2)
	for _, tier := range price.Tiers {
		if tier.Output == nil || tier.Input <= 0 || !validCustomVendorNumber(tier.Input) || !validCustomVendorNumber(*tier.Output) {
			return "", false
		}
		body := "p * " + customVendorPriceText(tier.Input)
		if tier.CacheRead != nil && validCustomVendorNumber(*tier.CacheRead) {
			body += " + cr * " + customVendorPriceText(*tier.CacheRead)
		}
		leaves = append(leaves, fmt.Sprintf(`tier("%s", %s + c * %s)`, tier.Band, body, customVendorPriceText(*tier.Output)))
	}
	return customVendorDeepSeekPeak + " ? " + leaves[0] + " : " + leaves[1], true
}

// customVendorBillingExpr renders a vendor list price as a token billing
// expression, with one branch per input-length tier or per time band. A price
// without an output price cannot be charged exactly and yields no expression.
func customVendorBillingExpr(name string, price customVendorPrice) (string, bool) {
	tiers := price.Tiers
	if len(tiers) == 0 {
		tiers = []customVendorTier{{Input: price.Input, Output: price.Output, CacheRead: price.CacheRead}}
	}
	if tiers[0].Band != "" {
		return customVendorTimeBandExpr(price)
	}
	var expr string
	for i := len(tiers) - 1; i >= 0; i-- {
		tier := tiers[i]
		body, ok := customVendorTierBody(name, tier)
		if !ok || tier.Band != "" {
			return "", false
		}
		label := "base"
		if i > 0 {
			label = fmt.Sprintf("tier_%d", i+1)
		}
		leaf := fmt.Sprintf(`tier("%s", %s)`, label, body)
		if i == len(tiers)-1 {
			if tier.Upper != 0 {
				return "", false
			}
			expr = leaf
			continue
		}
		if tier.Upper <= 0 {
			return "", false
		}
		expr = fmt.Sprintf("len <= %d ? %s : %s", int64(tier.Upper), leaf, expr)
	}
	return expr, true
}

// customVendorSyncExpr is the expression the price sync offers for a vendor
// list price, or false when the price cannot be charged exactly. A DeepSeek
// entry that holds only the peak-hour price is left out: DeepSeek charges half
// of it off-peak.
func customVendorSyncExpr(name string, price customVendorPrice) (string, bool) {
	if strings.HasPrefix(price.Provider, customVendorDeepSeekProvider) && len(price.Tiers) == 0 {
		return "", false
	}
	if len(price.Tiers) > 0 && price.Tiers[0].Usage != nil {
		schema, served := customTaskUsageSchema(name)
		if !served {
			return "", false
		}
		expr, ok := customVendorUsageExpr(schema, price.Tiers)
		return expr, ok && customBillingExprError(name, expr) == ""
	}
	expr, ok := customVendorBillingExpr(name, price)
	if !ok || billing_setting.SmokeTestExpr(expr) != nil {
		return "", false
	}
	return expr, true
}

// customVendorPricingSyncData serves the catalog's resolved prices (a manual
// entry first, then the vendor's own source) to the price sync.
func customVendorPricingSyncData() (map[string]any, error) {
	prices, _, err := resolveCustomVendorPrices()
	if err != nil {
		return nil, err
	}
	if len(prices) == 0 {
		return nil, errors.New("the vendor price catalog is empty; refresh it under Billing > Upstream pricing config")
	}
	modes := make(map[string]string, len(prices))
	exprs := make(map[string]string, len(prices))
	for name, price := range prices {
		expr, ok := customVendorSyncExpr(name, price)
		if !ok {
			continue
		}
		modes[name] = billing_setting.BillingModeTieredExpr
		exprs[name] = expr
	}
	if len(exprs) == 0 {
		return nil, errors.New("no vendor list price can be turned into a token price")
	}
	return map[string]any{
		billing_setting.BillingModeField: modes,
		billing_setting.BillingExprField: exprs,
	}, nil
}
