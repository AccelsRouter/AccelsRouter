package controller

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

const (
	customAlibabaIntlHost       = "dashscope-intl.aliyuncs.com"
	customAlibabaModelsPath     = "/api/v1/models"
	customAlibabaPageSize       = 100
	customAlibabaMaxPages       = 50
	customAlibabaTokensPerK     = 1024
	customAlibabaPerMillionUnit = "per 1m tokens"
	customAlibabaPerImageUnit   = "per image"
	customAlibabaPerSecondUnit  = "per second"
)

// customAlibabaVideoItem is the price of one second of video: video_ratio for
// every resolution, or video_ratio_720p and its like for one resolution.
var customAlibabaVideoItem = regexp.MustCompile(`^video_ratio(?:_(\d+p))?$`)

var customAlibabaRangeBound = regexp.MustCompile(`^(?:\d+(?:\.\d+)?[kK]?\s*<\s*)?(?:Input|Token|Tokens)\s*<=\s*(\d+(?:\.\d+)?)\s*([kKmM]?)$`)

type customAlibabaPriceItem struct {
	Type      string `json:"type"`
	Price     any    `json:"price"`
	PriceUnit string `json:"price_unit"`
	TimeBand  string `json:"time_band"`
}

type customAlibabaPriceTier struct {
	RangeName string                   `json:"range_name"`
	Prices    []customAlibabaPriceItem `json:"prices"`
}

type customAlibabaModel struct {
	Model    string                   `json:"model"`
	Provider string                   `json:"provider"`
	Prices   []customAlibabaPriceTier `json:"prices"`
}

type customAlibabaModelsPage struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  struct {
		Total  int                  `json:"total"`
		Models []customAlibabaModel `json:"models"`
	} `json:"output"`
}

func isCustomAlibabaIntlBaseURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	return err == nil && strings.EqualFold(parsed.Hostname(), customAlibabaIntlHost)
}

