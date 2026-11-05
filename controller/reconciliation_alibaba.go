package controller

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Alibaba Cloud Model Studio bill pulled over the Expenses and Costs OpenAPI
// (BssOpenApi DescribeInstanceBill: billable items per day) and compared like
// an uploaded bill. The bill attributes each day's usage of a model to an API
// key id; the channel's keys are resolved to their ids over the Model Studio
// OpenAPI (ListApiKeys). Both use the account's RAM access key pair.

const (
	customAlibabaAccessKeyOption = "AlibabaCloudAccessKeyID"
	customAlibabaSecretKeyOption = "AlibabaCloudAccessKeySecret"

	alibabaBillingAction   = "DescribeInstanceBill"
	alibabaBillingVersion  = "2017-12-14"
	alibabaBillingProduct  = "bailian"
	alibabaBillingPageSize = 300
	alibabaBillingMaxPages = 500

	alibabaApiKeysAction   = "ListApiKeys"
	alibabaApiKeysVersion  = "2026-02-10"
	alibabaApiKeysPath     = "/modelstudio/apikeys"
	alibabaApiKeysPageSize = 100
	alibabaApiKeysMaxPages = 50

	// Alibaba Cloud bills are cut in UTC+8 whatever the site.
	alibabaBillOffsetMinutes = 8 * 60
)

// alibabaCloudRegion is the Model Studio region a DashScope base URL serves:
// the international endpoint is Singapore, "-us" Virginia, the China
// endpoint Beijing. Billing is global per site, so the region also tells
// the billing host apart (China site vs. international).
func alibabaCloudRegion(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	host := ""
	if err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	switch {
	case strings.Contains(host, "dashscope-us"):
		return "us-east-1"
	case host == "dashscope.aliyuncs.com":
		return "cn-beijing"
	default:
		return "ap-southeast-1"
	}
}

// alibabaCanonicalQuery is the V3 canonical query string: parameters sorted
// by name, names and values RFC 3986-encoded (space as %20, '*' as %2A).
func alibabaCanonicalQuery(query url.Values) string {
	encode := func(s string) string {
		var b strings.Builder
		for _, c := range []byte(s) {
			if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
				b.WriteByte(c)
				continue
			}
			fmt.Fprintf(&b, "%%%02X", c)
		}
		return b.String()
	}
	parts := make([]string, 0, len(query))
	for _, name := range slices.Sorted(maps.Keys(query)) {
		for _, v := range query[name] {
			parts = append(parts, encode(name)+"="+encode(v))
		}
	}
	return strings.Join(parts, "&")
}

// alibabaStringToSign builds the V3 string to sign for a request whose
// signed headers are given (host and the x-acs-* ones), with the request's
// query string canonicalized.
func alibabaStringToSign(req *http.Request, headers map[string]string, bodyHex string) (stringToSign, signedHeaders string) {
	names := slices.Sorted(maps.Keys(headers))
	var canonicalHeaders strings.Builder
	for _, n := range names {
		canonicalHeaders.WriteString(n + ":" + strings.TrimSpace(headers[n]) + "\n")
	}
	signedHeaders = strings.Join(names, ";")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, path, alibabaCanonicalQuery(req.URL.Query()), canonicalHeaders.String(), signedHeaders, bodyHex}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonical))
	return "ACS3-HMAC-SHA256\n" + hex.EncodeToString(canonicalHash[:]), signedHeaders
}

