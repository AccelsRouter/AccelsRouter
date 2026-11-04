package controller

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const customVendorAlibabaProvider = "Alibaba Model Studio (intl)"

// fetchCustomAlibabaVendorPrices reads Model Studio's price list with the key of an
// enabled international Alibaba channel.
func fetchCustomAlibabaVendorPrices(ctx context.Context, client *http.Client) (map[string]customVendorPrice, error) {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		if channel == nil || channel.Status != common.ChannelStatusEnabled || !isCustomAlibabaIntlBaseURL(channel.GetBaseURL()) {
			continue
		}
		key, _, apiErr := channel.GetNextEnabledKey()
		if apiErr != nil || strings.TrimSpace(key) == "" {
			continue
		}
		models, err := fetchCustomAlibabaModels(ctx, client, channel.GetBaseURL(), strings.TrimSpace(key))
		if err != nil {
			return nil, err
		}
		return customAlibabaVendorPrices(models), nil
	}
	return nil, errors.New("no enabled international Alibaba channel")
}

// customAlibabaVendorPrices keeps Alibaba's own (qwen) models with every input-length
// tier. Models whose tiers cannot be read exactly, including those priced by time
// band, are left out rather than guessed.
func customAlibabaVendorPrices(models []customAlibabaModel) map[string]customVendorPrice {
	prices := make(map[string]customVendorPrice)
	for _, item := range models {
		name := strings.TrimSpace(item.Model)
		if name == "" || !strings.HasPrefix(strings.ToLower(item.Provider), "qwen") {
			continue
		}
		tiers, ok := customAlibabaVendorTiers(item.Prices)
		if !ok {
			continue
		}
		first := tiers[0]
		price := customVendorPrice{
			Provider:  customVendorAlibabaProvider,
			Input:     first.Input,
			Output:    first.Output,
			CacheRead: first.CacheRead,
		}
		if len(tiers) > 1 {
			price.Tiers = tiers
		}
		prices[name] = price
	}
	return prices
}

func customAlibabaVendorTiers(tiers []customAlibabaPriceTier) ([]customVendorTier, bool) {
	if len(tiers) == 0 {
		return nil, false
	}
	type boundedTier struct {
		limit float64
		tier  customVendorTier
	}
	bounded := make([]boundedTier, 0, len(tiers))
	for _, tier := range tiers {
		var input, output, cache *float64
		for _, price := range tier.Prices {
			if band := strings.ToLower(strings.TrimSpace(price.TimeBand)); band != "" && band != "standard" {
				return nil, false
			}
			if strings.ToLower(strings.TrimSpace(price.PriceUnit)) != customAlibabaPerMillionUnit {
				continue
			}
			text, valid := customAlibabaPriceString(price.Price)
			if !valid {
				return nil, false
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, false
			}
			switch price.Type {
			case "input_token":
				input = &value
			case "output_token":
				output = &value
			case "input_token_cache":
				cache = &value
			}
		}
		if input == nil || output == nil || *input <= 0 {
			return nil, false
		}
		limit, _, ok := customAlibabaRangeLimit(tier.RangeName, len(tiers) == 1)
		if !ok {
			return nil, false
		}
		bounded = append(bounded, boundedTier{limit, customVendorTier{Input: *input, Output: output, CacheRead: cache}})
	}
	sort.Slice(bounded, func(i, j int) bool { return bounded[i].limit < bounded[j].limit })
	result := make([]customVendorTier, len(bounded))
	for i, item := range bounded {
		result[i] = item.tier
		if i < len(bounded)-1 {
			result[i].Upper = item.limit
		}
	}
	return result, true
}
