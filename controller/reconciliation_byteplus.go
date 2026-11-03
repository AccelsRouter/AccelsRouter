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
)

type bytePlusBillRow struct {
	ExpenseTime        string `json:"ExpenseTime"` // "2026-10-08 10:15:00-2026-10-08 10:20:00"
	ConfigName         string `json:"ConfigName"`
	ChargeItemCode     string `json:"ChargeItemCode"`
	Count              string `json:"Count"`
	Unit               string `json:"Unit"`
	OriginalBillAmount string `json:"OriginalBillAmount"`
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

func fetchBytePlusSplitBill(ctx context.Context, client *http.Client, accessKey, secretKey, period string) ([]bytePlusBillRow, error) {
	var rows []bytePlusBillRow
	for page := range bytePlusBillingMaxPages {
		body, err := common.Marshal(map[string]any{
			"BillPeriod":    period,
			"Limit":         bytePlusBillingPageSize,
			"Offset":        page * bytePlusBillingPageSize,
			"GroupTerm":     1, // per settlement row, not merged
			"GroupPeriod":   2, // settlement detail: 5-minute ExpenseTime windows
			"NeedRecordNum": 1,
		})
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

// bytePlusBillEntries turns settlement rows into bill entries: one entry per
// row at the window's start, tokens classified by the charge item code
// (prompt / completion / KV-cache hit), the count scaled by its unit, the
// cost = what BytePlus actually charges (PayableAmount, USD). Rows with a
// non-token unit (images, seconds) keep the count as a usage amount.
func bytePlusBillEntries(rows []bytePlusBillRow) ([]model.UpstreamBillEntry, error) {
	entries := make([]model.UpstreamBillEntry, 0, len(rows))
	for i, r := range rows {
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
		cost, err := strconv.ParseFloat(strings.ReplaceAll(r.PayableAmount, ",", ""), 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: PayableAmount %q", i+1, r.PayableAmount)
		}
		e := model.UpstreamBillEntry{Wall: wall.Unix(), Model: strings.TrimSpace(r.ConfigName), Cost: cost}
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

// bytePlusModelKey matches BytePlus's model naming ("DeepSeek-V4.1-flash",
// "GLM_5.2") with ours ("deepseek-v4-1-flash"): lower-case, '.' and '_' as '-'.
func bytePlusModelKey(name string) string {
	return strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(strings.ToLower(strings.TrimSpace(name)))
}

// AdminCompareBytePlusBill — POST /api/admin/reconciliation/byteplus-bill
// {channel_id, period "YYYY-MM", bill_tz_offset?}
func AdminCompareBytePlusBill(c *gin.Context) {
	var req struct {
		ChannelId    int    `json:"channel_id"`
		Period       string `json:"period"`
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
	rows, err := fetchBytePlusSplitBill(ctx, client, accessKey, secretKey, req.Period)
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
	common.ApiSuccess(c, report)
}
