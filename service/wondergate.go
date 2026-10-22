package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// WonderGate acquiring gateway client.
// Docs: https://document.wondergate.io/zh/reference/introduction.html
//
// Auth: HTTP Basic Auth (merchantId:secretKey). Create-checkout returns a
// hosted redirectUrl (code 301). Async notifications are signed with SHA256
// over the sorted non-empty parameter values + secretKey.

const (
	wonderGateCheckoutPath    = "/checkout/payment"
	wonderGateSearchOrderPath = "/search/list/order"

	// Notification / transaction result codes (see appendix).
	WonderGateCodeTransactionSuccess  = 100
	WonderGateCodeTransactionDeclined = 101
	WonderGateCodeTransactionPending  = 102
	WonderGateCodeOrderClosed         = 103
	WonderGateCodeRefundSuccess       = 111
	wonderGateCodeSearchSuccess       = 121

	wonderGateHTTPTimeout = 20 * time.Second

	// Order-query date params ("2006-01-02 15:04:05"); the gateway caps the
	// range at one month. A ±24h pad absorbs any timezone skew between our
	// clock and the gateway's.
	wonderGateSearchTimeLayout = "2006-01-02 15:04:05"
	wonderGateSearchTimePad    = 24 * time.Hour
)

// WonderGateCheckoutRequest is the subset of /checkout/payment fields we send.
// billingAddress.country is mandatory; we supply a configured default because
// a balance top-up has no real billing address.
type WonderGateCheckoutRequest struct {
	TransactionID   string                   `json:"transactionId"`
	Currency        string                   `json:"currency"`
	Amount          string                   `json:"amount"`
	AppID           string                   `json:"appId"`
	TransactionIP   string                   `json:"transactionIp"`
	CallbackURL     string                   `json:"callbackUrl"`
	NotificationURL string                   `json:"notificationUrl"`
	ProductInfos    []map[string]interface{} `json:"productInfos"`
	BillingAddress  map[string]interface{}   `json:"billingAddress"`
	// TransactionMethod pre-selects the payment method (e.g. "AlipayHK") and
	// skips the cashier page; omitted, WonderGate shows the cashier.
	TransactionMethod string `json:"transactionMethod,omitempty"`
}

// WonderGateCheckoutResult is the parsed create-checkout response.
type WonderGateCheckoutResult struct {
	Code               int    `json:"code"`
	Message            string `json:"message"`
	TransactionMessage string `json:"transactionMessage"`
	RedirectURL        string `json:"redirectUrl"`
	UniqueID           string `json:"uniqueId"`
	TransactionID      string `json:"transactionId"`
}

// CreateWonderGateCheckout posts a checkout order and returns the hosted
// redirect URL. Uses the active environment's base URL + credentials.
func CreateWonderGateCheckout(
	ctx context.Context,
	req *WonderGateCheckoutRequest,
) (*WonderGateCheckoutResult, error) {
	merchantId, secretKey, _ := setting.WonderGateActiveCredentials()
	if merchantId == "" || secretKey == "" {
		return nil, errors.New("WonderGate 凭证未配置")
	}

	body, err := common.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(setting.WonderGateGatewayBaseURL(), "/") + wonderGateCheckoutPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(merchantId, secretKey)

	client := &http.Client{Timeout: wonderGateHTTPTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody := new(bytes.Buffer)
	if _, err := respBody.ReadFrom(resp.Body); err != nil {
		return nil, err
	}

	var result WonderGateCheckoutResult
	if err := common.Unmarshal(respBody.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w (body=%s)", err, respBody.String())
	}

	// 301 = redirect required (create success). Anything else is a failure.
	if result.Code != 301 || result.RedirectURL == "" {
		msg := result.TransactionMessage
		if msg == "" {
			msg = result.Message
		}
		return &result, fmt.Errorf("WonderGate 建单失败 code=%d message=%q", result.Code, msg)
	}
	return &result, nil
}

// VerifyWonderGateNotification verifies the async-notification signature and
// returns the parsed top-level fields.
//
// Signature (per docs): take every non-empty scalar parameter except `sign`,
// sort keys by ASCII, concatenate their raw values, append the SecretKey, then
// SHA256. Values are read from the raw JSON (json.RawMessage) so numeric forms
// such as "100.000" are compared exactly as the gateway signed them; nested
// objects/arrays are excluded.
func VerifyWonderGateNotification(rawBody []byte) (map[string]json.RawMessage, error) {
	_, secretKey, _ := setting.WonderGateActiveCredentials()
	if secretKey == "" {
		return nil, errors.New("WonderGate 凭证未配置")
	}

	var fields map[string]json.RawMessage
	if err := common.Unmarshal(rawBody, &fields); err != nil {
		return nil, err
	}

	signRaw, ok := fields["sign"]
	if !ok {
		return nil, errors.New("回调缺少 sign")
	}
	var sign string
	if err := common.Unmarshal(signRaw, &sign); err != nil || sign == "" {
		return nil, errors.New("回调 sign 无效")
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "sign" {
			continue
		}
		if wonderGateSignableValue(fields[k]) == "" {
			continue // skip empty / nested / null
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(wonderGateSignableValue(fields[k]))
	}
	sb.WriteString(secretKey)

	sum := sha256.Sum256([]byte(sb.String()))
	expected := hex.EncodeToString(sum[:])

	if subtle.ConstantTimeCompare(
		[]byte(strings.ToLower(expected)),
		[]byte(strings.ToLower(sign)),
	) != 1 {
		return nil, fmt.Errorf("验签失败 expected=%s got=%s", expected, sign)
	}
	return fields, nil
}

// wonderGateSignableValue converts a raw JSON value to the string used in the
// signature. Strings are unquoted; numbers/bools use their raw text; empty,
// null, and nested objects/arrays return "" (excluded from the signature).
func wonderGateSignableValue(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == `""` {
		return ""
	}
	if s[0] == '{' || s[0] == '[' {
		return "" // nested — excluded
	}
	if s[0] == '"' {
		var str string
		if err := common.Unmarshal(raw, &str); err != nil {
			return ""
		}
		return str
	}
	return s // number / boolean, raw form preserved
}

// WonderGateNotificationInt reads an integer field (e.g. code) from parsed
// notification fields.
func WonderGateNotificationInt(fields map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}
	var n int
	if err := common.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

// WonderGateNotificationString reads a string field from parsed notification
// fields.
func WonderGateNotificationString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := common.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// wonderGateOrderSearchResult is the parsed GET /search/list/order response.
// Each data entry's code carries the transaction status (100 approved, 101
// declined, 102 pending, 103 closed).
type wonderGateOrderSearchResult struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		Code          int    `json:"code"`
		Message       string `json:"message"`
		TransactionID string `json:"transactionId"`
		UniqueID      string `json:"uniqueId"`
	} `json:"data"`
}