// fetchCustomAlibabaModels pages through the Model Studio model list. The key is the
// channel's own DashScope key; the list endpoint reports USD prices on the
// international (Singapore) site.
func fetchCustomAlibabaModels(ctx context.Context, client *http.Client, baseURL, key string) ([]customAlibabaModel, error) {
	var models []customAlibabaModel
	for page := 1; page <= customAlibabaMaxPages; page++ {
		query := url.Values{"page_no": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(customAlibabaPageSize)}, "language": {"en-US"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+customAlibabaModelsPath+"?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		var decoded customAlibabaModelsPage
		if err := common.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
		if !decoded.Success && decoded.Code != "" {
			return nil, fmt.Errorf("%s: %s", decoded.Code, decoded.Message)
		}
		models = append(models, decoded.Output.Models...)
		if len(decoded.Output.Models) == 0 || len(models) >= decoded.Output.Total {
			break
		}
	}
	return models, nil
}

// customPricingSkippedField carries, beside a sync result's pricing fields, why
// each model the source lists got no price: model name to reason.
const customPricingSkippedField = "custom_skipped"

// customAlibabaPeakHours is when Model Studio charges the peak price of a model
// it prices by time of day: 08:00-22:00 Beijing time, every day. The price list
// names the band of each price but not its hours. Model Studio documents them
// on its DeepSeek model pages, so only DeepSeek models priced this way convert.
const customAlibabaPeakHours = `hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 22`

// convertCustomAlibabaModelsToBillingExpr turns Model Studio USD price lists into
// billing expressions. A model is skipped, never guessed, when any tier carries
// a billing item or range this converter does not understand; the reason is
// kept under customPricingSkippedField.
func convertCustomAlibabaModelsToBillingExpr(models []customAlibabaModel) map[string]any {
	modes := make(map[string]string)
	exprs := make(map[string]string)
	skipped := make(map[string]string)
	for _, item := range models {
		name := strings.TrimSpace(item.Model)
		if name == "" {
			continue
		}
		expr, reason := customAlibabaBillingExpr(name, item)
		if reason == "" {
			if _, err := billingexpr.CompileFromCache(expr); err != nil {
				reason = err.Error()
			}
		}
		if reason != "" {
			skipped[name] = reason
			continue
		}
		modes[name] = billing_setting.BillingModeTieredExpr
		exprs[name] = expr
	}
	if len(exprs) == 0 {
		return nil
	}
	return map[string]any{
		billing_setting.BillingModeField: modes,
		billing_setting.BillingExprField: exprs,
		customPricingSkippedField:        skipped,
	}
}

// customAlibabaBillingExpr renders the price list of one model, or says why it
// cannot: token prices by input length, DeepSeek token prices by time of day,
// one price per image, or prices per second of video.
func customAlibabaBillingExpr(name string, item customAlibabaModel) (expr string, reason string) {
	tiers := item.Prices
	if len(tiers) == 0 {
		return "", "Model Studio lists no price"
	}
	type boundedTier struct {
		limit float64
		body  string
	}
	bounded := make([]boundedTier, 0, len(tiers))
	for _, tier := range tiers {
		prices, reason := customAlibabaTierPrices(tier.Prices)
		if reason != "" {
			return "", reason
		}
		if len(prices.video) > 0 {
			if len(tiers) != 1 {
				return "", "per-second video prices with input-length tiers"
			}
			return customAlibabaVideoExpr(name, prices.video)
		}
		if prices.image != "" {
			if len(tiers) != 1 {
				return "", "per-image price with input-length tiers"
			}
			fixed := fmt.Sprintf(`tier("image", fixed(%s)) * image_count`, prices.image)
			usage := fmt.Sprintf(`tier("image", u("image_count") * %s)`, prices.image)
			// A model a task plugin serves is billed on the plugin's image_count fact.
			if customBillingExprError(name, fixed) != "" && customBillingExprError(name, usage) == "" {
				return usage, ""
			}
			return fixed, ""
		}
		if prices.offPeak != "" {
			if len(tiers) != 1 {
				return "", "time-of-day prices with input-length tiers"
			}
			if !strings.HasPrefix(strings.ToLower(item.Provider), "deepseek") {
				return "", "priced by time of day, and the peak hours are only known for DeepSeek models"
			}
			return fmt.Sprintf(`%s ? tier("peak", %s) : tier("off_peak", %s)`, customAlibabaPeakHours, prices.tokens, prices.offPeak), ""
		}
		limit, label, ok := customAlibabaRangeLimit(tier.RangeName, len(tiers) == 1)
		if !ok {
			return "", fmt.Sprintf("unreadable input range %q", tier.RangeName)
		}
		bounded = append(bounded, boundedTier{limit, fmt.Sprintf(`tier("%s", %s)`, label, prices.tokens)})
	}
	sort.Slice(bounded, func(i, j int) bool { return bounded[i].limit < bounded[j].limit })
	expr = bounded[len(bounded)-1].body
	for i := len(bounded) - 2; i >= 0; i-- {
		expr = fmt.Sprintf("len <= %d ? %s : %s", int64(bounded[i].limit), bounded[i].body, expr)
	}
	return expr, ""
}

// customAlibabaVideoExpr bills a video model on the seconds and resolution its
// task plugin reports: one tier per resolution the plugin declares for the
// model, each at Model Studio's price per second. A model no plugin meters, a
// declared resolution without a price, or a plugin fact Model Studio does not
// price by leaves the model without a price.
func customAlibabaVideoExpr(name string, video map[string]string) (expr string, reason string) {
	schema, served := customTaskUsageSchema(name)
	if _, metered := schema["seconds"]; !served || !metered {
		return "", "priced per second of video, and no task plugin on this gateway meters the seconds of this model"
	}
	for _, key := range slices.Sorted(maps.Keys(schema)) {
		if field := schema[key]; key != "resolution" && (len(field.Enum) > 0 || field.Type == "boolean") {
			return "", fmt.Sprintf("the plugin also prices this model by %s, and Model Studio lists per-second prices by resolution only", key)
		}
	}
	tiers := []customVendorTier{{}}
	if _, flat := video[""]; !flat {
		resolutions := schema["resolution"].Enum
		if len(resolutions) == 0 {
			return "", "priced per second by resolution, and the plugin serving this model reports no resolution"
		}
		tiers = make([]customVendorTier, len(resolutions))
		for i, resolution := range resolutions {
			if _, listed := video[resolution]; !listed {
				return "", fmt.Sprintf("Model Studio lists no per-second price for %s", resolution)
			}
			tiers[i].When = map[string]string{"resolution": resolution}
		}
	} else if len(video) != 1 {
		return "", "one per-second price for every resolution mixed with prices by resolution"
	}
	for i := range tiers {
		price, err := strconv.ParseFloat(video[tiers[i].When["resolution"]], 64)
		if err != nil {
			return "", "unreadable per-second price"
		}
		tiers[i].Usage = map[string]float64{"seconds": price}
	}
	expr, ok := customVendorUsageExpr(schema, tiers)
	if !ok {
		return "", "unreadable per-second price"
	}
	return expr, customBillingExprError(name, expr)
}

// customAlibabaTierPrice is one tier of a Model Studio price list: the token
// price body (the peak one when offPeak is set), the price of one image, or
// the price of one second of video by resolution ("" when it has one price).
type customAlibabaTierPrice struct {
	tokens  string
	offPeak string
	image   string
	video   map[string]string
}

// customAlibabaTierPrices reads one tier from real-time prices per 1M tokens:
// input, output, optional implicit cache hit (explicit cache read is the
// fallback) and optional explicit cache creation, either at one standard price
// or at a peak and an off-peak price, or exactly one image_number price per
// image, or video_ratio prices per second. Batch prices are ignored; any other
// item makes the whole model unsupported, with the reason.
func customAlibabaTierPrices(items []customAlibabaPriceItem) (customAlibabaTierPrice, string) {
	type bandPrices struct{ input, output, cacheImplicit, cacheRead, cacheCreate string }
	bands := map[string]*bandPrices{"standard": {}, "peak": {}, "off_peak": {}}
	var image string
	video := make(map[string]string)
	for _, item := range items {
		var band string
		switch strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(item.TimeBand))) {
		case "", "standard":
			band = "standard"
		case "peak", "busy":
			band = "peak"
		case "offpeak", "idle":
			band = "off_peak"
		default:
			return customAlibabaTierPrice{}, fmt.Sprintf("unknown time band %q", item.TimeBand)
		}
		price, valid := customAlibabaPriceString(item.Price)
		if !valid {
			return customAlibabaTierPrice{}, fmt.Sprintf("unreadable price of %s", item.Type)
		}
		unit := strings.ToLower(strings.TrimSpace(item.PriceUnit))
		perM := unit == customAlibabaPerMillionUnit || unit == "per million tokens"
		prices := bands[band]
		videoItem := customAlibabaVideoItem.FindStringSubmatch(item.Type)
		switch {
		case videoItem != nil && unit == customAlibabaPerSecondUnit && band == "standard" && video[strings.ToUpper(videoItem[1])] == "":
			video[strings.ToUpper(videoItem[1])] = price
		case item.Type == "input_token" && perM && prices.input == "":
			prices.input = price
		case item.Type == "output_token" && perM && prices.output == "":
			prices.output = price
		case item.Type == "input_token_cache" && perM && prices.cacheImplicit == "":
			prices.cacheImplicit = price
		case item.Type == "input_token_cache_read" && perM && prices.cacheRead == "":
			prices.cacheRead = price
		case item.Type == "input_token_cache_creation_5m" && perM && prices.cacheCreate == "":
			prices.cacheCreate = price
		case (item.Type == "input_token_batch" || item.Type == "output_token_batch") && perM:
		case item.Type == "image_number" && unit == customAlibabaPerImageUnit && band == "standard" && image == "":
			image = price
		default:
			return customAlibabaTierPrice{}, fmt.Sprintf("unsupported price item %s (%s, %s)", item.Type, item.PriceUnit, band)
		}
	}
	body := func(prices *bandPrices) string {
		if prices.input == "" || prices.output == "" {
			return ""
		}
		text := fmt.Sprintf("p * %s + c * %s", prices.input, prices.output)
		cacheHit := prices.cacheImplicit
		if cacheHit == "" {
			cacheHit = prices.cacheRead
		}
		if cacheHit != "" {
			text += " + cr * " + cacheHit
		}
		if prices.cacheCreate != "" {
			text += " + cc * " + prices.cacheCreate
		}
		return text
	}
	standard, peak, offPeak := body(bands["standard"]), body(bands["peak"]), body(bands["off_peak"])
	timed := *bands["peak"] != bandPrices{} || *bands["off_peak"] != bandPrices{}
	switch {
	case len(video) > 0 && (image != "" || *bands["standard"] != bandPrices{} || timed):
		return customAlibabaTierPrice{}, "per-second video price mixed with other prices"
	case len(video) > 0:
		return customAlibabaTierPrice{video: video}, ""
	case image != "" && *bands["standard"] == bandPrices{} && !timed:
		return customAlibabaTierPrice{image: image}, ""
	case image != "":
		return customAlibabaTierPrice{}, "per-image price mixed with token prices"
	case timed && (*bands["standard"] != bandPrices{} || peak == "" || offPeak == ""):
		return customAlibabaTierPrice{}, "time-of-day prices without both a peak and an off-peak input and output price"
	case timed:
		return customAlibabaTierPrice{tokens: peak, offPeak: offPeak}, ""
	case standard == "":
		return customAlibabaTierPrice{}, "no input and output token price"
	}
	return customAlibabaTierPrice{tokens: standard}, ""
}

