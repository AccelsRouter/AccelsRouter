package controller

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/larknotify"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

const (
	// A price is below its vendor's when it falls short by more than this
	// share, which leaves the rounding of a converted price out.
	customVendorBelowTolerance = 0.001
	// The CNY rate needs attention once the market rate buys this much less
	// CNY per USD than the rate vendor prices are converted at.
	customVendorRateDriftShare = 0.01
	// More condition combinations than this are not compared.
	customVendorBelowMaxConditions = 64
	// The European Central Bank's daily reference rate, served by Frankfurter.
	customMarketCNYPerUSDURL = "https://api.frankfurter.dev/v1/latest?from=USD&to=CNY"
)

var customBillingLengthBound = regexp.MustCompile(`\blen\s*<=?\s*([0-9]+)`)

// customChargesBelow finds where the current price of a model charges less
// than its vendor list price: the unit both are compared at (what), what the
// model charges for it and what the vendor lists, in USD per 1M tokens or per
// unit of a task fact. Where several units fall short it reports the one that
// falls short the most. A price in a shape that cannot be compared with the
// vendor's, such as one price per request against a price per token, is not
// reported.
func customChargesBelow(current, vendor string, schema map[string]jsplugin.UsageFieldSchema) (what string, charged, listed float64, below bool) {
	type unit struct {
		what    string
		charged float64
		listed  float64
	}
	var units []unit
	cost := func(expr string, params billingexpr.TokenParams, usage map[string]any) (float64, bool) {
		result, _, err := billingexpr.RunExprWithRequest(expr, params, billingexpr.RequestInput{Usage: usage})
		return result, err == nil && !math.IsNaN(result) && !math.IsInf(result, 0) && result >= 0
	}
	if priced := billingexpr.UsedUsageKeys(vendor); len(priced) > 0 {
		if len(billingexpr.UsedUsageKeys(current)) == 0 {
			return "", 0, 0, false
		}
		conditions := []map[string]any{{}}
		var quantities []string
		for _, key := range slices.Sorted(maps.Keys(schema)) {
			field := schema[key]
			var values []any
			switch {
			case field.Type == "boolean":
				values = []any{true, false}
			case len(field.Enum) > 0:
				for _, value := range field.Enum {
					values = append(values, value)
				}
			default:
				quantities = append(quantities, key)
				continue
			}
			var next []map[string]any
			for _, condition := range conditions {
				for _, value := range values {
					extended := maps.Clone(condition)
					extended[key] = value
					next = append(next, extended)
				}
			}
			conditions = next
		}
		if len(conditions) > customVendorBelowMaxConditions {
			return "", 0, 0, false
		}
		for _, condition := range conditions {
			var labels []string
			for _, key := range slices.Sorted(maps.Keys(condition)) {
				switch value := condition[key].(type) {
				case string:
					labels = append(labels, value)
				case bool:
					if value {
						labels = append(labels, key)
					}
				}
			}
			for _, quantity := range quantities {
				if !priced[quantity] {
					continue
				}
				usage := maps.Clone(condition)
				for _, other := range quantities {
					usage[other] = 0.0
				}
				// A token fact is priced per million, any other fact per unit.
				usage[quantity] = 1.0
				if schema[quantity].Unit == "token" {
					usage[quantity] = 1e6
				}
				theirs, ok := cost(vendor, billingexpr.TokenParams{}, usage)
				ours, comparable := cost(current, billingexpr.TokenParams{}, usage)
				if ok && comparable {
					units = append(units, unit{strings.Join(append(labels, quantity), " · "), ours, theirs})
				}
			}
		}
	} else {
		if billingexpr.UsesFixedPricing(current) || len(billingexpr.UsedUsageKeys(current)) > 0 {
			return "", 0, 0, false
		}
		// Each side of every input-length bound either expression names.
		lengths := []float64{1}
		for _, expr := range []string{current, vendor} {
			for _, bound := range customBillingLengthBound.FindAllStringSubmatch(expr, -1) {
				if value, err := strconv.ParseFloat(bound[1], 64); err == nil {
					lengths = append(lengths, value, value+1)
				}
			}
		}
		slices.Sort(lengths)
		lengths = slices.Compact(lengths)
		kinds := []struct{ variable, name string }{
			{"p", "input"}, {"c", "output"}, {"cr", "cache read"}, {"cc", "cache write"}, {"ai", "audio input"}, {"img", "image input"},
		}
		// A million tokens of one kind; a kind the expression does not price
		// by itself is charged as ordinary input.
		million := func(expr, variable string, length float64) billingexpr.TokenParams {
			params := billingexpr.TokenParams{Len: length}
			if variable != "c" && !billingexpr.UsedVars(expr)[variable] {
				variable = "p"
			}
			switch variable {
			case "p":
				params.P = 1e6
			case "c":
				params.C = 1e6
			case "cr":
				params.CR = 1e6
			case "cc":
				params.CC = 1e6
			case "ai":
				params.AI = 1e6
			case "img":
				params.Img = 1e6
			}
			return params
		}
		for _, length := range lengths {
			for _, kind := range kinds {
				if kind.variable != "p" && kind.variable != "c" && !billingexpr.UsedVars(vendor)[kind.variable] {
					continue
				}
				theirs, ok := cost(vendor, million(vendor, kind.variable, length), nil)
				ours, comparable := cost(current, million(current, kind.variable, length), nil)
				if !ok || !comparable {
					continue
				}
				name := kind.name
				if len(lengths) > 1 {
					name += fmt.Sprintf(" at %d input tokens", int64(length))
				}
				units = append(units, unit{name, ours / 1e6, theirs / 1e6})
			}
		}
	}
	worst := 1 - customVendorBelowTolerance
	for _, candidate := range units {
		if candidate.listed <= 0 || candidate.charged/candidate.listed >= worst {
			continue
		}
		worst = candidate.charged / candidate.listed
		what, charged, listed, below = candidate.what, candidate.charged, candidate.listed, true
	}
	return what, math.Round(charged*1e6) / 1e6, math.Round(listed*1e6) / 1e6, below
}

