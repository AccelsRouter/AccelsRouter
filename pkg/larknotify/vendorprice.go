package larknotify

import (
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

// VendorPriceReport is what one refresh of the vendor list prices found.
type VendorPriceReport struct {
	Changes  []VendorPriceChange
	Failures []VendorPriceFailure
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
)

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
// the models whose price changed or that are newly listed, and the sources
// that stopped being readable. Changes held for review come first, then the
// models the gateway serves, then the rest by name. It is red when something
// needs a person (a held change, a failed source), orange otherwise.
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
	return Card{Title: tr(MsgVendorPriceTitle, nil), Color: color, Fields: fields, Note: tr(MsgVendorPriceNote, nil)}
}
