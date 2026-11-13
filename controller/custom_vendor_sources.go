package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	customVendorGoogleProvider = "Google AI pricing page"
	customVendorOpenAIProvider = "OpenAI list price (OpenRouter)"
)

var (
	customGoogleTag       = regexp.MustCompile(`\{%[^%]*%\}`)
	customGoogleModelID   = regexp.MustCompile("\\*\\[`([^`]+)`\\]")
	customGoogleAmount    = regexp.MustCompile(`\$([0-9][0-9,]*(?:\.[0-9]+)?)`)
	customGoogleModality  = regexp.MustCompile(`^\$[0-9.,]+\s*\(([^)]*)\)`)
	customGoogleEffective = regexp.MustCompile(`(through|starting) ([A-Z][a-z]+ [0-9]{1,2}, [0-9]{4})`)
	customGoogleNonToken  = regexp.MustCompile(`(?i)/min|\((?:audio|images?|video)\)|per image`)
)

// customGoogleAmounts is the prices one cell states for one kind of token: a
// single price, or one for prompts up to Upper tokens and one above.
type customGoogleAmounts struct {
	Base  float64
	Upper float64
	Above float64
}

// customGoogleCellPrices reads a "Paid Tier" cell of the Gemini pricing page.
// A cell lists one price per condition: a modality in parentheses, a prompt
// length ("prompts <= 200k tokens"), and an effective date ("through November
// 30, 2026" / "starting December 1, 2026"). Prices not per token (storage per
// hour, per image, per minute) are left out, as is everything after
// "Equivalent to". It returns the text price and the audio price in effect on
// day; ok is false when the cell cannot be read exactly.
func customGoogleCellPrices(cell string, day time.Time) (text, audio *customGoogleAmounts, ok bool) {
	cell = strings.Join(strings.Fields(strings.ReplaceAll(customGoogleTag.ReplaceAllString(cell, " "), `\`, " ")), " ")
	if cut := strings.Index(cell, "Equivalent"); cut >= 0 {
		cell = cell[:cut]
	}
	starts := customGoogleAmount.FindAllStringSubmatchIndex(cell, -1)
	for i, match := range starts {
		end := len(cell)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		clause := cell[match[0]:end]
		if strings.Contains(clause, " per ") || strings.Contains(clause, "/ 1,000") || strings.Contains(clause, "/min") {
			continue
		}
		amount, err := strconv.ParseFloat(strings.ReplaceAll(cell[match[2]:match[3]], ",", ""), 64)
		if err != nil {
			return nil, nil, false
		}
		in := true
		for _, effective := range customGoogleEffective.FindAllStringSubmatch(clause, -1) {
			date, err := time.Parse("January 2, 2006", effective[2])
			if err != nil {
				return nil, nil, false
			}
			if (effective[1] == "through" && day.After(date)) || (effective[1] == "starting" && day.Before(date)) {
				in = false
			}
		}
		if !in {
			continue
		}
		target := &text
		if label := customGoogleModality.FindStringSubmatch(clause); label != nil && !strings.Contains(strings.ToLower(label[1]), "text") {
			if !strings.Contains(strings.ToLower(label[1]), "audio") {
				return nil, nil, false
			}
			target = &audio
		}
		if *target == nil {
			*target = &customGoogleAmounts{}
		}
		amounts := *target
		below := customTierBelow.FindStringSubmatch(clause)
		above := customTierAbove.FindStringSubmatch(clause)
		switch {
		case !strings.Contains(clause, "prompt"):
			if amounts.Base != 0 || amounts.Upper != 0 {
				return nil, nil, false
			}
			amounts.Base = amount
		case below != nil:
			upper, valid := customTierTokens(below)
			if !valid || amounts.Base != 0 {
				return nil, nil, false
			}
			amounts.Base, amounts.Upper = amount, upper
		case above != nil:
			if amounts.Above != 0 {
				return nil, nil, false
			}
			amounts.Above = amount
		default:
			return nil, nil, false
		}
	}
	for _, amounts := range []*customGoogleAmounts{text, audio} {
		if amounts != nil && (amounts.Base <= 0 || (amounts.Upper > 0) != (amounts.Above > 0)) {
			return nil, nil, false
		}
	}
	return text, audio, true
}

// parseCustomGooglePricing reads the Gemini Developer API pricing page: one
// section per model, whose first table is the standard paid tier. Only token
// prices that can be read exactly are kept: text input, output, context
// caching, prompt-length tiers and a separate audio input price. Models
// priced per minute, per image or by output modality (live, transcription,
// TTS, image generation) are left out. Prices with an effective date follow
// the Pacific date of now.
func parseCustomGooglePricing(page []byte, now time.Time) (map[string]customVendorPrice, error) {
	pacific := now.UTC().Add(-8 * time.Hour)
	day := time.Date(pacific.Year(), pacific.Month(), pacific.Day(), 0, 0, 0, 0, time.UTC)
	prices := make(map[string]customVendorPrice)
	for section := range strings.SplitSeq("\n"+string(page), "\n## ") {
		head, rest, found := strings.Cut(section, "{% table %}")
		if !found {
			continue
		}
		ids := customGoogleModelID.FindAllStringSubmatch(head, -1)
		table, _, _ := strings.Cut(rest, "{% /table %}")
		if len(ids) == 0 || !strings.Contains(table, "per 1M tokens") {
			continue
		}
		cells := make(map[string]string)
		for row := range strings.SplitSeq(table, "\n  ---") {
			parts := strings.Split("\n"+row, "\n  * ")
			if len(parts) != 4 {
				continue
			}
			label := strings.ToLower(strings.TrimSpace(parts[1]))
			for _, kind := range []string{"input price", "output price", "context caching price"} {
				if strings.HasPrefix(label, kind) {
					cells[kind] = parts[3]
				}
			}
		}
		if customGoogleNonToken.MatchString(cells["output price"]) || strings.Contains(cells["input price"], "/min") {
			continue
		}
		input, audio, ok := customGoogleCellPrices(cells["input price"], day)
		if !ok || input == nil || (audio != nil && audio.Upper > 0) {
			continue
		}
		output, _, ok := customGoogleCellPrices(cells["output price"], day)
		if !ok || output == nil || (output.Upper > 0 && output.Upper != input.Upper) {
			continue
		}
		cache, _, ok := customGoogleCellPrices(cells["context caching price"], day)
		if !ok || (cache != nil && cache.Upper > 0 && cache.Upper != input.Upper) {
			cache = nil
		}
		base := customVendorTier{Upper: input.Upper, Input: input.Base, Output: &output.Base}
		if cache != nil {
			base.CacheRead = &cache.Base
		}
		if audio != nil {
			base.AudioInput = &audio.Base
		}
		price := customVendorPrice{Provider: customVendorGoogleProvider, Input: base.Input, Output: base.Output, CacheRead: base.CacheRead}
		switch {
		case input.Upper > 0:
			top := base
			top.Upper, top.Input = 0, input.Above
			if output.Upper > 0 {
				top.Output = &output.Above
			}
			if cache != nil && cache.Upper > 0 {
				top.CacheRead = &cache.Above
			}
			price.Tiers = []customVendorTier{base, top}
		case audio != nil:
			price.Tiers = []customVendorTier{base}
		}
		for _, id := range ids {
			if _, taken := prices[id[1]]; !taken {
				prices[id[1]] = price
			}
		}
	}
	return customPageResult(prices)
}

// customOpenRouterPricing is a model's price on OpenRouter, USD per token as
// decimal strings. An override replaces the price for prompts above
// MinPromptTokens.
type customOpenRouterPricing struct {
	Prompt          string                    `json:"prompt"`
	Completion      string                    `json:"completion"`
	InputCacheRead  string                    `json:"input_cache_read"`
	InputCacheWrite string                    `json:"input_cache_write"`
	Audio           string                    `json:"audio"`
	AudioOutput     string                    `json:"audio_output"`
	Image           string                    `json:"image"`
	ImageOutput     string                    `json:"image_output"`
	Request         string                    `json:"request"`
	MinPromptTokens float64                   `json:"min_prompt_tokens"`
	Overrides       []customOpenRouterPricing `json:"overrides"`
}

// customOpenRouterPerMillion converts a USD-per-token string to USD per 1M
// tokens; an absent or unreadable price is nil.
func customOpenRouterPerMillion(value string) *float64 {
	// ParseFloat reads "NaN" and "Inf" as numbers; neither is a price.
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || !validCustomVendorNumber(parsed) {
		return nil
	}
	perMillion, _ := strconv.ParseFloat(strconv.FormatFloat(parsed*1e6, 'f', 6, 64), 64)
	return &perMillion
}

func (p customOpenRouterPricing) tier() (customVendorTier, bool) {
	input, output := customOpenRouterPerMillion(p.Prompt), customOpenRouterPerMillion(p.Completion)
	if input == nil || output == nil || *input <= 0 {
		return customVendorTier{}, false
	}
	tier := customVendorTier{Input: *input, Output: output, CacheRead: customOpenRouterPerMillion(p.InputCacheRead)}
	if write := customOpenRouterPerMillion(p.InputCacheWrite); write != nil && *write > 0 {
		tier.CacheWrite = write
	}
	return tier, true
}

// parseCustomOpenAIPricing reads OpenAI's own models from OpenRouter's model
// list. OpenAI's pricing pages refuse automated clients; OpenRouter lists each
// provider's price per token, with long-prompt prices as overrides. Batch
// variants, the open-weight gpt-oss models (served by other hosts) and models
// with audio, image or per-request prices are left out.
func parseCustomOpenAIPricing(body []byte) (map[string]customVendorPrice, error) {
	var response struct {
		Data []struct {
			ID      string          `json:"id"`
			Pricing json.RawMessage `json:"pricing"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("unreadable model list: %w", err)
	}
	prices := make(map[string]customVendorPrice)
	for _, item := range response.Data {
		name, own := strings.CutPrefix(item.ID, "openai/")
		if !own || strings.Contains(name, ":") || strings.HasPrefix(name, "gpt-oss") {
			continue
		}
		var pricing customOpenRouterPricing
		if err := common.Unmarshal(item.Pricing, &pricing); err != nil {
			continue
		}
		if price, ok := pricing.vendorPrice(); ok {
			prices[name] = price
		}
	}
	return customPageResult(prices)
}

// vendorPrice is an OpenRouter price as a token list price with its
// long-prompt tiers. A price with audio, image or per-request amounts, or with
// overrides that do not rise, yields nothing.
func (p customOpenRouterPricing) vendorPrice() (customVendorPrice, bool) {
	for _, value := range []string{p.Audio, p.AudioOutput, p.Image, p.ImageOutput, p.Request} {
		if amount := customOpenRouterPerMillion(value); amount != nil && *amount > 0 {
			return customVendorPrice{}, false
		}
	}
	base, ok := p.tier()
	if !ok {
		return customVendorPrice{}, false
	}
	tiers := []customVendorTier{base}
	overrides := slices.Clone(p.Overrides)
	sort.Slice(overrides, func(i, j int) bool { return overrides[i].MinPromptTokens < overrides[j].MinPromptTokens })
	for _, override := range overrides {
		tier, valid := override.tier()
		if !valid || override.MinPromptTokens <= tiers[len(tiers)-1].Upper {
			return customVendorPrice{}, false
		}
		tiers[len(tiers)-1].Upper = override.MinPromptTokens
		tiers = append(tiers, tier)
	}
	price := customVendorPrice{Provider: customVendorOpenAIProvider, Input: base.Input, Output: base.Output, CacheRead: base.CacheRead}
	if len(tiers) > 1 || base.CacheWrite != nil {
		price.Tiers = tiers
	}
	return price, true
}

// customOpenRouterModelsURL is OpenRouter's public model list; a model's
// endpoints are listed under it at /<id>/endpoints.
const customOpenRouterModelsURL = "https://openrouter.ai/api/v1/models"

// fetchCustomOpenAIVendorPrices reads OpenAI's list prices from OpenRouter:
// the models of its model list, each at the price of OpenAI's own standard
// endpoint. The list carries one price per model, and that can be another
// host's regional endpoint, a tenth dearer. A model OpenAI itself does not
// serve there keeps the listed price; one whose endpoints cannot be read is
// left out, so the price stored for it stays.
func fetchCustomOpenAIVendorPrices(ctx context.Context, client *http.Client, modelsURL string) (map[string]customVendorPrice, error) {
	body, err := fetchCustomVendorBody(ctx, client, modelsURL)
	if err != nil {
		return nil, err
	}
	prices, err := parseCustomOpenAIPricing(body)
	if err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(prices))
	own := make([]*customVendorPrice, len(names))
	unread := make([]bool, len(names))
	slots := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			defer customRecoverJob("OpenAI vendor price endpoints")
			slots <- struct{}{}
			defer func() { <-slots }()
			var reply struct {
				Data struct {
					Endpoints []struct {
						ProviderName string                  `json:"provider_name"`
						ServiceTier  *string                 `json:"service_tier"`
						Pricing      customOpenRouterPricing `json:"pricing"`
					} `json:"endpoints"`
				} `json:"data"`
			}
			body, err := fetchCustomVendorBody(ctx, client, modelsURL+"/openai/"+name+"/endpoints")
			if err != nil || common.Unmarshal(body, &reply) != nil {
				unread[i] = true
				return
			}
			for _, endpoint := range reply.Data.Endpoints {
				// Flex and priority processing are service tiers at other prices.
				if endpoint.ProviderName != "OpenAI" || (endpoint.ServiceTier != nil && *endpoint.ServiceTier != "") {
					continue
				}
				if price, ok := endpoint.Pricing.vendorPrice(); ok {
					own[i] = &price
				}
			}
		})
	}
	wg.Wait()
	for i, name := range names {
		switch {
		case unread[i]:
			delete(prices, name)
		case own[i] != nil:
			prices[name] = *own[i]
		}
	}
	return customPageResult(prices)
}
