package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	customVendorPricesURL      = "https://models.dev/api.json"
	customVendorPricesCacheTTL = 30 * time.Minute
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

type customVendorPrice struct {
	Provider  string   `json:"provider"`
	Input     float64  `json:"input"`
	Output    *float64 `json:"output"`
	CacheRead *float64 `json:"cache_read"`
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

var customVendorPricesCache struct {
	sync.Mutex
	fetchedAt time.Time
	prices    map[string]customVendorPrice
}

func fetchCustomVendorPrices(ctx context.Context) (map[string]customVendorPrice, error) {
	customVendorPricesCache.Lock()
	defer customVendorPricesCache.Unlock()
	if customVendorPricesCache.prices != nil && time.Since(customVendorPricesCache.fetchedAt) < customVendorPricesCacheTTL {
		return customVendorPricesCache.prices, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, customVendorPricesURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev: %s", resp.Status)
	}
	prices, err := customVendorPricesFromModelsDev(io.LimitReader(resp.Body, customVendorPricesMaxBytes))
	if err != nil {
		return nil, err
	}
	customVendorPricesCache.prices = prices
	customVendorPricesCache.fetchedAt = time.Now()
	return prices, nil
}

// GetCustomVendorPrices returns each model vendor's own list price.
func GetCustomVendorPrices(c *gin.Context) {
	prices, err := fetchCustomVendorPrices(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": prices})
}
