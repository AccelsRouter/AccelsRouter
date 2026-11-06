package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// BytePlus split bill pulled over the billing OpenAPI (ListSplitBillDetail,
// grouped per 5-minute settlement window) and compared like an uploaded bill.
// Uses the account AK/SK already configured for BytePlus price sync.

const (
	bytePlusBillingHost     = "open.byteplusapi.com"
	bytePlusBillingService  = "billing"
	bytePlusBillingQuery    = "Action=ListSplitBillDetail&Version=2022-01-01"
	bytePlusBillingPageSize = 300
	bytePlusBillingMaxPages = 100
	// OpenGetApiKeySID resolves an Ark API key to the split item id the bill
	// attributes its usage to (on the Ark control plane, not the billing one).
	bytePlusApiKeySIDQuery = "Action=OpenGetApiKeySID&Version=2024-01-01"
	// GetInferenceUsage is the usage statistics behind the console's usage
	// page (also on the Ark control plane): per-day request and token counts
	// by model and API key, 5-30 minutes behind the calls. The bill has no
	// call counts, so the request counts come from here.
	bytePlusUsageQuery = "Action=GetInferenceUsage&Version=2024-01-01"
)

// bytePlusSplitDimension asks the bill to split every ModelArk product by
// API key, so rows can be filtered to one key's SID.
var bytePlusSplitDimension = func() string {
	products := []string{"ModelArk", "ModelArk_open_source_llm", "ModelArk_video_generation", "ModelArk_managed_agents", "Smart_Drawing_T2I", "hitem_third_party", "rodin_third_party"}
	m := make(map[string]string, len(products))
	for _, p := range products {
		m[p] = "apikey_id"
	}
	s, _ := common.Marshal(m)
	return string(s)
}()

// bytePlusSplitBillBody is one page of the split-bill query: settlement
// rows in 5-minute windows, filtered to one API key's split item when
// splitItemID is set.
func bytePlusSplitBillBody(period string, page int, splitItemID string) map[string]any {
	body := map[string]any{
		"BillPeriod":    period,
		"Limit":         bytePlusBillingPageSize,
		"Offset":        page * bytePlusBillingPageSize,
		"GroupTerm":     1, // per settlement row, not merged
		"GroupPeriod":   2, // settlement detail: 5-minute ExpenseTime windows
		"NeedRecordNum": 1,
	}
	if splitItemID != "" {
		body["SplitDimension"] = bytePlusSplitDimension
		body["SplitItemID"] = splitItemID
	}
	return body
}

// bytePlusApiKeySID resolves an Ark API key (as a channel stores it) to the
// split item id ("apikey-...") the bill attributes its usage to.
func bytePlusApiKeySID(ctx context.Context, client *http.Client, accessKey, secretKey, apiKey string) (string, error) {
	body, err := common.Marshal(map[string]string{"ApiKey": apiKey})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+customBytePlusPricingHost+"/?"+bytePlusApiKeySIDQuery, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	signBytePlusOpenAPIRequest(req, body, accessKey, secretKey, time.Now(), customBytePlusPricingHost, customBytePlusPricingService, bytePlusApiKeySIDQuery)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	var decoded struct {
		ResponseMetadata struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result struct {
			SID string `json:"SID"`
		} `json:"Result"`
	}
	if err := common.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("%s", resp.Status)
	}
	if apiErr := decoded.ResponseMetadata.Error; apiErr != nil {
		return "", fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
	}
	if decoded.Result.SID == "" {
		return "", fmt.Errorf("%s: no SID in the response", resp.Status)
	}
	return decoded.Result.SID, nil
}

// bytePlusUsageBody asks the usage statistics for the period's days up to
// lastDay (the last day the bill covers, so the near-real-time usage does
// not run past the T+1 bill), per model, filtered to the API key ids when
// given: the bill's split item ids are those same ids.
func bytePlusUsageBody(period, lastDay string, apiKeyIDs []string) map[string]any {
	filters := []map[string]any{{"Key": "ModelName", "Values": []string{}}}
	if len(apiKeyIDs) > 0 {
		filters = append(filters, map[string]any{"Key": "ApikeyID", "Values": apiKeyIDs})
	}
	return map[string]any{"StartTime": period + "-01", "EndTime": lastDay, "QueryInterval": "Day", "Filters": filters}
}

