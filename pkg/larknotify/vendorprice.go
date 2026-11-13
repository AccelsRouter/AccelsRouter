package larknotify

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

// VendorPriceKind is what happened to a model's list price at its vendor.
type VendorPriceKind string

const (
	// VendorPricePending is a change held for review because it moved too far.
	VendorPricePending VendorPriceKind = "pending"
	VendorPriceChanged VendorPriceKind = "changed"
	// VendorPriceNew is a model the vendor lists for the first time.
	VendorPriceNew VendorPriceKind = "new"
)

// VendorPriceTier is one price a vendor lists for a model, in USD per 1M
// tokens. Range names the prompt lengths it covers ("≤200k", ">200k") and Band
// the time band ("peak", "off_peak"); both are empty for a single price.
type VendorPriceTier struct {
	Range      string
	Band       string
	Input      float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
	AudioInput *float64
}

// VendorPriceChange is one model whose list price changed. Old is empty for a
// newly listed model. InUse is set when the gateway serves the model.
type VendorPriceChange struct {
	Kind   VendorPriceKind
	Model  string
	Source string
	Old    []VendorPriceTier
	New    []VendorPriceTier
	InUse  bool
}

// VendorPriceFailure is a price source that could be read before and now cannot.
type VendorPriceFailure struct {
	Source  string
	Message string
}

// VendorPriceBelow is a model the gateway serves for less than its vendor
// lists: What names the unit that falls short the most, Charged and Listed are
// the two prices of that unit in USD. What is a kind of token (one of the
// keys of vendorPriceUnits), with Length the input tokens the prices are
// compared at when they change with the input length, or the names of a task
// plugin's facts, shown as they are.
type VendorPriceBelow struct {
	Model   string
	What    string
	Length  int64
	Charged float64
	Listed  float64
}

// VendorRateDrift is the CNY per USD rate vendor prices are converted at (Set)
// against the market rate of Date, when the market rate buys less CNY: prices
// listed in CNY then cost more USD than they were converted to.
type VendorRateDrift struct {
	Set    float64
	Market float64
	Date   string
}

// VendorPriceReport is what one refresh of the vendor list prices found.
type VendorPriceReport struct {
	Changes  []VendorPriceChange
	Failures []VendorPriceFailure
	Below    []VendorPriceBelow
	Rate     *VendorRateDrift
}

// Message keys of the vendor price card. They are declared again in
// i18n/keys.go and written in i18n/locales/*.yaml.
const (
	MsgVendorPriceTitle       = "lark.vendor_price_title"
	MsgVendorPriceKindPending = "lark.vendor_price_kind_pending"
	MsgVendorPriceKindChanged = "lark.vendor_price_kind_changed"
	MsgVendorPriceKindNew     = "lark.vendor_price_kind_new"
	MsgVendorPriceInUse       = "lark.vendor_price_in_use"
	MsgVendorPriceTier        = "lark.vendor_price_tier"
	MsgVendorPriceCacheRead   = "lark.vendor_price_cache_read"
	MsgVendorPriceCacheWrite  = "lark.vendor_price_cache_write"
	MsgVendorPriceAudioInput  = "lark.vendor_price_audio_input"
	MsgVendorPriceBandPeak    = "lark.vendor_price_band_peak"
	MsgVendorPriceBandOffPeak = "lark.vendor_price_band_off_peak"
	MsgVendorPriceFieldFailed = "lark.vendor_price_field_failed"
	MsgVendorPriceMore        = "lark.vendor_price_more"
	MsgVendorPriceNote        = "lark.vendor_price_note"
	MsgVendorPriceTitleBelow  = "lark.vendor_price_title_below"
	MsgVendorPriceFieldBelow  = "lark.vendor_price_field_below"
	MsgVendorPriceBelow       = "lark.vendor_price_below"
	MsgVendorPriceFieldRate   = "lark.vendor_price_field_rate"
	MsgVendorPriceRateDrift   = "lark.vendor_price_rate_drift"
	MsgVendorPriceUnitAt      = "lark.vendor_price_unit_at"
)

// vendorPriceUnits are the message keys of the token kinds a price is compared
// on. They are declared again in i18n/keys.go and written in
// i18n/locales/*.yaml.
var vendorPriceUnits = map[string]string{
	"input":       "lark.vendor_price_unit_input",
	"output":      "lark.vendor_price_unit_output",
	"cache read":  "lark.vendor_price_unit_cache_read",
	"cache write": "lark.vendor_price_unit_cache_write",
	"audio input": "lark.vendor_price_unit_audio_input",
	"image input": "lark.vendor_price_unit_image_input",
}

// MaxVendorPriceChanges is how many models one card lists; the rest are counted.
const MaxVendorPriceChanges = 20