// signAlibabaCloudRequest signs an Alibaba Cloud OpenAPI request the V3 way
// (ACS3-HMAC-SHA256): the action and version travel in x-acs-* headers, the
// parameters in the query string (RPC) or the path (ROA).
func signAlibabaCloudRequest(req *http.Request, body []byte, accessKey, secretKey, action, version, nonce string, now time.Time) {
	bodyHash := sha256.Sum256(body)
	bodyHex := hex.EncodeToString(bodyHash[:])
	headers := map[string]string{
		"host":                  req.URL.Host,
		"x-acs-action":          action,
		"x-acs-version":         version,
		"x-acs-date":            now.UTC().Format("2006-01-02T15:04:05Z"),
		"x-acs-signature-nonce": nonce,
		"x-acs-content-sha256":  bodyHex,
	}
	stringToSign, signedHeaders := alibabaStringToSign(req, headers, bodyHex)
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(stringToSign))
	for name, value := range headers {
		if name != "host" {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Authorization", "ACS3-HMAC-SHA256 Credential="+accessKey+",SignedHeaders="+signedHeaders+",Signature="+hex.EncodeToString(mac.Sum(nil)))
}

func alibabaNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// callAlibabaCloud sends a signed GET and decodes the JSON body; an error
// response ({"Code","Message"} or {"code","message"}) becomes an error.
func callAlibabaCloud(ctx context.Context, client *http.Client, rawURL, accessKey, secretKey, action, version string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	signAlibabaCloudRequest(req, nil, accessKey, secretKey, action, version, alibabaNonce(), time.Now())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes))
	resp.Body.Close()
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
			LCode   string `json:"code"`
			LMsg    string `json:"message"`
		}
		if common.Unmarshal(raw, &apiErr) == nil && (apiErr.Code+apiErr.LCode != "") {
			return fmt.Errorf("%s: %s", apiErr.Code+apiErr.LCode, apiErr.Message+apiErr.LMsg)
		}
		return fmt.Errorf("%s", resp.Status)
	}
	if err := common.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: unreadable response", resp.Status)
	}
	return nil
}

type alibabaBillItem struct {
	// InstanceID is "<api key id>;<workspace>;<model>;<input_token|output_token|...>;<channel>;<free-quota flag>"
	// for a Model Studio inference row; trailing empty segments are dropped.
	InstanceID        string  `json:"InstanceID"`
	BillingDate       string  `json:"BillingDate"` // "2026-10-08"
	Item              string  `json:"Item"`        // "PayAsYouGoBill", "SubscriptionOrder", "Refund", "Adjustment"
	SubscriptionType  string  `json:"SubscriptionType"`
	ProductCode       string  `json:"ProductCode"`
	BillingItem       string  `json:"BillingItem"`
	Usage             string  `json:"Usage"`
	UsageUnit         string  `json:"UsageUnit"`
	Currency          string  `json:"Currency"`
	PretaxGrossAmount float64 `json:"PretaxGrossAmount"` // list price
	InvoiceDiscount   float64 `json:"InvoiceDiscount"`   // contract / promotion discount
	PretaxAmount      float64 `json:"PretaxAmount"`      // payable after coupons and free quota, before tax
}

type alibabaBillPage struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
	Success bool   `json:"Success"`
	Data    struct {
		Items     []alibabaBillItem `json:"Items"`
		NextToken string            `json:"NextToken"`
	} `json:"Data"`
}

// fetchAlibabaDailyBill pulls the billable-item bill of every day of the
// period (YYYY-MM), all products: Model Studio rows are told apart when the
// bill is converted.
func fetchAlibabaDailyBill(ctx context.Context, client *http.Client, host, accessKey, secretKey, period string) ([]alibabaBillItem, error) {
	first, err := time.Parse("2006-01", period)
	if err != nil {
		return nil, err
	}
	var items []alibabaBillItem
	pages := 0
	for day := first; day.Month() == first.Month(); day = day.AddDate(0, 0, 1) {
		nextToken := ""
		for {
			if pages++; pages > alibabaBillingMaxPages {
				return nil, fmt.Errorf("the bill has more than %d rows", alibabaBillingMaxPages*alibabaBillingPageSize)
			}
			query := url.Values{
				"BillingCycle":  {period},
				"BillingDate":   {day.Format("2006-01-02")},
				"Granularity":   {"DAILY"},
				"IsBillingItem": {"true"},
				"MaxResults":    {strconv.Itoa(alibabaBillingPageSize)},
			}
			if nextToken != "" {
				query.Set("NextToken", nextToken)
			}
			var page alibabaBillPage
			if err := callAlibabaCloud(ctx, client, "https://"+host+"/?"+query.Encode(), accessKey, secretKey, alibabaBillingAction, alibabaBillingVersion, &page); err != nil {
				return nil, err
			}
			if !page.Success {
				return nil, fmt.Errorf("%s: %s", page.Code, page.Message)
			}
			items = append(items, page.Data.Items...)
			nextToken = page.Data.NextToken
			if nextToken == "" {
				break
			}
		}
	}
	return items, nil
}

