package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	customAlibabaPerMillionUnit = "per million tokens"
	customAlibabaPerImageUnit   = "per image"
)

var customAlibabaRangeBound = regexp.MustCompile(`^(?:\d+(?:\.\d+)?[kK]?\s*<\s*)?(?:Input|Token|Tokens)\s*<=\s*(\d+(?:\.\d+)?)\s*([kKmM]?)$`)

type customAlibabaPriceItem struct {
	Type      string `json:"type"`
	Price     any    `json:"price"`
	PriceUnit string `json:"price_unit"`
}

type customAlibabaPriceTier struct {
	RangeName string                   `json:"range_name"`
	Prices    []customAlibabaPriceItem `json:"prices"`
}

type customAlibabaModel struct {
	Model  string                   `json:"model"`
	Prices []customAlibabaPriceTier `json:"prices"`
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

// convertCustomAlibabaModelsToBillingExpr turns Model Studio USD price lists into
// token billing expressions. A model is skipped, never guessed, when any tier
// carries a billing item or range this converter does not understand.
func convertCustomAlibabaModelsToBillingExpr(models []customAlibabaModel) map[string]any {
	modes := make(map[string]string)
	exprs := make(map[string]string)
	for _, item := range models {
		name := strings.TrimSpace(item.Model)
		if name == "" {
			continue
		}
		expr, ok := customAlibabaBillingExpr(item.Prices)
		if !ok {
			continue
		}
		if _, err := billingexpr.CompileFromCache(expr); err != nil {
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
	}
}

func customAlibabaBillingExpr(tiers []customAlibabaPriceTier) (string, bool) {
	if len(tiers) == 0 {
		return "", false
	}
	type boundedTier struct {
		limit float64
		label string
		body  string
	}
	bounded := make([]boundedTier, 0, len(tiers))
	for _, tier := range tiers {
		body, image, ok := customAlibabaTierBody(tier.Prices)
		if !ok {
			return "", false
		}
		if image {
			if len(tiers) != 1 {
				return "", false
			}
			return fmt.Sprintf(`tier("image", fixed(%s)) * image_count`, body), true
		}
		limit, label, ok := customAlibabaRangeLimit(tier.RangeName, len(tiers) == 1)
		if !ok {
			return "", false
		}
		bounded = append(bounded, boundedTier{limit, label, fmt.Sprintf(`tier("%s", %s)`, label, body)})
	}
	sort.Slice(bounded, func(i, j int) bool { return bounded[i].limit < bounded[j].limit })
	expr := bounded[len(bounded)-1].body
	for i := len(bounded) - 2; i >= 0; i-- {
		expr = fmt.Sprintf("len <= %d ? %s : %s", int64(bounded[i].limit), bounded[i].body, expr)
	}
	return expr, true
}

// customAlibabaTierBody renders one tier. It accepts exactly one input_token and one
// output_token price per million tokens, or exactly one image_number price
// per image; anything else makes the whole model unsupported.
func customAlibabaTierBody(items []customAlibabaPriceItem) (body string, image bool, ok bool) {
	var input, output, imagePrice string
	for _, item := range items {
		price, valid := customAlibabaPriceString(item.Price)
		if !valid {
			return "", false, false
		}
		switch {
		case item.Type == "input_token" && item.PriceUnit == customAlibabaPerMillionUnit && input == "":
			input = price
		case item.Type == "output_token" && item.PriceUnit == customAlibabaPerMillionUnit && output == "":
			output = price
		case item.Type == "image_number" && item.PriceUnit == customAlibabaPerImageUnit && imagePrice == "":
			imagePrice = price
		default:
			return "", false, false
		}
	}
	if imagePrice != "" && input == "" && output == "" {
		return imagePrice, true, true
	}
	if input == "" || output == "" || imagePrice != "" {
		return "", false, false
	}
	return fmt.Sprintf("p * %s + c * %s", input, output), false, true
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
