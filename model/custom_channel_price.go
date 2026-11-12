package model

import (
	"maps"
	"math"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// CustomChannelPrice is what an upstream channel charges for a model from
// EffectiveAt on: the pricing values its price list reported, in the price
// sync's fields as JSON, or nothing once it stopped listing the model. A row
// is added only when the price changes, so a model's rows are the history of
// its cost at that channel, which reconciliation prices recorded usage with.
// ModelName is the name the upstream knows the model by.
type CustomChannelPrice struct {
	ID          int    `json:"id" gorm:"primaryKey"`
	ChannelId   int    `json:"channel_id" gorm:"index:idx_custom_channel_price,priority:1"`
	ModelName   string `json:"model_name" gorm:"size:191;index:idx_custom_channel_price,priority:2"`
	EffectiveAt int64  `json:"effective_at" gorm:"index:idx_custom_channel_price,priority:3"`
	Pricing     string `json:"pricing" gorm:"type:text"`
}

// RecordCustomChannelPrices stores what a channel's price list reports at now
// and returns how many models changed. A model whose price equals its latest
// row adds nothing. With complete set, a model the list no longer names gets
// an empty row, so usage after now is not priced at a price that is gone.
func RecordCustomChannelPrices(channelId int, prices map[string]map[string]any, complete bool, now int64) (int, error) {
	var rows []CustomChannelPrice
	if err := DB.Where("channel_id = ?", channelId).Order("effective_at, id").Find(&rows).Error; err != nil {
		return 0, err
	}
	latest := make(map[string]CustomChannelPrice, len(rows))
	for _, row := range rows {
		latest[row.ModelName] = row
	}
	var added []CustomChannelPrice
	for _, name := range slices.Sorted(maps.Keys(prices)) {
		encoded, err := common.Marshal(prices[name])
		if err != nil {
			return 0, err
		}
		// Compared as decoded values: the encoding does not order its keys.
		var next, previous map[string]any
		if err := common.Unmarshal(encoded, &next); err != nil {
			return 0, err
		}
		if row, known := latest[name]; known && row.Pricing != "" {
			if err := common.UnmarshalJsonStr(row.Pricing, &previous); err == nil && reflect.DeepEqual(previous, next) {
				continue
			}
		}
		added = append(added, CustomChannelPrice{ChannelId: channelId, ModelName: name, EffectiveAt: now, Pricing: string(encoded)})
	}
	if complete {
		for _, name := range slices.Sorted(maps.Keys(latest)) {
			if _, listed := prices[name]; !listed && latest[name].Pricing != "" {
				added = append(added, CustomChannelPrice{ChannelId: channelId, ModelName: name, EffectiveAt: now})
			}
		}
	}
	if len(added) == 0 {
		return 0, nil
	}
	return len(added), DB.Create(&added).Error
}

// customChannelPriceHistory is every recorded price of a channel, per
// upstream model name in the order they took effect.
func customChannelPriceHistory(channelId int) (map[string][]CustomChannelPrice, error) {
	var rows []CustomChannelPrice
	if err := DB.Where("channel_id = ?", channelId).Order("effective_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	history := make(map[string][]CustomChannelPrice)
	for _, row := range rows {
		history[row.ModelName] = append(history[row.ModelName], row)
	}
	return history, nil
}

// customChannelPriceAt picks the price in force at a time from a model's
// history. Usage older than the first record is priced at that first record,
// which later reports: the price was not being recorded yet.
func customChannelPriceAt(history []CustomChannelPrice, at int64) (price CustomChannelPrice, later, found bool) {
	if len(history) == 0 {
		return CustomChannelPrice{}, false, false
	}
	index := sort.Search(len(history), func(i int) bool { return history[i].EffectiveAt > at }) - 1
	if index < 0 {
		return history[0], true, history[0].Pricing != ""
	}
	return history[index], false, history[index].Pricing != ""
}

// customChannelUsage is what a channel recorded for one model in one hour.
type customChannelUsage struct {
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	UsageUnit        string
	UsageAmount      int64
}

var customChannelLengthBound = regexp.MustCompile(`\blen\s*<=?\s*([0-9]+)`)

// The task usage fact a quota_data usage unit was recorded from.
var customChannelUsageFacts = map[string]string{
	UsageUnitToken: "tokens", UsageUnitSecond: "seconds", UsageUnitImage: "image_count", UsageUnitClip: "clips",
}

// customChannelCost prices an hour of recorded usage at an upstream price, in
// USD. The hourly record does not keep what a price may depend on per request
// (the input length of each call, a video's resolution), so the cost is the
// range between the cheapest and the dearest those could make it; lowest and
// highest are equal where the price does not depend on them. Cached input is
// taken as part of the recorded input. ok is false for a price that cannot be
// applied to the record at all.
func customChannelCost(pricing map[string]any, usage customChannelUsage, name string) (lowest, highest float64, ok bool) {
	number := func(key string) (float64, bool) {
		value, exists := pricing[key].(float64)
		return value, exists && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
	}
	expression, _ := pricing[billing_setting.BillingExprField].(string)
	if pricing[billing_setting.BillingModeField] != billing_setting.BillingModeTieredExpr || expression == "" {
		if price, perCall := number("model_price"); perCall {
			return price * float64(usage.Requests), price * float64(usage.Requests), true
		}
		ratio, priced := number("model_ratio")
		if !priced {
			return 0, 0, false
		}
		// A ratio of 1 is USD 2 per 1M tokens.
		input := ratio * 2 / 1e6
		completion, cache := 1.0, 1.0
		if value, set := number("completion_ratio"); set {
			completion = value
		}
		if value, set := number("cache_ratio"); set {
			cache = value
		}
		fresh := max(usage.PromptTokens-usage.CacheReadTokens, 0)
		cost := input * (float64(fresh) + float64(usage.CacheReadTokens)*cache + float64(usage.CompletionTokens)*completion)
		return cost, cost, true
	}
	run := func(params billingexpr.TokenParams, facts map[string]any) (float64, bool) {
		result, _, err := billingexpr.RunExprWithRequest(expression, params, billingexpr.RequestInput{Usage: facts})
		return result, err == nil && !math.IsNaN(result) && !math.IsInf(result, 0) && result >= 0
	}
	var costs []float64
	if facts := billingexpr.UsedUsageKeys(expression); len(facts) > 0 {
		fact := customChannelUsageFacts[usage.UsageUnit]
		if fact == "" || !facts[fact] {
			return 0, 0, false
		}
		// Every value of the conditions the plugin serving the model declares.
		conditions := []map[string]any{{fact: float64(usage.UsageAmount)}}
		generation := jsplugin.DefaultRegistry.Generation()
		var schema map[string]jsplugin.UsageFieldSchema
		if plugin, served := generation.GetByModel(name); served {
			schema, _ = plugin.Meta.UsageForModel(name)
		} else if target, aliased := ResolveTaskModelAlias(generation, name); aliased {
			if plugin, served := generation.Get(target.PluginKey); served {
				schema, _ = plugin.Meta.UsageForModel(target.Declared)
			}
		}
		for _, key := range slices.Sorted(maps.Keys(schema)) {
			field := schema[key]
			var values []any
			switch {
			case !facts[key]:
				continue
			case field.Type == "boolean":
				values = []any{true, false}
			case len(field.Enum) > 0:
				for _, value := range field.Enum {
					values = append(values, value)
				}
			default:
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
		for _, condition := range conditions {
			if cost, valid := run(billingexpr.TokenParams{}, condition); valid {
				costs = append(costs, cost)
			}
		}
	} else {
		if billingexpr.UsesFixedPricing(expression) {
			return 0, 0, false
		}
		params := billingexpr.TokenParams{P: float64(usage.PromptTokens), C: float64(usage.CompletionTokens)}
		if billingexpr.UsedVars(expression)["cr"] {
			params.P = float64(max(usage.PromptTokens-usage.CacheReadTokens, 0))
			params.CR = float64(usage.CacheReadTokens)
		}
		lengths := []float64{1}
		for _, bound := range customChannelLengthBound.FindAllStringSubmatch(expression, -1) {
			if value, err := strconv.ParseFloat(bound[1], 64); err == nil {
				lengths = append(lengths, value+1)
			}
		}
		for _, length := range lengths {
			params.Len = length
			if cost, valid := run(params, nil); valid {
				// A token expression yields USD per 1M tokens times tokens.
				costs = append(costs, cost/1e6)
			}
		}
	}
	if len(costs) == 0 {
		return 0, 0, false
	}
	return slices.Min(costs), slices.Max(costs), true
}
