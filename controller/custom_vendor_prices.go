package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
)

const (
	customVendorPricesURL      = "https://models.dev/api.json"
	customVendorPricesMaxBytes = 32 << 20
)

// customVendorProviders are the models.dev providers that publish their own
// international list prices, in priority order. Resellers and aggregators are left
// out on purpose: the preset sync picks the cheapest listing of every model, which
// is not what the vendor charges. alibaba also resells third-party models, so it
// comes after the vendors that own them.
var customVendorProviders = []string{
	"deepseek", "zai", "moonshotai", "minimax", "openai", "anthropic",
	"google", "xai", "mistral", "alibaba",
}

// customVendorTier is one input-length tier of a vendor price. Upper is the largest
// input, in tokens, the tier covers; 0 means it has no upper bound.
type customVendorTier struct {
	Upper     float64  `json:"upper"`
	Input     float64  `json:"input"`
	Output    *float64 `json:"output"`
	CacheRead *float64 `json:"cache_read"`
}

type customVendorPrice struct {
	Provider  string             `json:"provider"`
	Input     float64            `json:"input"`
	Output    *float64           `json:"output"`
	CacheRead *float64           `json:"cache_read"`
	Tiers     []customVendorTier `json:"tiers,omitempty"`
}

// customVendorPricesFromModelsDev keeps, for every model, the price of the first
// vendor provider that lists a non-zero input price. Costs are USD per 1M tokens.
func customVendorPricesFromModelsDev(reader io.Reader) (map[string]customVendorPrice, error) {
	var upstream map[string]modelsDevProvider
	if err := common.DecodeJson(reader, &upstream); err != nil {
		return nil, fmt.Errorf("failed to decode models.dev response: %w", err)
	}
	prices := make(map[string]customVendorPrice)
	for _, provider := range customVendorProviders {
		for name, item := range upstream[provider].Models {
			if _, taken := prices[name]; taken {
				continue
			}
			candidate, ok := buildModelsDevCandidate(provider, item.Cost)
			if !ok || candidate.Input <= 0 {
				continue
			}
			prices[name] = customVendorPrice{
				Provider:  provider,
				Input:     candidate.Input,
				Output:    candidate.Output,
				CacheRead: candidate.CacheRead,
			}
		}
	}
	if len(prices) == 0 {
		return nil, fmt.Errorf("no vendor prices found in models.dev response")
	}
	return prices, nil
}

func fetchCustomVendorBody(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", customVendorPageUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, customVendorPricesMaxBytes))
}