// fetchBytePlusInferenceUsage returns how many requests the usage statistics
// count per model (as the usage names it) for the period's days up to
// lastDay. The response is a table: Fields names the columns, Data holds
// the rows as strings.
func fetchBytePlusInferenceUsage(ctx context.Context, client *http.Client, accessKey, secretKey, period, lastDay string, apiKeyIDs []string) (map[string]int64, error) {
	body, err := common.Marshal(bytePlusUsageBody(period, lastDay, apiKeyIDs))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+customBytePlusPricingHost+"/?"+bytePlusUsageQuery, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	signBytePlusOpenAPIRequest(req, body, accessKey, secretKey, time.Now(), customBytePlusPricingHost, customBytePlusPricingService, bytePlusUsageQuery)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var decoded struct {
		ResponseMetadata struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result struct {
			Fields []struct {
				Name string `json:"Name"`
			} `json:"Fields"`
			Data [][]string `json:"Data"`
		} `json:"Result"`
	}
	if err := common.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	if apiErr := decoded.ResponseMetadata.Error; apiErr != nil {
		return nil, fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	modelCol, requestsCol := -1, -1
	for i, f := range decoded.Result.Fields {
		switch f.Name {
		case "ModelName":
			modelCol = i
		case "ReqCnt":
			requestsCol = i
		}
	}
	if modelCol < 0 || requestsCol < 0 {
		return nil, errors.New("the usage has no ModelName / ReqCnt columns")
	}
	requests := map[string]int64{}
	for i, row := range decoded.Result.Data {
		if len(row) <= max(modelCol, requestsCol) {
			return nil, fmt.Errorf("row %d: %d columns", i+1, len(row))
		}
		n, err := strconv.ParseInt(strings.TrimSpace(row[requestsCol]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: ReqCnt %q", i+1, row[requestsCol])
		}
		requests[strings.TrimSpace(row[modelCol])] += n
	}
	return requests, nil
}

type bytePlusBillRow struct {
	ExpenseTime    string `json:"ExpenseTime"`  // "2026-10-08 10:15:00-2026-10-08 10:20:00"
	BillCategory   string `json:"BillCategory"` // "Consumption-usage" / "Consumption-purchase" (resource packs)
	Product        string `json:"Product"`      // "ModelArk", "ModelArk_open_source_llm", "TOS", ...
	ConfigName     string `json:"ConfigName"`
	ChargeItemCode string `json:"ChargeItemCode"`
	Count          string `json:"Count"`
	Unit           string `json:"Unit"`
	// OriginalBillAmount is the list price; DiscountBillAmount the amount
	// after the account's contract discount, before coupons and tax;
	// PayableAmount what is actually charged after coupons, with tax.
	OriginalBillAmount string `json:"OriginalBillAmount"`
	DiscountBillAmount string `json:"DiscountBillAmount"`
	PayableAmount      string `json:"PayableAmount"`
}

type bytePlusBillPage struct {
	ResponseMetadata struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result struct {
		List  []bytePlusBillRow `json:"List"`
		Total int               `json:"Total"`
	} `json:"Result"`
}

var bytePlusBillPeriodRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func fetchBytePlusSplitBill(ctx context.Context, client *http.Client, accessKey, secretKey, period, splitItemID string) ([]bytePlusBillRow, error) {
	var rows []bytePlusBillRow
	for page := range bytePlusBillingMaxPages {
		body, err := common.Marshal(bytePlusSplitBillBody(period, page, splitItemID))
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+bytePlusBillingHost+"/?"+bytePlusBillingQuery, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		signBytePlusOpenAPIRequest(req, body, accessKey, secretKey, time.Now(), bytePlusBillingHost, bytePlusBillingService, bytePlusBillingQuery)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var decoded bytePlusBillPage
		if err := common.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		if apiErr := decoded.ResponseMetadata.Error; apiErr != nil {
			return nil, fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		rows = append(rows, decoded.Result.List...)
		if len(decoded.Result.List) < bytePlusBillingPageSize || len(rows) >= decoded.Result.Total {
			return rows, nil
		}
	}
	return nil, fmt.Errorf("the bill has more than %d rows", bytePlusBillingMaxPages*bytePlusBillingPageSize)
}

// bytePlusBillEntries turns usage settlement rows into bill entries: one
// entry per row at the window's start, tokens classified by the charge item
// code (prompt / completion / KV-cache hit), the count scaled by its unit,
// the cost = the billed amount after contract discount (USD) and the payable
// = what BytePlus actually charges after coupons. KV-cache-hit tokens are
// billed apart from the prompt but are part of it, so they count towards
// the prompt tokens too, as in our records. Rows with a non-token unit
// (images, seconds) keep the count as a usage amount. Resource-pack
// purchases and object storage (TOS) are not model usage and are skipped.
func bytePlusBillEntries(rows []bytePlusBillRow) ([]model.UpstreamBillEntry, error) {
	entries := make([]model.UpstreamBillEntry, 0, len(rows))
	for i, r := range rows {
		if (r.BillCategory != "" && r.BillCategory != "Consumption-usage") || r.Product == "TOS" {
			continue
		}
		// "<start>-<end>", both "2006-01-02 15:04:05": the window start is the
		// first 19 characters. (Splitting on "-" would break on days 20-29.)
		const stampLen = len("2006-01-02 15:04:05")
		var wall time.Time
		var err error
		if len(r.ExpenseTime) >= stampLen {
			wall, err = time.ParseInLocation("2006-01-02 15:04:05", r.ExpenseTime[:stampLen], time.UTC)
		}
		if err != nil || len(r.ExpenseTime) < stampLen {
			return nil, fmt.Errorf("row %d: unrecognized ExpenseTime %q", i+1, r.ExpenseTime)
		}
		count, err := strconv.ParseFloat(strings.ReplaceAll(r.Count, ",", ""), 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: Count %q", i+1, r.Count)
		}
		payable, err := bytePlusAmount(r.PayableAmount)
		if err != nil {
			return nil, fmt.Errorf("row %d: PayableAmount %q", i+1, r.PayableAmount)
		}
		billed := r.DiscountBillAmount
		if strings.TrimSpace(billed) == "" {
			billed = r.OriginalBillAmount
		}
		cost, err := bytePlusAmount(billed)
		if err != nil {
			return nil, fmt.Errorf("row %d: bill amount %q", i+1, billed)
		}
		e := model.UpstreamBillEntry{Wall: wall.Unix(), Model: strings.TrimSpace(r.ConfigName), Cost: cost, Payable: payable}
		unit := strings.ToLower(strings.TrimSpace(r.Unit))
		if strings.Contains(unit, "token") {
			scale := 1.0
			switch {
			case strings.HasPrefix(unit, "k "), strings.HasPrefix(unit, "thousand"):
				scale = 1e3
			case strings.HasPrefix(unit, "m "), strings.HasPrefix(unit, "million"):
				scale = 1e6
			}
			tokens := int64(count*scale + 0.5)
			code := strings.ToLower(r.ChargeItemCode)
			switch {
			case strings.Contains(code, "cache"):
				e.CacheReadTokens = tokens
				e.PromptTokens = tokens
			case strings.Contains(code, "completion"), strings.Contains(code, "output"):
				e.CompletionTokens = tokens
			default:
				e.PromptTokens = tokens
			}
		} else {
			e.UsageUnit = strings.TrimSpace(r.Unit)
			e.UsageAmount = int64(count + 0.5)
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, errors.New("the bill has no rows for this period")
	}
	return entries, nil
}

// bytePlusAmount reads a bill amount ("0.2786180000", "1,234.5"); an empty
// field counts as zero.
func bytePlusAmount(s string) (float64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// bytePlusModelVersionRe is the "-YYMMDD" version of an endpoint model id
// ("deepseek-v4-1-flash-261017"); the bill names the product without it.
var bytePlusModelVersionRe = regexp.MustCompile(`-\d{6}$`)

// bytePlusModelKey matches BytePlus's model naming ("DeepSeek-V4.1-flash",
// "GLM_5.2") with ours ("deepseek-v4-1-flash-261017"): lower-case, '.' and
// '_' as '-', without the "-YYMMDD" version.
func bytePlusModelKey(name string) string {
	key := strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(strings.ToLower(strings.TrimSpace(name)))
	return bytePlusModelVersionRe.ReplaceAllString(key, "")
}

// AdminCompareBytePlusBill — POST /api/admin/reconciliation/byteplus-bill
// {channel_id, period "YYYY-MM", scope "channel_key" (default) | "account",
// bill_tz_offset?}. With the channel_key scope the bill is filtered to the
// usage attributed to the channel's own API key(s); the account scope takes
// everything the account was billed for.
func AdminCompareBytePlusBill(c *gin.Context) {
	var req struct {
		ChannelId    int    `json:"channel_id"`
		Period       string `json:"period"`
		Scope        string `json:"scope"`
		BillTzOffset *int   `json:"bill_tz_offset"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ChannelId <= 0 {
		common.ApiErrorMsg(c, "Select the channel the bill belongs to")
		return
	}
	if !bytePlusBillPeriodRe.MatchString(req.Period) {
		common.ApiErrorMsg(c, "Billing period must be YYYY-MM")
		return
	}
	if req.BillTzOffset != nil && (*req.BillTzOffset < -14*60 || *req.BillTzOffset > 14*60) {
		common.ApiErrorMsg(c, "Invalid bill time zone")
		return
	}
	if req.Scope == "" {
		req.Scope = "channel_key"
	}
	if req.Scope != "channel_key" && req.Scope != "account" {
		common.ApiErrorMsg(c, "Bill scope must be channel_key or account")
		return
	}
	common.OptionMapRWMutex.RLock()
	accessKey := strings.TrimSpace(common.OptionMap[customBytePlusAccessKeyOption])
	secretKey := strings.TrimSpace(common.OptionMap[customBytePlusSecretKeyOption])
	common.OptionMapRWMutex.RUnlock()
	if accessKey == "" || secretKey == "" {
		common.ApiErrorMsg(c, "Set the BytePlus access key ID and secret key under Settings > Models > BytePlus pricing first")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second}
	var splitItems []string
	var err error
	if req.Scope == "channel_key" {
		channel, err := model.GetChannelById(req.ChannelId, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		keys := channel.GetKeys()
		if len(keys) == 0 {
			common.ApiErrorMsg(c, "The channel has no API key to filter the bill by")
			return
		}
		seen := map[string]bool{}
		for _, key := range keys {
			sid, err := bytePlusApiKeySID(ctx, client, accessKey, secretKey, strings.TrimSpace(key))
			if err != nil {
				common.ApiErrorMsg(c, "BytePlus could not resolve the channel's API key in this account ("+err.Error()+"); compare the whole account instead")
				return
			}
			if !seen[sid] {
				seen[sid] = true
				splitItems = append(splitItems, sid)
			}
		}
	}
	var rows []bytePlusBillRow
	if len(splitItems) == 0 {
		rows, err = fetchBytePlusSplitBill(ctx, client, accessKey, secretKey, req.Period, "")
	} else {
		for _, sid := range splitItems {
			var keyRows []bytePlusBillRow
			if keyRows, err = fetchBytePlusSplitBill(ctx, client, accessKey, secretKey, req.Period, sid); err != nil {
				break
			}
			rows = append(rows, keyRows...)
		}
	}
	if err != nil {
		common.ApiErrorMsg(c, "BytePlus billing API: "+err.Error())
		return
	}
	entries, err := bytePlusBillEntries(rows)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_, adminOffset := time.Now().In(reconLocation(c)).Zone()
	report, err := model.CompareUpstreamBill(req.ChannelId, entries, req.BillTzOffset, adminOffset/60, bytePlusModelKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	report.BillPayableKnown = true
	report.BillScope = req.Scope
	report.BillSplitItems = splitItems
	report.BillSource = "byteplus"
	// The bill counts no calls; the usage statistics do. A failure there
	// (a key without the usage permission) leaves the comparison without
	// request counts rather than without a comparison.
	var lastWall int64
	for _, e := range entries {
		lastWall = max(lastWall, e.Wall)
	}
	lastDay := time.Unix(lastWall, 0).UTC().Format("2006-01-02")
	if usage, err := fetchBytePlusInferenceUsage(ctx, client, accessKey, secretKey, req.Period, lastDay, splitItems); err != nil {
		report.UsageError = "BytePlus usage API: " + err.Error()
	} else {
		report.SetBillRequests(usage, bytePlusModelKey)
	}
	common.ApiSuccess(c, report)
}