// customVendorUnderpriced lists the models an enabled channel serves that
// charge less than their vendor list price, by name. A model priced by ratios
// is compared through the expression its price converts to.
func customVendorUnderpriced() ([]larknotify.VendorPriceBelow, error) {
	data, err := customVendorPricingSyncData()
	if err != nil {
		return nil, nil
	}
	vendor, _ := data[billing_setting.BillingExprField].(map[string]string)
	var names []string
	for _, name := range model.GetEnabledModels() {
		if _, listed := vendor[name]; listed {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	snapshot, err := model.GetModelPricingSnapshot(names)
	if err != nil {
		return nil, err
	}
	var found []larknotify.VendorPriceBelow
	for _, entry := range snapshot.Entries {
		current, _ := entry.Effective["billing_setting.billing_expr"].(string)
		if entry.Effective["billing_setting.billing_mode"] != billing_setting.BillingModeTieredExpr {
			conversion, err := model.PreviewModelPricingConversion(entry.ModelName, entry.Configured)
			if err != nil || conversion.Expression == "" {
				continue
			}
			current = conversion.Expression
		}
		if what, charged, listed, below := customChargesBelow(current, vendor[entry.ModelName], entry.UsageSchema); below {
			found = append(found, larknotify.VendorPriceBelow{Model: entry.ModelName, What: what, Charged: charged, Listed: listed})
		}
	}
	return found, nil
}

// fetchCustomMarketCNYPerUSD reads how many CNY one USD buys at the latest
// daily reference rate, and the day it is from.
func fetchCustomMarketCNYPerUSD(ctx context.Context, client *http.Client) (rate float64, date string, err error) {
	body, err := fetchCustomVendorBody(ctx, client, customMarketCNYPerUSDURL)
	if err != nil {
		return 0, "", err
	}
	var reply struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := common.Unmarshal(body, &reply); err != nil {
		return 0, "", err
	}
	rate = reply.Rates["CNY"]
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, "", fmt.Errorf("no CNY rate in the reply")
	}
	return rate, reply.Date, nil
}

// customVendorRateDrift reports the CNY rate vendor prices are converted at
// (set) once the market rate buys noticeably less CNY per USD: prices listed
// in CNY then cost more USD than they were converted to.
func customVendorRateDrift(set, market float64, date string) *larknotify.VendorRateDrift {
	if set <= 0 || market <= 0 || market >= set*(1-customVendorRateDriftShare) {
		return nil
	}
	return &larknotify.VendorRateDrift{Set: set, Market: market, Date: date}
}
