package controller

import (
	"bytes"
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

var customVendorPricesCache struct {
	sync.Mutex
	fetchedAt time.Time
	prices    map[string]customVendorPrice
	sources   map[string]string
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

// fetchCustomVendorPrices combines models.dev with the vendors' own pricing pages.
// A page price replaces the models.dev one; a page that cannot be read or parsed
// is skipped and reported in sources, so one broken page never hides the rest.
func fetchCustomVendorPrices(ctx context.Context) (map[string]customVendorPrice, map[string]string, error) {
	customVendorPricesCache.Lock()
	defer customVendorPricesCache.Unlock()
	if customVendorPricesCache.prices != nil && time.Since(customVendorPricesCache.fetchedAt) < customVendorPricesCacheTTL {
		return customVendorPricesCache.prices, customVendorPricesCache.sources, nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	sources := make(map[string]string)

	prices := make(map[string]customVendorPrice)
	if body, err := fetchCustomVendorBody(ctx, client, customVendorPricesURL); err != nil {
		sources["models.dev"] = "error: " + err.Error()
	} else if fetched, err := customVendorPricesFromModelsDev(bytes.NewReader(body)); err != nil {
		sources["models.dev"] = "error: " + err.Error()
	} else {
		prices = fetched
		sources["models.dev"] = fmt.Sprintf("ok: %d models", len(fetched))
	}

	type pageResult struct {
		prices map[string]customVendorPrice
		err    error
	}
	results := make([]pageResult, len(customVendorPageSources))
	var wg sync.WaitGroup
	for i, source := range customVendorPageSources {
		wg.Add(1)
		go func(i int, source customVendorPageSource) {
			defer wg.Done()
			body, err := fetchCustomVendorBody(ctx, client, source.URL)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].prices, results[i].err = source.Parse(body)
		}(i, source)
	}
	wg.Wait()
	for i, source := range customVendorPageSources {
		if results[i].err != nil {
			sources[source.Name] = "error: " + results[i].err.Error()
			continue
		}
		for model, price := range results[i].prices {
			prices[model] = price
		}
		sources[source.Name] = fmt.Sprintf("ok: %d models", len(results[i].prices))
	}
	// Alibaba's own models: the tiered list price from Model Studio itself, the same
	// data a channel sync reads, so the two cannot disagree.
	if alibaba, err := fetchCustomAlibabaVendorPrices(ctx, client); err != nil {
		sources["alibaba"] = "error: " + err.Error()
	} else {
		for model, price := range alibaba {
			prices[model] = price
		}
		sources["alibaba"] = fmt.Sprintf("ok: %d models", len(alibaba))
	}
	if len(prices) == 0 {
		return nil, sources, fmt.Errorf("no vendor prices could be loaded")
	}
	customVendorPricesCache.prices = prices
	customVendorPricesCache.sources = sources
	customVendorPricesCache.fetchedAt = time.Now()
	return prices, sources, nil
}

// GetCustomVendorPrices returns each model vendor's own list price, and how each
// source fared.
func GetCustomVendorPrices(c *gin.Context) {
	prices, sources, err := fetchCustomVendorPrices(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "sources": sources})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": prices, "sources": sources})
}
