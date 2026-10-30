package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// fetchCustomPricingSync serves the price-sync sources that have their own
// vendor API instead of a new-api style /api/pricing: BytePlus ModelArk and
// the international Alibaba Model Studio. handled is false for every other
// channel, which then follows the stock sync path.
func fetchCustomPricingSync(ctx context.Context, client *http.Client, upstream dto.UpstreamDTO) (data map[string]any, handled bool, err error) {
	if upstream.ID == 0 {
		return nil, false, nil
	}
	switch {
	case isCustomBytePlusBaseURL(upstream.BaseURL):
		common.OptionMapRWMutex.RLock()
		accessKey := strings.TrimSpace(common.OptionMap[customBytePlusAccessKeyOption])
		secretKey := strings.TrimSpace(common.OptionMap[customBytePlusSecretKeyOption])
		common.OptionMapRWMutex.RUnlock()
		if accessKey == "" || secretKey == "" {
			return nil, true, errors.New("BytePlus pricing sync needs the access key ID and secret key set under Settings > Models > BytePlus pricing")
		}
		prices, err := fetchCustomBytePlusModelPrices(ctx, client, "https://"+customBytePlusPricingHost, accessKey, secretKey)
		if err != nil {
			return nil, true, err
		}
		return customPricingSyncResult(convertCustomBytePlusPricesToBillingExpr(prices))
	case isCustomAlibabaIntlBaseURL(upstream.BaseURL):
		channel, err := model.GetChannelById(upstream.ID, true)
		if err != nil {
			return nil, true, errors.New("failed to get channel key: " + err.Error())
		}
		key, _, apiErr := channel.GetNextEnabledKey()
		if apiErr != nil || strings.TrimSpace(key) == "" {
			return nil, true, errors.New("no enabled API key configured for this channel")
		}
		models, err := fetchCustomAlibabaModels(ctx, client, upstream.BaseURL, strings.TrimSpace(key))
		if err != nil {
			return nil, true, err
		}
		return customPricingSyncResult(convertCustomAlibabaModelsToBillingExpr(models))
	}
	return nil, false, nil
}

func customPricingSyncResult(converted map[string]any) (map[string]any, bool, error) {
	if converted == nil {
		return nil, true, errors.New("no supported pricing entries found")
	}
	return converted, true, nil
}
