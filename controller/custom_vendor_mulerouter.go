package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const customMuleRouterProvider = "MuleRouter docs"

type customMuleRouterDocKind uint8

const (
	customMuleRouterKindVideo customMuleRouterDocKind = iota
	customMuleRouterKindImage
	customMuleRouterKindQwenEdit
	customMuleRouterKindChat
)

type customMuleRouterDocSpec struct {
	Model        string
	URL          string
	Kind         customMuleRouterDocKind
	Resolutions  []string
	PromptExtend bool
}

var customMuleRouterPapayaPriceRe = regexp.MustCompile(`(?i)input\s+costs\s+(?:USD|\$)\s*([0-9]+(?:\.[0-9]+)?)\s+per\s+million\s+tokens[^.\n]*?output\s+costs\s+(?:USD|\$)\s*([0-9]+(?:\.[0-9]+)?)\s+per\s+million\s+tokens`)

var customMuleRouterDocPages = []customMuleRouterDocSpec{
	{
		Model:       "berry-1.0",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/231deaf2-201e-437e-a5e3-6bbc3f8f7ae1/berry-1.0/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"480p", "720p", "1080p"},
	},
	{
		Model:       "berry-1.0-pro",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/366ae5e7-b7ae-44b8-8357-eb2f2d3ced50/berry-1.0-pro/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"1080p", "2k", "4k"},
	},
	{
		Model:       "berry-1.0-turbo",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/e02b62ce-6474-4057-90c3-2cd7d47b9613/berry-1.0-turbo/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"480p", "720p", "1080p"},
	},
	{
		Model:       "berry-1.0-pro-turbo",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/1a4cdd33-0b93-4ad1-8505-3625c668f3f6/berry-1.0-pro-turbo/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"1080p", "2k", "4k"},
	},
	{
		Model:       "wan2.7-i2v-spicy",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/e397bac6-a072-4eaa-b2e3-79d92742b095/wan2.7-i2v-spicy/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"720p", "1080p"},
	},
	{
		Model:        "wan2.2-i2v-spicy",
		URL:          "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/c5b796fe-3a0d-4e8e-9942-c45da4edf224/wan2.2-i2v-spicy/generation.md",
		Kind:         customMuleRouterKindVideo,
		Resolutions:  []string{"480p", "720p"},
		PromptExtend: true,
	},
	{
		Model:       "w2.2-i2v-lora",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/6a4f2839-00df-4dba-ba13-c8630d1868e3/w2.2-i2v-lora/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"480p", "720p"},
	},
	{
		Model:       "wan-animate",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/214cd04e-6d65-4adc-99c5-96d952c6885b/wan-animate/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"480p", "720p"},
	},
	{
		Model:       "flashvsr",
		URL:         "https://www.mulerouter.ai/docs/api-reference/endpoint/mulerouter/flashvsr/generation.md",
		Kind:        customMuleRouterKindVideo,
		Resolutions: []string{"720p", "1080p", "2k", "4k"},
	},
	{
		Model:        "z-image-spicy",
		URL:          "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/06983eef-3ce8-4a6c-8cbf-30398f24d410/z-image-spicy/generation.md",
		Kind:         customMuleRouterKindImage,
		PromptExtend: true,
	},
	{
		Model: "z-image-spicy-pro",
		URL:   "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/ce406ee9-32c2-4945-a0a7-d2a021571c9b/z-image-spicy-pro/generation.md",
		Kind:  customMuleRouterKindImage,
	},
	{
		Model: "qwen-image-edit-spicy",
		URL:   "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/cac64d6a-6378-4d94-976f-dc8ff2ef53b5/qwen-image-edit-spicy/generation.md",
		Kind:  customMuleRouterKindQwenEdit,
	},
	{
		Model: "face-swap",
		URL:   "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/241f77a4-dcca-4d4e-bb60-97db70309ff0/face-swap/generation.md",
		Kind:  customMuleRouterKindImage,
	},
	{
		Model: "head-swap",
		URL:   "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/42d46cd1-bb97-4d16-85e4-c77be08d6115/head-swap/generation.md",
		Kind:  customMuleRouterKindImage,
	},
	{
		Model: "carrothub-papaya-1.0",
		URL:   "https://www.mulerouter.ai/docs/api-reference/endpoint/carrothub/bdf28a03-356f-4182-b0e3-4336450d95c7/carrothub-papaya-1.0/chat.md",
		Kind:  customMuleRouterKindChat,
	},
}

func isCustomMuleRouterHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "mulerouter.ai" || strings.HasSuffix(host, ".mulerouter.ai")
}

func isCustomMuleRouterBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return isCustomMuleRouterHost(parsed.Hostname())
}

func fetchCustomMuleRouterVendorPrices(ctx context.Context, client *http.Client) (map[string]customVendorPrice, error) {
	return fetchCustomMuleRouterPricesFrom(ctx, client, customMuleRouterDocPages)
}

func fetchCustomMuleRouterPricesFrom(ctx context.Context, client *http.Client, specs []customMuleRouterDocSpec) (map[string]customVendorPrice, error) {
	if client == nil {
		client = &http.Client{}
	}
	type result struct {
		price customVendorPrice
		err   error
	}
	results := make([]result, len(specs))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i := range specs {
		spec := specs[i]
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := fetchCustomVendorBody(ctx, client, spec.URL)
			if err != nil {
				results[i].err = fmt.Errorf("mulerouter %s: %w", spec.Model, err)
				return
			}
			price, err := parseCustomMuleRouterDoc(spec, body)
			if err != nil {
				results[i].err = fmt.Errorf("mulerouter %s: %w", spec.Model, err)
				return
			}
			results[i].price = price
		})
	}
	wg.Wait()

	prices := make(map[string]customVendorPrice, len(specs))
	for i, spec := range specs {
		if results[i].err != nil {
			return nil, results[i].err
		}
		prices[spec.Model] = results[i].price
	}
	return prices, nil
}

func customMuleRouterPricingSection(markdown string) (string, bool) {
	lines := strings.Split(markdown, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Pricing" {
			start = i + 1
			continue
		}
		if start >= 0 && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			return strings.Join(lines[start:i], "\n"), true
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n"), true
	}
	return "", false
}

func customMuleRouterResolutionMatch(label string, resolutions []string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(label))
	for _, res := range resolutions {
		if lower == res || strings.HasPrefix(lower, res+" ") || strings.HasPrefix(lower, res+"(") {
			return res, true
		}
	}
	return "", false
}

