package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
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

// customRecordBillingExprError stores the reason candidate's expression cannot
// be saved here under the upstream's name, creating errors on first use.
func customRecordBillingExprError(errors *map[string]string, upstream, name string, candidate map[string]any) {
	expression, _ := candidate[billing_setting.BillingExprField].(string)
	if expression == "" {
		return
	}
	message := customBillingExprError(name, expression)
	if message == "" {
		return
	}
	if *errors == nil {
		*errors = make(map[string]string)
	}
	(*errors)[upstream] = message
}