type alibabaApiKeysPage struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Success   bool   `json:"success"`
	NextToken string `json:"nextToken"`
	ApiKeys   []struct {
		ApiKeyId    int64  `json:"apiKeyId"`
		ApiKeyValue string `json:"apiKeyValue"` // masked: "sk-ws-djI.8O7d*****2aICctnid4u4"
	} `json:"apiKeys"`
}

// alibabaApiKeyIDs resolves the channel's API keys to the ids the bill
// attributes their usage to, by matching each key against the masked
// values Model Studio lists for the account.
func alibabaApiKeyIDs(ctx context.Context, client *http.Client, host, accessKey, secretKey string, keys []string) ([]string, error) {
	type listed struct {
		id             string
		prefix, suffix string
	}
	var all []listed
	nextToken := ""
	for range alibabaApiKeysMaxPages {
		query := url.Values{"maxResults": {strconv.Itoa(alibabaApiKeysPageSize)}}
		if nextToken != "" {
			query.Set("nextToken", nextToken)
		}
		var page alibabaApiKeysPage
		if err := callAlibabaCloud(ctx, client, "https://"+host+alibabaApiKeysPath+"?"+query.Encode(), accessKey, secretKey, alibabaApiKeysAction, alibabaApiKeysVersion, &page); err != nil {
			return nil, err
		}
		if !page.Success && page.Code != "" && page.Code != "200" {
			return nil, fmt.Errorf("%s: %s", page.Code, page.Message)
		}
		for _, k := range page.ApiKeys {
			prefix, suffix, masked := strings.Cut(k.ApiKeyValue, "*")
			if masked {
				suffix = suffix[strings.LastIndex(suffix, "*")+1:]
			}
			all = append(all, listed{id: strconv.FormatInt(k.ApiKeyId, 10), prefix: prefix, suffix: suffix})
		}
		nextToken = page.NextToken
		if nextToken == "" {
			break
		}
	}
	var ids []string
	for _, key := range keys {
		key = strings.TrimSpace(key)
		found := ""
		for _, l := range all {
			if len(key) >= len(l.prefix)+len(l.suffix) && strings.HasPrefix(key, l.prefix) && strings.HasSuffix(key, l.suffix) {
				found = l.id
				break
			}
		}
		if found == "" {
			shown := key
			if len(shown) > 12 {
				shown = shown[:8] + "..." + shown[len(shown)-4:]
			}
			return nil, fmt.Errorf("the key %s is not among the account's %d Model Studio API keys", shown, len(all))
		}
		if !slices.Contains(ids, found) {
			ids = append(ids, found)
		}
	}
	return ids, nil
}

