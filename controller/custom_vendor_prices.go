package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

const (
	customVendorPricesMaxBytes = 32 << 20
)

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