func parseCustomMuleRouterDoc(spec customMuleRouterDocSpec, page []byte) (customVendorPrice, error) {
	// Mintlify escapes currency signs as "\$" in its Markdown documentation.
	normalized := strings.ReplaceAll(string(page), `\$`, "$")
	if spec.Kind == customMuleRouterKindChat {
		matches := customMuleRouterPapayaPriceRe.FindStringSubmatch(normalized)
		if len(matches) != 3 {
			return customVendorPrice{}, fmt.Errorf("no token pricing statement found")
		}
		input, okIn := customFirstDollar("$" + matches[1])
		output, okOut := customFirstDollar("$" + matches[2])
		if !okIn || !okOut || input <= 0 || output <= 0 {
			return customVendorPrice{}, fmt.Errorf("invalid token prices %q / %q", matches[1], matches[2])
		}
		return customVendorPrice{
			Provider: customMuleRouterProvider,
			Input:    input,
			Output:   &output,
		}, nil
	}

	section, ok := customMuleRouterPricingSection(normalized)
	if !ok {
		return customVendorPrice{}, fmt.Errorf("missing ## Pricing section")
	}
	tables := parseCustomMarkdownTables(section)
	if len(tables) == 0 {
		return customVendorPrice{}, fmt.Errorf("no pricing table found")
	}

	switch spec.Kind {
	case customMuleRouterKindVideo:
		perRes := make(map[string]float64, len(spec.Resolutions))
		var rewritePrice float64
		var hasRewrite bool
		for _, table := range tables {
			for _, row := range table.Rows {
				label := customCell(row, 0)
				lower := strings.ToLower(label)
				price, hasDollar := customFirstDollar(customCell(row, 1))
				if !hasDollar || price <= 0 {
					continue
				}
				if res, matched := customMuleRouterResolutionMatch(label, spec.Resolutions); matched {
					perRes[res] = price
					continue
				}
				if strings.Contains(lower, "prompt rewriting") {
					rewritePrice = price
					hasRewrite = true
				}
			}
		}
		var tiers []customVendorTier
		for _, res := range spec.Resolutions {
			secPrice, exists := perRes[res]
			if !exists {
				return customVendorPrice{}, fmt.Errorf("missing %s rate", res)
			}
			if spec.PromptExtend {
				if !hasRewrite {
					return customVendorPrice{}, fmt.Errorf("missing prompt rewriting rate")
				}
				tiers = append(tiers,
					customVendorTier{
						When:  map[string]string{"resolution": res, "prompt_extend": "false"},
						Usage: map[string]float64{"seconds": secPrice},
					},
					customVendorTier{
						When:  map[string]string{"resolution": res, "prompt_extend": "true"},
						Usage: map[string]float64{"seconds": secPrice, "per_request": rewritePrice},
					},
				)
				continue
			}
			tiers = append(tiers, customVendorTier{
				When:  map[string]string{"resolution": res},
				Usage: map[string]float64{"seconds": secPrice},
			})
		}
		return customVendorPrice{Provider: customMuleRouterProvider, Input: tiers[0].Usage["seconds"], Tiers: tiers}, nil

	case customMuleRouterKindImage:
		var imagePrice, rewritePrice float64
		var hasImage, hasRewrite bool
		for _, table := range tables {
			for _, row := range table.Rows {
				lower := strings.ToLower(customCell(row, 0))
				price, hasDollar := customFirstDollar(customCell(row, 1))
				if !hasDollar || price <= 0 {
					continue
				}
				switch {
				case strings.Contains(lower, "image generation") || strings.Contains(lower, "face swap") || strings.Contains(lower, "head swap"):
					imagePrice = price
					hasImage = true
				case strings.Contains(lower, "prompt rewriting"):
					rewritePrice = price
					hasRewrite = true
				}
			}
		}
		if !hasImage {
			return customVendorPrice{}, fmt.Errorf("missing image generation rate")
		}
		if spec.PromptExtend {
			if !hasRewrite {
				return customVendorPrice{}, fmt.Errorf("missing prompt rewriting rate")
			}
			return customVendorPrice{
				Provider: customMuleRouterProvider,
				Input:    imagePrice,
				Tiers: []customVendorTier{
					{When: map[string]string{"prompt_extend": "false"}, Usage: map[string]float64{"image_count": imagePrice}},
					{When: map[string]string{"prompt_extend": "true"}, Usage: map[string]float64{"image_count": imagePrice, "per_request": rewritePrice}},
				},
			}, nil
		}
		return customVendorPrice{
			Provider: customMuleRouterProvider,
			Input:    imagePrice,
			Tiers: []customVendorTier{
				{Usage: map[string]float64{"image_count": imagePrice}},
			},
		}, nil

	case customMuleRouterKindQwenEdit:
		perCount := make(map[string]float64, 3)
		for _, table := range tables {
			for _, row := range table.Rows {
				lower := strings.ToLower(customCell(row, 0))
				price, hasDollar := customFirstDollar(customCell(row, 1))
				if !hasDollar || price <= 0 {
					continue
				}
				switch {
				case strings.Contains(lower, "1 image"):
					perCount["1"] = price
				case strings.Contains(lower, "2 images"):
					perCount["2"] = price
				case strings.Contains(lower, "3 images"):
					perCount["3"] = price
				}
			}
		}
		var tiers []customVendorTier
		for _, count := range []string{"1", "2", "3"} {
			price, exists := perCount[count]
			if !exists {
				return customVendorPrice{}, fmt.Errorf("missing %s-image rate", count)
			}
			tiers = append(tiers, customVendorTier{
				When:  map[string]string{"input_images": count},
				Usage: map[string]float64{"image_count": price},
			})
		}
		return customVendorPrice{Provider: customMuleRouterProvider, Input: tiers[0].Usage["image_count"], Tiers: tiers}, nil
	}

	return customVendorPrice{}, fmt.Errorf("unsupported pricing format")
}

func customMuleRouterPricingSyncData(ctx context.Context, client *http.Client) (map[string]any, error) {
	prices := make(map[string]customVendorPrice)
	if model.DB != nil {
		var rows []model.CustomVendorPrice
		if err := model.DB.Where("source = ?", "mulerouter").Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			var tiers []customVendorTier
			if row.Tiers != "" {
				if err := common.UnmarshalJsonStr(row.Tiers, &tiers); err != nil {
					continue
				}
			}
			prices[row.ModelName] = customVendorPrice{
				Provider:  row.Provider,
				Input:     row.InputPrice,
				Output:    row.OutputPrice,
				CacheRead: row.CacheReadPrice,
				Tiers:     tiers,
			}
		}
	}
	if len(prices) == 0 {
		var err error
		prices, err = fetchCustomMuleRouterVendorPrices(ctx, client)
		if err != nil {
			return nil, err
		}
	}
	data := make(map[string]any, len(prices))
	for name, price := range prices {
		expr, ok := customVendorSyncExpr(name, price)
		if !ok {
			continue
		}
		data[name] = map[string]any{
			"billing_mode": "expression",
			"billing_expr": expr,
		}
	}
	return data, nil
}
