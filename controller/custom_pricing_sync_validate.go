package controller

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// customBillingExprError reports why this gateway would refuse to save an
// upstream's billing expression for name, or "" when the expression is
// acceptable. It runs the checks UpdateModelPricing applies to a changed
// expression: a usage expression (u("seconds") and friends) needs a local
// task plugin whose usage schema for the model declares every key it reads,
// so a model only the upstream's plugins serve cannot adopt such a price. The
// price sync marks the candidate instead of failing the whole batch at save.
func customBillingExprError(name, expression string) string {
	generation := jsplugin.DefaultRegistry.Generation()
	if plugins := generation.PluginsByModel(name); len(plugins) > 0 {
		for _, plugin := range plugins {
			schema, _ := plugin.Meta.UsageForModel(name)
			if err := billing_setting.SmokeTestTaskExpr(expression, schema); err != nil {
				return fmt.Sprintf("plugin %s: %v", plugin.Meta.Key, err)
			}
		}
		return ""
	}
	if target, resolved := model.ResolveTaskModelAlias(generation, name); resolved {
		if plugin, ok := generation.Get(target.PluginKey); ok {
			schema, _ := plugin.Meta.UsageForModel(target.Declared)
			if err := billing_setting.SmokeTestTaskExpr(expression, schema); err != nil {
				return err.Error()
			}
			return ""
		}
	}
	if err := billing_setting.SmokeTestExpr(expression); err != nil {
		return err.Error()
	}
	return ""
}

// customConvertCharacterExpr rewrites an upstream task expression that prices
// nothing but u("characters") into the ordinary expression this gateway bills
// a speech request with. The relay counts the input text's characters as the
// request's prompt tokens, so a linear USD-per-character price is the same
// charge as p times that price per million. Only a plain linear price without
// a constant, request rules or fixed pricing converts; "" otherwise.
func customConvertCharacterExpr(expression string) string {
	keys := billingexpr.UsedUsageKeys(expression)
	if len(keys) != 1 || !keys["characters"] {
		return ""
	}
	for _, probe := range []string{"param(", "header(", "hour(", "minute(", "weekday(", "month(", "day(", "fixed("} {
		if strings.Contains(expression, probe) {
			return ""
		}
	}
	cost := func(characters float64) (float64, bool) {
		result, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: map[string]any{"characters": characters}})
		if err != nil || math.IsNaN(result) || math.IsInf(result, 0) || result < 0 {
			return 0, false
		}
		return result, true
	}
	zero, ok := cost(0)
	if !ok || zero != 0 {
		return ""
	}
	perMillion, ok := cost(1_000_000)
	if !ok {
		return ""
	}
	for _, characters := range []float64{1, 123_456, 3_000_000} {
		result, ok := cost(characters)
		if !ok || math.Abs(result-perMillion*characters/1_000_000) > 1e-9*math.Max(1, result) {
			return ""
		}
	}
	price := strconv.FormatFloat(math.Round(perMillion*1e6)/1e6, 'f', -1, 64)
	converted := fmt.Sprintf(`tier("base", p * %s)`, price)
	if billing_setting.SmokeTestExpr(converted) != nil {
		return ""
	}
	return converted
}

// customRecordBillingExprError settles what the sync does with candidate's
// expression when this gateway cannot save it: a per-character task price is
// replaced by its ordinary equivalent and the original kept under conversions;
// anything else keeps the reason under errors. Both maps are keyed by the
// upstream's name and created on first use.
func customRecordBillingExprError(errors, conversions *map[string]string, upstream, name string, candidate map[string]any) {
	expression, _ := candidate[billing_setting.BillingExprField].(string)
	if expression == "" {
		return
	}
	message := customBillingExprError(name, expression)
	if message == "" {
		return
	}
	if converted := customConvertCharacterExpr(expression); converted != "" {
		candidate[billing_setting.BillingExprField] = converted
		if *conversions == nil {
			*conversions = make(map[string]string)
		}
		(*conversions)[upstream] = expression
		return
	}
	if *errors == nil {
		*errors = make(map[string]string)
	}
	(*errors)[upstream] = message
}
