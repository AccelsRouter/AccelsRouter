package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The worked example of Alibaba Cloud's V3 signature doc (RunInstances):
// the canonical request built from the method, query and x-acs-* headers
// must hash to the documented string to sign.
func TestAlibabaStringToSign(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://ecs.cn-shanghai.aliyuncs.com/?ImageId=win2019_1809_x64_dtc_zh-cn_40G_alibase_20230811.vhd&RegionId=cn-shanghai", nil)
	require.NoError(t, err)
	const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	stringToSign, signed := alibabaStringToSign(req, map[string]string{
		"host": "ecs.cn-shanghai.aliyuncs.com", "x-acs-action": "RunInstances", "x-acs-version": "2014-05-26",
		"x-acs-date": "2023-10-26T10:22:32Z", "x-acs-signature-nonce": "3156853299f313e23d1673dc12e1703d", "x-acs-content-sha256": emptyHash,
	}, emptyHash)
	assert.Equal(t, "ACS3-HMAC-SHA256\n7ea06492da5221eba5297e897ce16e55f964061054b7695beedaac1145b1e259", stringToSign)
	assert.Equal(t, "host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version", signed)

	assert.Equal(t, "A=1%202&B=%2A~&a.1=x", alibabaCanonicalQuery(url.Values{"a.1": {"x"}, "B": {"*~"}, "A": {"1 2"}}))

	signAlibabaCloudRequest(req, nil, "ak", "sk", "RunInstances", "2014-05-26", "nonce", time.Date(2023, 10, 26, 10, 22, 32, 0, time.UTC))
	assert.Equal(t, "2023-10-26T10:22:32Z", req.Header.Get("x-acs-date"))
	assert.Equal(t, emptyHash, req.Header.Get("x-acs-content-sha256"))
	assert.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "ACS3-HMAC-SHA256 Credential=ak,SignedHeaders=host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version,Signature="))
}

func TestAlibabaCloudRegion(t *testing.T) {
	assert.Equal(t, "ap-southeast-1", alibabaCloudRegion("https://dashscope-intl.aliyuncs.com"))
	assert.Equal(t, "ap-southeast-1", alibabaCloudRegion(""))
	assert.Equal(t, "us-east-1", alibabaCloudRegion("https://dashscope-us.aliyuncs.com/compatible-mode/v1"))
	assert.Equal(t, "cn-beijing", alibabaCloudRegion("https://dashscope.aliyuncs.com"))
}

