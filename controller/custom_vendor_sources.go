package controller

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed < 0 {
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
		other := false
		for _, value := range []string{pricing.Audio, pricing.AudioOutput, pricing.Image, pricing.ImageOutput, pricing.Request} {
			if amount := customOpenRouterPerMillion(value); amount != nil && *amount > 0 {
				other = true
			}
		}
		base, ok := pricing.tier()
		if other || !ok {
			continue
		}
		tiers := []customVendorTier{base}
		sort.Slice(pricing.Overrides, func(i, j int) bool {
			return pricing.Overrides[i].MinPromptTokens < pricing.Overrides[j].MinPromptTokens
		})
		for _, override := range pricing.Overrides {
			tier, valid := override.tier()
			if !valid || override.MinPromptTokens <= tiers[len(tiers)-1].Upper {
				ok = false
				break
			}
			tiers[len(tiers)-1].Upper = override.MinPromptTokens
			tiers = append(tiers, tier)
		}
		if !ok {
			continue
		}
		price := customVendorPrice{Provider: customVendorOpenAIProvider, Input: base.Input, Output: base.Output, CacheRead: base.CacheRead}
		if len(tiers) > 1 || base.CacheWrite != nil {
			price.Tiers = tiers
		}
		prices[name] = price
	}
	return customPageResult(prices)
}
