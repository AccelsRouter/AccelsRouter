package controller

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

const (
	customBytePlusPricingHost     = "ark.ap-southeast-1.byteplusapi.com"
	customBytePlusPricingRegion   = "ap-southeast-1"
	customBytePlusPricingService  = "ark"
	customBytePlusPricingQuery    = "Action=ListModelChargeItems&Version=2024-01-01"
	customBytePlusPricingPageSize = 50
	customBytePlusPricingMaxPages = 20
	customBytePlusAccessKeyOption = "BytePlusPricingAccessKeyID"
	customBytePlusSecretKeyOption = "BytePlusPricingSecretKey"
)

type customBytePlusChargeItem struct {
	Type          string  `json:"Type"`
	OriginalPrice float64 `json:"OriginalPrice"`
	UnitCode      string  `json:"UnitCode"`
}

type customBytePlusPriceTier struct {
	MaxPromptTokens int64                      `json:"MaxPromptTokens"`
	ChargeItems     []customBytePlusChargeItem `json:"ChargeItems"`
}

type customBytePlusModelPrice struct {
	FoundationModelName string                     `json:"FoundationModelName"`
	ChargeItems         []customBytePlusChargeItem `json:"ChargeItems"`
	MultiChargeItems    []customBytePlusPriceTier  `json:"MultiChargeItems"`
}

type customBytePlusPricingPage struct {
	ResponseMetadata struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result struct {
		Items      []customBytePlusModelPrice `json:"Items"`
		TotalCount int                        `json:"TotalCount"`
	} `json:"Result"`
}

func isCustomBytePlusBaseURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	return err == nil && strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".bytepluses.com")
}

func customHMACSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

