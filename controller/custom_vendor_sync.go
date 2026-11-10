package controller

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

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

// customVendorTierBody renders one tier's USD per 1M tokens. A cache read the
// vendor does not price stays in p at the input price. Anthropic publishes
// cache writes as multiples of the base input price (1.25x for 5 minutes, 2x
// for 1 hour) and the billing engine separates them for names containing
// "claude", so those models carry cc and cc1h.
func customVendorTierBody(name string, input float64, output, cacheRead *float64) (string, bool) {
	if output == nil || input <= 0 || !validCustomVendorNumber(input) || !validCustomVendorNumber(*output) {
		return "", false
	}
	body := fmt.Sprintf("p * %s + c * %s", customVendorPriceText(input), customVendorPriceText(*output))
	if cacheRead != nil && validCustomVendorNumber(*cacheRead) {
		body += " + cr * " + customVendorPriceText(*cacheRead)
	}
	if strings.Contains(strings.ToLower(name), "claude") {
		body += fmt.Sprintf(" + cc * %s + cc1h * %s", customVendorPriceText(input*1.25), customVendorPriceText(input*2))
	}
	return body, true
}

// customVendorBillingExpr renders a vendor list price as a token billing
// expression, with one branch per input-length tier. A price without an output
// price cannot be charged exactly and yields no expression.
func customVendorBillingExpr(name string, price customVendorPrice) (string, bool) {
	if len(price.Tiers) == 0 {
		body, ok := customVendorTierBody(name, price.Input, price.Output, price.CacheRead)
		if !ok {
			return "", false
		}
		return fmt.Sprintf(`tier("base", %s)`, body), true
	}
	var expr string
	for i := len(price.Tiers) - 1; i >= 0; i-- {
		tier := price.Tiers[i]
		body, ok := customVendorTierBody(name, tier.Input, tier.Output, tier.CacheRead)
		if !ok {
			return "", false
		}
		label := "base"
		if i > 0 {
			label = fmt.Sprintf("tier_%d", i+1)
		}
		leaf := fmt.Sprintf(`tier("%s", %s)`, label, body)
		if i == len(price.Tiers)-1 {
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
// list price, or false when the price cannot be charged exactly. DeepSeek is
// left out: the catalog keeps only its peak-hour price, while the built-in
// expressions carry DeepSeek's peak and off-peak prices.
func customVendorSyncExpr(name string, price customVendorPrice) (string, bool) {
	if price.Provider == customVendorDeepSeekProvider {
		return "", false
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