// ErrWonderGateOrderNotFound is returned when the gateway has no record of
// the queried transactionId (e.g. the user never reached the payment page).
var ErrWonderGateOrderNotFound = errors.New("wondergate order not found")

// QueryWonderGateOrderCode looks up a transaction by our transactionId via
// GET /search/list/order and returns its status code (100 approved, 101
// declined, 102 pending, 103 closed).
//
// SAFETY: the gateway has been observed to return transactions OTHER than the
// queried transactionId in the result window (a declined $1 order was almost
// credited off another order's approved entry). Every entry is therefore
// verified against our transactionId client-side, and entries that do not
// echo a transactionId are never trusted for crediting.
// createdAtUnix bounds the mandatory startDate/endingDate params.
func QueryWonderGateOrderCode(ctx context.Context, transactionId string, createdAtUnix int64) (int, error) {
	merchantId, secretKey, _ := setting.WonderGateActiveCredentials()
	if merchantId == "" || secretKey == "" {
		return 0, errors.New("WonderGate 凭证未配置")
	}
	if transactionId == "" {
		return 0, errors.New("未提供交易单号")
	}

	createdAt := time.Unix(createdAtUnix, 0)
	params := url.Values{}
	params.Set("transactionId", transactionId)
	params.Set("startDate", createdAt.Add(-wonderGateSearchTimePad).Format(wonderGateSearchTimeLayout))
	params.Set("endingDate", time.Now().Add(wonderGateSearchTimePad).Format(wonderGateSearchTimeLayout))

	reqURL := strings.TrimRight(setting.WonderGateGatewayBaseURL(), "/") + wonderGateSearchOrderPath + "?" + params.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	httpReq.SetBasicAuth(merchantId, secretKey)

	client := &http.Client{Timeout: wonderGateHTTPTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("WonderGate 订单查询失败: HTTP %d", resp.StatusCode)
	}

	var result wonderGateOrderSearchResult
	if err := common.DecodeJson(resp.Body, &result); err != nil {
		return 0, err
	}
	if result.Code != wonderGateCodeSearchSuccess {
		return 0, fmt.Errorf("WonderGate 订单查询失败: code=%d message=%q", result.Code, result.Message)
	}
	return matchWonderGateOrderCode(result, transactionId)
}

// matchWonderGateOrderCode attributes a status code from the search result to
// OUR transactionId. This is a billing boundary: crediting on an entry that
// belongs to a different order turns someone else's approval into free quota,
// so only an exact transactionId echo is trusted and ambiguity always errs.
func matchWonderGateOrderCode(result wonderGateOrderSearchResult, transactionId string) (int, error) {
	if len(result.Data) == 0 {
		return 0, ErrWonderGateOrderNotFound
	}

	matched := false
	echoed := false
	code := 0
	for _, item := range result.Data {
		if item.TransactionID != "" {
			echoed = true
		}
		if item.TransactionID != transactionId {
			continue
		}
		matched = true
		code = item.Code
		if code == WonderGateCodeTransactionSuccess {
			break
		}
	}
	if matched {
		return code, nil
	}
	if !echoed {
		// The gateway did not echo transactionId on any entry, so entries
		// cannot be attributed to our order. Never credit on ambiguity.
		common.SysError(fmt.Sprintf(
			"WonderGate 订单查询结果无 transactionId 回显，拒绝对账 trade_no=%s entries=%d first_code=%d",
			transactionId, len(result.Data), result.Data[0].Code))
		return 0, fmt.Errorf("WonderGate 订单查询结果无法安全匹配 trade_no=%s", transactionId)
	}
	// Entries echo transactionIds but none is ours: the filter returned other
	// orders; treat our order as not found and log what came back.
	common.SysError(fmt.Sprintf(
		"WonderGate 订单查询过滤失效：返回 %d 条均非目标单 trade_no=%s first_txn=%s",
		len(result.Data), transactionId, result.Data[0].TransactionID))
	return 0, ErrWonderGateOrderNotFound
}