// alibabaBillEntries turns Model Studio pay-as-you-go rows into bill entries
// spanning their day: tokens classified by the instance id's input/output
// type (cached input counts towards the prompt too, as in our records), the
// usage scaled by its unit, the cost = list price minus the contract /
// promotion discount, the payable = what is charged after coupons and free
// quota (before tax). Rows of other products, subscriptions, refunds and
// adjustments are skipped; when apiKeyIDs is set, so are rows attributed
// to other keys.
func alibabaBillEntries(items []alibabaBillItem, apiKeyIDs []string) ([]model.UpstreamBillEntry, error) {
	entries := make([]model.UpstreamBillEntry, 0, len(items))
	for i, it := range items {
		segments := strings.Split(it.InstanceID, ";")
		if it.ProductCode != alibabaBillingProduct && len(segments) < 4 {
			continue
		}
		if (it.Item != "" && it.Item != "PayAsYouGoBill") || it.SubscriptionType == "Subscription" {
			continue
		}
		if len(apiKeyIDs) > 0 && !slices.Contains(apiKeyIDs, segments[0]) {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", it.BillingDate, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("row %d: unrecognized BillingDate %q", i+1, it.BillingDate)
		}
		count, err := bytePlusAmount(it.Usage)
		if err != nil {
			return nil, fmt.Errorf("row %d: Usage %q", i+1, it.Usage)
		}
		e := model.UpstreamBillEntry{
			Wall: day.Unix(), Span: 24 * 3600, Model: strings.TrimSpace(it.InstanceID),
			Cost: it.PretaxGrossAmount - it.InvoiceDiscount, Payable: it.PretaxAmount,
		}
		if len(segments) >= 3 {
			e.Model = strings.TrimSpace(segments[2])
		}
		usageType := ""
		if len(segments) >= 4 {
			usageType = strings.ToLower(segments[3])
		}
		unit := strings.ToLower(strings.TrimSpace(it.UsageUnit))
		if strings.Contains(unit, "token") || strings.Contains(usageType, "token") {
			scale := 1.0
			switch {
			case strings.HasPrefix(unit, "k"), strings.HasPrefix(unit, "thousand"), strings.HasPrefix(unit, "千"):
				scale = 1e3
			case strings.HasPrefix(unit, "m"), strings.HasPrefix(unit, "million"), strings.HasPrefix(unit, "百万"):
				scale = 1e6
			case strings.HasPrefix(unit, "万"):
				scale = 1e4
			}
			tokens := int64(count*scale + 0.5)
			switch {
			case strings.Contains(usageType, "cache"):
				e.CacheReadTokens = tokens
				e.PromptTokens = tokens
			case strings.Contains(usageType, "output"):
				e.CompletionTokens = tokens
			default:
				e.PromptTokens = tokens
			}
		} else {
			e.UsageUnit = strings.TrimSpace(it.UsageUnit)
			e.UsageAmount = int64(count + 0.5)
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, errors.New("the bill has no Model Studio rows for this period")
	}
	return entries, nil
}

// alibabaModelSnapshotRe is the dated snapshot ("-2026-01-23") or "-latest"
// suffix of a model name; the bill names the model without it.
var alibabaModelSnapshotRe = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}|latest)$`)

func alibabaModelKey(name string) string {
	return alibabaModelSnapshotRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "")
}

// AdminCompareAlibabaBill — POST /api/admin/reconciliation/alibaba-bill
// {channel_id, period "YYYY-MM", scope "channel_key" (default) | "account",
// bill_tz_offset?}. With the channel_key scope the bill is filtered to the
// usage attributed to the channel's own API key(s); the account scope takes
// everything the account was billed for Model Studio. The bill's rows are
// days, so the zone is never detected: UTC+8 unless pinned.
func AdminCompareAlibabaBill(c *gin.Context) {
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
	accessKey := strings.TrimSpace(common.OptionMap[customAlibabaAccessKeyOption])
	secretKey := strings.TrimSpace(common.OptionMap[customAlibabaSecretKeyOption])
	common.OptionMapRWMutex.RUnlock()
	if accessKey == "" || secretKey == "" {
		common.ApiErrorMsg(c, "Set the Alibaba Cloud access key ID and secret under Settings > Models > Upstream pricing first")
		return
	}
	channel, err := model.GetChannelById(req.ChannelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	region := alibabaCloudRegion(channel.GetBaseURL())
	billingHost := "business.ap-southeast-1.aliyuncs.com"
	if strings.HasPrefix(region, "cn-") {
		billingHost = "business.aliyuncs.com"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second}
	var apiKeyIDs []string
	if req.Scope == "channel_key" {
		keys := channel.GetKeys()
		if len(keys) == 0 {
			common.ApiErrorMsg(c, "The channel has no API key to filter the bill by")
			return
		}
		apiKeyIDs, err = alibabaApiKeyIDs(ctx, client, "modelstudio."+region+".aliyuncs.com", accessKey, secretKey, keys)
		if err != nil {
			common.ApiErrorMsg(c, "Model Studio could not resolve the channel's API key in this account ("+err.Error()+"); compare the whole account instead")
			return
		}
	}
	items, err := fetchAlibabaDailyBill(ctx, client, billingHost, accessKey, secretKey, req.Period)
	if err != nil {
		common.ApiErrorMsg(c, "Alibaba Cloud billing API: "+err.Error())
		return
	}
	entries, err := alibabaBillEntries(items, apiKeyIDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	offset := alibabaBillOffsetMinutes
	if req.BillTzOffset != nil {
		offset = *req.BillTzOffset
	}
	report, err := model.CompareUpstreamBill(req.ChannelId, entries, &offset, offset, alibabaModelKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	report.BillPayableKnown = true
	report.BillScope = req.Scope
	report.BillSplitItems = apiKeyIDs
	report.BillSource = "alibaba"
	common.ApiSuccess(c, report)
}