func vendorPriceText(tiers []VendorPriceTier, tr Translator) string {
	usd := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	lines := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		output := "-"
		if tier.Output != nil {
			output = usd(*tier.Output)
		}
		parts := []string{tr(MsgVendorPriceTier, map[string]any{"Input": usd(tier.Input), "Output": output})}
		for _, extra := range []struct {
			key   string
			price *float64
		}{{MsgVendorPriceCacheRead, tier.CacheRead}, {MsgVendorPriceCacheWrite, tier.CacheWrite}, {MsgVendorPriceAudioInput, tier.AudioInput}} {
			if extra.price != nil {
				parts = append(parts, tr(extra.key, map[string]any{"Price": usd(*extra.price)}))
			}
		}
		line := strings.Join(parts, " · ")
		switch {
		case tier.Band == "peak":
			line = tr(MsgVendorPriceBandPeak, nil) + ": " + line
		case tier.Band == "off_peak":
			line = tr(MsgVendorPriceBandOffPeak, nil) + ": " + line
		case tier.Range != "":
			line = tier.Range + ": " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// VendorPriceCard is the message for one refresh of the vendor list prices:
// the models whose price changed or that are newly listed, the sources that
// stopped being readable, the models the gateway serves for less than their
// vendor lists and a CNY rate the market has moved away from. Changes held for
// review come first, then the models the gateway serves, then the rest by
// name. It is red when something needs a person (a held change, a failed
// source, a price below the vendor's, a drifted rate), orange otherwise.
func VendorPriceCard(report VendorPriceReport, tr Translator) Card {
	order := map[VendorPriceKind]int{VendorPricePending: 0, VendorPriceChanged: 1, VendorPriceNew: 2}
	sorted := slices.Clone(report.Changes)
	slices.SortStableFunc(sorted, func(a, b VendorPriceChange) int {
		if a.Kind != b.Kind {
			return order[a.Kind] - order[b.Kind]
		}
		if a.InUse != b.InUse {
			if a.InUse {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Model, b.Model)
	})
	kinds := map[VendorPriceKind]string{
		VendorPricePending: MsgVendorPriceKindPending,
		VendorPriceChanged: MsgVendorPriceKindChanged,
		VendorPriceNew:     MsgVendorPriceKindNew,
	}
	color := "orange"
	var fields []Field
	for _, change := range sorted[:min(len(sorted), MaxVendorPriceChanges)] {
		name := tr(kinds[change.Kind], nil) + " · " + change.Model + " (" + change.Source + ")"
		if change.InUse {
			name += " · " + tr(MsgVendorPriceInUse, nil)
		}
		value := vendorPriceText(change.New, tr)
		if len(change.Old) > 0 {
			value = vendorPriceText(change.Old, tr) + "\n→ " + value
		}
		fields = append(fields, Field{Name: name, Value: value})
		if change.Kind == VendorPricePending {
			color = "red"
		}
	}
	if extra := len(sorted) - MaxVendorPriceChanges; extra > 0 {
		fields = append(fields, Field{Name: "…", Value: tr(MsgVendorPriceMore, map[string]any{"Count": extra})})
	}
	if len(report.Failures) > 0 {
		lines := make([]string, 0, len(report.Failures))
		for _, failure := range report.Failures {
			lines = append(lines, failure.Source+": "+failure.Message)
		}
		fields = append(fields, Field{Name: tr(MsgVendorPriceFieldFailed, nil), Value: strings.Join(lines, "\n")})
		color = "red"
	}
	percent := func(share float64) string { return strconv.FormatFloat(math.Round(share*1000)/10, 'f', -1, 64) }
	if len(report.Below) > 0 {
		below := slices.Clone(report.Below)
		slices.SortFunc(below, func(a, b VendorPriceBelow) int { return strings.Compare(a.Model, b.Model) })
		lines := make([]string, 0, min(len(below), MaxVendorPriceChanges)+1)
		for _, item := range below[:min(len(below), MaxVendorPriceChanges)] {
			what := item.What
			if key, known := vendorPriceUnits[what]; known {
				what = tr(key, nil)
			}
			if item.Length > 0 {
				what = tr(MsgVendorPriceUnitAt, map[string]any{"Unit": what, "Length": item.Length})
			}
			lines = append(lines, tr(MsgVendorPriceBelow, map[string]any{
				"Model": item.Model, "What": what,
				"Charged": strconv.FormatFloat(item.Charged, 'f', -1, 64), "Listed": strconv.FormatFloat(item.Listed, 'f', -1, 64),
				"Percent": percent(1 - item.Charged/item.Listed),
			}))
		}
		if extra := len(below) - MaxVendorPriceChanges; extra > 0 {
			lines = append(lines, tr(MsgVendorPriceMore, map[string]any{"Count": extra}))
		}
		fields = append(fields, Field{Name: tr(MsgVendorPriceFieldBelow, nil), Value: strings.Join(lines, "\n")})
		color = "red"
	}
	if report.Rate != nil {
		fields = append(fields, Field{Name: tr(MsgVendorPriceFieldRate, nil), Value: tr(MsgVendorPriceRateDrift, map[string]any{
			"Set": strconv.FormatFloat(report.Rate.Set, 'f', -1, 64), "Market": strconv.FormatFloat(report.Rate.Market, 'f', -1, 64),
			"Date": report.Rate.Date, "Percent": percent(report.Rate.Set/report.Rate.Market - 1),
		})})
		color = "red"
	}
	title := MsgVendorPriceTitle
	if len(report.Changes)+len(report.Failures) == 0 {
		title = MsgVendorPriceTitleBelow
	}
	return Card{Title: tr(title, nil), Color: color, Fields: fields, Note: tr(MsgVendorPriceNote, nil)}
}
