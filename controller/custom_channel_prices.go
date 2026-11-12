package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

// The context key under which FetchUpstreamRatios hands each upstream's price
// list to a func(name string, data map[string]any).
const customUpstreamPriceSinkKey = "custom_upstream_price_sink"

// customChannelVendorSource names the vendor price catalog source that lists
// what a channel pointed at a vendor's own API is charged, or "" for a channel
// of another kind.
func customChannelVendorSource(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.HasSuffix(host, ".bytepluses.com"):
		return "byteplus"
	case strings.HasSuffix(host, ".volces.com"):
		return "volcengine"
	}
	return ""
}

// recordCustomChannelPrices records what every enabled channel charges for
// the models it serves, under the names the upstream knows them by, and
// returns how many prices changed. A channel's own price list is read the way
// the price sync reads it; a model that list leaves out on a channel pointed
// at ByteDance's own API takes the vendor list price. A channel whose list
// cannot be read keeps its recorded prices.
func recordCustomChannelPrices(ctx context.Context) (int, error) {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return 0, err
	}
	var upstreams []dto.UpstreamDTO
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled || !strings.HasPrefix(channel.GetBaseURL(), "http") {
			continue
		}
		upstream := dto.UpstreamDTO{ID: channel.Id, Name: channel.Name, BaseURL: channel.GetBaseURL()}
		if channel.Type == constant.ChannelTypeOpenRouter {
			upstream.Endpoint = "openrouter"
		}
		upstreams = append(upstreams, upstream)
	}
	lists := make(map[string]map[string]any)
	if len(upstreams) > 0 {
		body, err := common.Marshal(map[string]any{"upstreams": upstreams, "timeout": 30})
		if err != nil {
			return 0, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		request.Header.Set("Content-Type", "application/json")
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = request
		c.Set(customUpstreamPriceSinkKey, func(name string, data map[string]any) { lists[name] = data })
		FetchUpstreamRatios(c)
	}
	var catalog []model.CustomVendorPrice
	if err := model.DB.Where("source IN ?", []string{"byteplus", "volcengine"}).Find(&catalog).Error; err != nil {
		return 0, err
	}
	listed := make(map[string]model.CustomVendorPrice, len(catalog))
	for _, row := range catalog {
		listed[row.Source+"\n"+row.ModelName] = row
	}

	now := common.GetTimestamp()
	changed := 0
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		mapping := map[string]string{}
		if text := channel.GetModelMapping(); text != "" && text != "{}" {
			_ = common.UnmarshalJsonStr(text, &mapping)
		}
		list, read := lists[fmt.Sprintf("%s(%d)", channel.Name, channel.Id)]
		source := customChannelVendorSource(channel.GetBaseURL())
		prices := make(map[string]map[string]any)
		for _, served := range channel.GetModels() {
			name := served
			if mapped := mapping[served]; mapped != "" {
				name = mapped
			}
			candidate := modelPricingSyncValues(list, name)
			_, hasRatio := candidate["model_ratio"]
			_, hasPrice := candidate["model_price"]
			_, hasExpression := candidate[billing_setting.BillingExprField]
			if hasRatio || hasPrice || hasExpression {
				prices[name] = candidate
				continue
			}
			// ByteDance lists a model family once; a dated snapshot costs the same.
			row, found := listed[source+"\n"+name]
			if !found {
				row, found = listed[source+"\n"+customArkSnapshotDate.ReplaceAllString(name, "")]
			}
			if !found {
				continue
			}
			if expr, ok := customVendorSyncExpr(name, customVendorPriceFromRow(row)); ok {
				prices[name] = map[string]any{
					billing_setting.BillingModeField: billing_setting.BillingModeTieredExpr,
					billing_setting.BillingExprField: expr,
				}
			}
		}
		if len(prices) == 0 {
			continue
		}
		// Only a list that was read says a model is gone from it.
		added, err := model.RecordCustomChannelPrices(channel.Id, prices, read, now)
		if err != nil {
			return changed, err
		}
		changed += added
	}
	return changed, nil
}
