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
// input, in tokens, the tier covers; 0 means it has no upper bound. A price
// that needs CacheWrite or AudioInput carries them on its tiers, a single
// unbounded tier when the vendor has no length tiers. Band marks the tiers of
// a vendor that prices by time of day instead ("peak" first, then "off_peak").
//
// A tier with Usage prices a task model instead of tokens: USD per unit of each
// usage fact the model's task plugin reports (per 1M for a token fact), for
// requests whose facts equal When. "input_images_after_first" prices every
// input image but the first.
type customVendorTier struct {
	Upper      float64            `json:"upper"`
	Input      float64            `json:"input"`
	Output     *float64           `json:"output"`
	CacheRead  *float64           `json:"cache_read"`
	CacheWrite *float64           `json:"cache_write,omitempty"`
	AudioInput *float64           `json:"audio_input,omitempty"`
	ImageInput *float64           `json:"image_input,omitempty"`
	Band       string             `json:"band,omitempty"`
	Usage      map[string]float64 `json:"usage,omitempty"`
	When       map[string]string  `json:"when,omitempty"`
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