func customAlibabaPriceString(value any) (string, bool) {
	var parsed float64
	switch typed := value.(type) {
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return "", false
		}
		parsed = number
	case float64:
		parsed = typed
	default:
		return "", false
	}
	if parsed < 0 || parsed != parsed || parsed > 1e6 {
		return "", false
	}
	return strconv.FormatFloat(parsed, 'f', -1, 64), true
}

// customAlibabaRangeLimit reads the upper input-token bound of a tier name such as
// "32k<Input<=128k". "Default" is only valid for an untiered model and is
// unbounded. K is taken as 1024 tokens, matching Model Studio context sizes.
func customAlibabaRangeLimit(rangeName string, single bool) (float64, string, bool) {
	name := strings.TrimSpace(rangeName)
	if single && strings.EqualFold(name, "default") {
		return 0, "base", true
	}
	match := customAlibabaRangeBound.FindStringSubmatch(name)
	if match == nil {
		return 0, "", false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value <= 0 {
		return 0, "", false
	}
	switch strings.ToLower(match[2]) {
	case "k":
		return value * customAlibabaTokensPerK, "le" + match[1] + "k", true
	case "m":
		return value * customAlibabaTokensPerK * customAlibabaTokensPerK, "le" + match[1] + "m", true
	}
	return value, "le" + match[1], true
}