// Daily billable-item rows become day-spanning entries: the model and the
// input/output type come from the instance id, cached input counts towards
// the prompt, the cost is the list price less the discount and the payable
// what is charged; other products, subscriptions and, when filtering by
// key, other keys' rows are skipped.
func TestAlibabaBillEntries(t *testing.T) {
	items := []alibabaBillItem{
		{InstanceID: "2965964;ws-1;qwen-plus;input_token;;0", BillingDate: "2026-10-08", Item: "PayAsYouGoBill", SubscriptionType: "PayAsYouGo", ProductCode: "bailian", Usage: "12345", UsageUnit: "Token", PretaxGrossAmount: 0.0049, InvoiceDiscount: 0.0009, PretaxAmount: 0.004},
		{InstanceID: "2965964;ws-1;qwen-plus;output_token", BillingDate: "2026-10-08", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "1.5", UsageUnit: "KToken", PretaxGrossAmount: 0.0018, PretaxAmount: 0.0018},
		{InstanceID: "2965964;ws-1;qwen-plus;cached_input_token;;0", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "1,000", UsageUnit: "Token", PretaxGrossAmount: 0.0001, PretaxAmount: 0},
		{InstanceID: "2965964;ws-1;wan2.2-t2i-flash;image;;0", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "3", UsageUnit: "Piece", PretaxGrossAmount: 0.06, PretaxAmount: 0.06},
		// Usage a resource plan covered is reported outside Usage.
		{InstanceID: "2965964;ws-1;qwen3.8-flash;input_token;;1", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "0", UsageUnit: "Token", DeductedByResourcePackage: "87807"},
		{InstanceID: "2965964;ws-1;qwen3.8-flash;output_token;;1", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "17", UsageUnit: "Token", DeductedByResourcePackage: "1300", PretaxGrossAmount: 0.00001, PretaxAmount: 0.00001},
		// Free-quota usage as the international site bills it: a row with nothing on it.
		{InstanceID: "2965964;ws-1;qwen3.8-flash;input_token_cache;0", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "0", UsageUnit: "千tokens", DeductedByResourcePackage: ""},
		// Another key's usage.
		{InstanceID: "111;ws-1;qwen-max;input_token;;0", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "sfm", Usage: "500", UsageUnit: "Token", PretaxGrossAmount: 0.001, PretaxAmount: 0.001},
		// Not model usage.
		{InstanceID: "i-abc", BillingDate: "2026-10-09", Item: "PayAsYouGoBill", ProductCode: "ecs", Usage: "24", UsageUnit: "Hour", PretaxGrossAmount: 1, PretaxAmount: 1},
		{InstanceID: "sp-123", BillingDate: "2026-10-01", Item: "SubscriptionOrder", SubscriptionType: "Subscription", ProductCode: "bailian", PretaxGrossAmount: 100, PretaxAmount: 100},
	}
	entries, err := alibabaBillEntries(items, nil)
	require.NoError(t, err)
	d8 := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC).Unix()
	d9 := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC).Unix()
	assert.Equal(t, []model.UpstreamBillEntry{
		{Wall: d8, Span: 86400, Model: "qwen-plus", PromptTokens: 12345, Cost: 0.004, Payable: 0.004},
		{Wall: d8, Span: 86400, Model: "qwen-plus", CompletionTokens: 1500, Cost: 0.0018, Payable: 0.0018},
		{Wall: d9, Span: 86400, Model: "qwen-plus", PromptTokens: 1000, CacheReadTokens: 1000, Cost: 0.0001},
		{Wall: d9, Span: 86400, Model: "wan2.2-t2i-flash", UsageUnit: "Piece", UsageAmount: 3, Cost: 0.06, Payable: 0.06},
		{Wall: d9, Span: 86400, Model: "qwen3.8-flash", PromptTokens: 87807},
		{Wall: d9, Span: 86400, Model: "qwen3.8-flash", CompletionTokens: 1317, Cost: 0.00001, Payable: 0.00001},
		{Wall: d9, Span: 86400, Model: "qwen3.8-flash"},
		{Wall: d9, Span: 86400, Model: "qwen-max", PromptTokens: 500, Cost: 0.001, Payable: 0.001},
	}, entries)

	filtered, err := alibabaBillEntries(items, []string{"111"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, "qwen-max", filtered[0].Model)

	_, err = alibabaBillEntries(items, []string{"999"})
	require.Error(t, err)
	_, err = alibabaBillEntries([]alibabaBillItem{{InstanceID: "1;ws;m;input_token", BillingDate: "bad", ProductCode: "bailian", Usage: "1"}}, nil)
	require.Error(t, err)

	assert.Equal(t, "qwen-plus", alibabaModelKey("Qwen-Plus-2025-04-28"))
	assert.Equal(t, "qwen-plus", alibabaModelKey("qwen-plus-latest"))
	assert.Equal(t, "qwen3-max", alibabaModelKey("qwen3-max"))
}

// The channel's keys are matched against the masked values Model Studio
// lists (prefix and suffix around the stars) and resolved to the ids the
// bill carries, paging through the list; an unknown key is an error that
// does not leak the key.
func TestAlibabaApiKeyIDs(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
		assert.Equal(t, "ListApiKeys", r.Header.Get("x-acs-action"))
		assert.Equal(t, "2026-02-10", r.Header.Get("x-acs-version"))
		assert.Contains(t, r.Header.Get("Authorization"), "Credential=ak,")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("nextToken") == "" {
			w.Write([]byte(`{"success":true,"nextToken":"p2","apiKeys":[{"apiKeyId":2965964,"apiKeyValue":"sk-ws-djI.8O7d*****2aICctnid4u4"}]}`))
			return
		}
		w.Write([]byte(`{"success":true,"apiKeys":[{"apiKeyId":7,"apiKeyValue":"sk-aaaa****zzzz"},{"apiKeyId":8,"apiKeyValue":"sk-plain"}]}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := srv.Client()
	// The helper dials https; point the test client at the plain server.
	client.Transport = rewriteToPlain(srv.URL)

	ids, err := alibabaApiKeyIDs(context.Background(), client, host, "ak", "sk", []string{"sk-ws-djI.8O7dXXXXXXXX2aICctnid4u4", "sk-plain", "sk-aaaaBBBBzzzz", " sk-plain "})
	require.NoError(t, err)
	assert.Equal(t, []string{"2965964", "8", "7"}, ids)
	assert.Len(t, seen, 2)
	assert.Contains(t, seen[1], "nextToken=p2")

	_, err = alibabaApiKeyIDs(context.Background(), client, host, "ak", "sk", []string{"sk-unknownkey-123456"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-unknownkey-123456")
}

// rewriteToPlain routes the controller's https URLs to an httptest server.
type rewriteToPlain string

func (base rewriteToPlain) RoundTrip(req *http.Request) (*http.Response, error) {
	target, _ := url.Parse(string(base))
	req = req.Clone(req.Context())
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	return http.DefaultTransport.RoundTrip(req)
}