// signCustomBytePlusRequest signs a JSON POST to the ark control plane with an IAM
// access key pair (HMAC-SHA256 over date/region/service).
func signCustomBytePlusRequest(req *http.Request, body []byte, accessKey, secretKey string, now time.Time) {
	now = now.UTC()
	xDate, day := now.Format("20060102T150405Z"), now.Format("20060102")
	bodyHash := sha256.Sum256(body)
	bodyHex := hex.EncodeToString(bodyHash[:])
	signedHeaders := "content-type;host;x-content-sha256;x-date"
	canonical := strings.Join([]string{
		http.MethodPost, "/", customBytePlusPricingQuery,
		"content-type:application/json\nhost:" + customBytePlusPricingHost + "\nx-content-sha256:" + bodyHex + "\nx-date:" + xDate + "\n",
		signedHeaders, bodyHex,
	}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonical))
	scope := day + "/" + customBytePlusPricingRegion + "/" + customBytePlusPricingService + "/request"
	toSign := strings.Join([]string{"HMAC-SHA256", xDate, scope, hex.EncodeToString(canonicalHash[:])}, "\n")
	signingKey := customHMACSHA256(customHMACSHA256(customHMACSHA256(customHMACSHA256([]byte(secretKey), day), customBytePlusPricingRegion), customBytePlusPricingService), "request")
	signature := hex.EncodeToString(customHMACSHA256(signingKey, toSign))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Date", xDate)
	req.Header.Set("X-Content-Sha256", bodyHex)
	req.Header.Set("Authorization", "HMAC-SHA256 Credential="+accessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func fetchCustomBytePlusModelPrices(ctx context.Context, client *http.Client, endpoint, accessKey, secretKey string) ([]customBytePlusModelPrice, error) {
	var models []customBytePlusModelPrice
	for page := 1; page <= customBytePlusPricingMaxPages; page++ {
		body, err := common.Marshal(map[string]any{
			"Fields":     []string{"Base", "Price"},
			"Filter":     map[string]any{},
			"PageNumber": page,
			"PageSize":   customBytePlusPricingPageSize,
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/?"+customBytePlusPricingQuery, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		signCustomBytePlusRequest(req, body, accessKey, secretKey, time.Now())
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var decoded customBytePlusPricingPage
		if err := common.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		if apiErr := decoded.ResponseMetadata.Error; apiErr != nil {
			return nil, fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		models = append(models, decoded.Result.Items...)
		if len(decoded.Result.Items) < customBytePlusPricingPageSize || (decoded.Result.TotalCount > 0 && len(models) >= decoded.Result.TotalCount) {
			break
		}
	}
	return models, nil
}

// convertCustomBytePlusPricesToBillingExpr builds expressions from the published
// list prices (OriginalPrice, USD). Models whose price items are ambiguous or
// not text/image are skipped rather than guessed.
func convertCustomBytePlusPricesToBillingExpr(models []customBytePlusModelPrice) map[string]any {
	modes := make(map[string]string)
	exprs := make(map[string]string)
	for _, item := range models {
		name := strings.TrimSpace(item.FoundationModelName)
		if name == "" {
			continue
		}
		expr, ok := customBytePlusBillingExpr(item)
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

func customBytePlusBillingExpr(item customBytePlusModelPrice) (string, bool) {
	if len(item.MultiChargeItems) > 1 {
		tiers := slices.Clone(item.MultiChargeItems)
		// MaxPromptTokens 0 is the open-ended top tier and sorts last.
		tierLimit := func(tier customBytePlusPriceTier) int64 {
			if tier.MaxPromptTokens == 0 {
				return math.MaxInt64
			}
			return tier.MaxPromptTokens
		}
		slices.SortFunc(tiers, func(a, b customBytePlusPriceTier) int { return cmp.Compare(tierLimit(a), tierLimit(b)) })
		last := len(tiers) - 1
		for i, tier := range tiers {
			if (tier.MaxPromptTokens == 0) != (i == last) {
				return "", false
			}
		}
		expr, ok := customBytePlusTokenBody(tiers[last].ChargeItems)
		if !ok {
			return "", false
		}
		expr = fmt.Sprintf(`tier("top", %s)`, expr)
		for i := last - 1; i >= 0; i-- {
			body, ok := customBytePlusTokenBody(tiers[i].ChargeItems)
			if !ok {
				return "", false
			}
			expr = fmt.Sprintf(`len <= %d ? tier("le%d", %s) : %s`, tiers[i].MaxPromptTokens, tiers[i].MaxPromptTokens, body, expr)
		}
		return expr, true
	}
	if body, ok := customBytePlusTokenBody(item.ChargeItems); ok {
		return fmt.Sprintf(`tier("base", %s)`, body), true
	}
	return customBytePlusImageExpr(item.ChargeItems)
}

// customBytePlusTokenBody prices a tier from its prompt, completion, context-cache
// hit and audio-prompt items; all prices are USD per thousand tokens.
func customBytePlusTokenBody(items []customBytePlusChargeItem) (string, bool) {
	var prompt, completion, hit, audio *float64
	for _, charge := range items {
		if charge.OriginalPrice < 0 || math.IsNaN(charge.OriginalPrice) || !strings.EqualFold(charge.UnitCode, "K tokens") {
			continue
		}
		price := math.Round(charge.OriginalPrice*1000*1e6) / 1e6
		switch charge.Type {
		case "InferencePrompt":
			prompt = &price
		case "InferenceCompletion":
			completion = &price
		case "ContextSessionHit":
			hit = &price
		case "AudioPrompt":
			audio = &price
		}
	}
	if prompt == nil || completion == nil {
		return "", false
	}
	var body strings.Builder
	fmt.Fprintf(&body, "p * %s + c * %s", customFormatPrice(*prompt), customFormatPrice(*completion))
	if hit != nil {
		fmt.Fprintf(&body, " + cr * %s", customFormatPrice(*hit))
	}
	if audio != nil {
		fmt.Fprintf(&body, " + ai * %s", customFormatPrice(*audio))
	}
	return body.String(), true
}

// customBytePlusImageExpr accepts a model whose generation items are all priced per
// piece at one identical price; several different per-piece prices (sizes,
// layers) need request conditions and are left to manual configuration.
func customBytePlusImageExpr(items []customBytePlusChargeItem) (string, bool) {
	var price float64
	found := false
	for _, charge := range items {
		// "Inference" is a placeholder row priced 1 for every model.
		if charge.Type == "Inference" || !strings.EqualFold(charge.UnitCode, "Piece") || charge.OriginalPrice < 0 || math.IsNaN(charge.OriginalPrice) {
			continue
		}
		if !strings.HasSuffix(charge.Type, "Completion") {
			return "", false
		}
		if found && charge.OriginalPrice != price {
			return "", false
		}
		price, found = charge.OriginalPrice, true
	}
	if !found {
		return "", false
	}
	return fmt.Sprintf(`tier("image", fixed(%s)) * image_count`, customFormatPrice(price)), true
}

func customFormatPrice(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
