package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/larknotify"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	customVendorManualSource = "manual"
	// A refreshed price that moves by more than this factor is held for review.
	customVendorMaxJump = 3.0
	// A source that returns less than this share of the models it returned
	// before is assumed to have broken.
	customVendorMinKeepShare = 0.5
	customVendorGuardMinRows = 10
	customVendorRefreshEvery = 24 * time.Hour
)

// customVendorSourcePriority decides which source wins when several price a model.
var customVendorSourcePriority = []string{
	customVendorManualSource, "alibaba", "z.ai", "deepseek", "kimi", "anthropic", "xai", "google", "openai", "byteplus", "volcengine",
}

type customVendorSourceResult struct {
	Name   string
	Prices map[string]customVendorPrice
	Note   string
	Err    error
}

// fetchCustomVendorSources reads every source; one that fails is reported, not fatal.
func fetchCustomVendorSources(ctx context.Context) []customVendorSourceResult {
	client := &http.Client{Timeout: 30 * time.Second}
	results := make([]customVendorSourceResult, 0, len(customVendorPageSources)+1)
	for _, source := range customVendorPageSources {
		results = append(results, customVendorSourceResult{Name: source.Name})
	}
	results = append(results, customVendorSourceResult{Name: "alibaba"})

	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(result *customVendorSourceResult) {
			defer wg.Done()
			switch result.Name {
			case "alibaba":
				result.Prices, result.Note, result.Err = fetchCustomAlibabaVendorPrices(ctx, client)
			default:
				for _, source := range customVendorPageSources {
					if source.Name != result.Name {
						continue
					}
					body, err := fetchCustomVendorBody(ctx, client, source.URL)
					if err != nil {
						result.Err = err
						return
					}
					result.Prices, result.Err = source.Parse(body)
				}
			}
		}(&results[i])
	}
	wg.Wait()
	return results
}

func customVendorRowFromPrice(modelName, source string, price customVendorPrice, now int64) model.CustomVendorPrice {
	row := model.CustomVendorPrice{
		ModelName:      modelName,
		Source:         source,
		Provider:       price.Provider,
		InputPrice:     price.Input,
		OutputPrice:    price.Output,
		CacheReadPrice: price.CacheRead,
		UpdatedAt:      now,
	}
	if len(price.Tiers) > 0 {
		if encoded, err := common.Marshal(price.Tiers); err == nil {
			row.Tiers = string(encoded)
		}
	}
	return row
}

func customVendorPriceFromRow(row model.CustomVendorPrice) customVendorPrice {
	price := customVendorPrice{
		Provider:  row.Provider,
		Input:     row.InputPrice,
		Output:    row.OutputPrice,
		CacheRead: row.CacheReadPrice,
	}
	if row.Tiers != "" {
		_ = common.Unmarshal([]byte(row.Tiers), &price.Tiers)
	}
	return price
}

func customVendorSamePrice(a, b model.CustomVendorPrice) bool {
	same := func(x, y *float64) bool {
		if x == nil || y == nil {
			return x == y
		}
		return math.Abs(*x-*y) < 1e-9
	}
	return math.Abs(a.InputPrice-b.InputPrice) < 1e-9 &&
		same(a.OutputPrice, b.OutputPrice) &&
		same(a.CacheReadPrice, b.CacheReadPrice) &&
		a.Tiers == b.Tiers
}

func customVendorJump(a, b *float64) float64 {
	if a == nil || b == nil {
		return 1
	}
	if *a <= 0 && *b <= 0 {
		return 1
	}
	if *a <= 0 || *b <= 0 {
		return math.Inf(1)
	}
	return math.Max(*a / *b, *b / *a)
}

func customVendorIsLargeJump(old, next model.CustomVendorPrice) bool {
	in1, in2 := old.InputPrice, next.InputPrice
	return customVendorJump(&in1, &in2) > customVendorMaxJump ||
		customVendorJump(old.OutputPrice, next.OutputPrice) > customVendorMaxJump
}

// customVendorPriceJSON is a stable encoding: it leaves out the row id and the
// timestamp, so the same price always yields the same text.
func customVendorPriceJSON(row model.CustomVendorPrice) string {
	row.ID, row.UpdatedAt = 0, 0
	encoded, err := common.Marshal(row)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// customVendorChangeSummary collects what one refresh found, for the Lark card.
type customVendorChangeSummary struct {
	Applied int
	Pending int
	Report  larknotify.VendorPriceReport
}

// customVendorTokenCount writes a prompt-length bound the way vendors do:
// 200000 as "200k", 1000000 as "1M".
func customVendorTokenCount(tokens float64) string {
	switch {
	case tokens >= 1e6 && math.Mod(tokens, 1e6) == 0:
		return strconv.FormatFloat(tokens/1e6, 'f', -1, 64) + "M"
	case tokens >= 1e3 && math.Mod(tokens, 1e3) == 0:
		return strconv.FormatFloat(tokens/1e3, 'f', -1, 64) + "k"
	}
	return strconv.FormatFloat(tokens, 'f', -1, 64)
}

// customVendorCardTiers lists a stored price the way the Lark card shows it:
// one line per input-length tier or time band.
func customVendorCardTiers(row model.CustomVendorPrice) []larknotify.VendorPriceTier {
	price := customVendorPriceFromRow(row)
	tiers := price.Tiers
	if len(tiers) == 0 {
		tiers = []customVendorTier{{Input: price.Input, Output: price.Output, CacheRead: price.CacheRead}}
	}
	// A task model shows its first usage price, with what it is charged per.
	if tiers[0].Usage != nil {
		unit := "per image"
		if _, tokens := tiers[0].Usage["tokens"]; tokens {
			unit = "per 1M video tokens"
		}
		return []larknotify.VendorPriceTier{{Range: unit, Input: price.Input}}
	}
	lines := make([]larknotify.VendorPriceTier, 0, len(tiers))
	for i, tier := range tiers {
		line := larknotify.VendorPriceTier{
			Band: tier.Band, Input: tier.Input, Output: tier.Output,
			CacheRead: tier.CacheRead, CacheWrite: tier.CacheWrite, AudioInput: tier.AudioInput,
		}
		switch {
		case tier.Band != "" || len(tiers) == 1:
		case tier.Upper > 0:
			line.Range = "≤" + customVendorTokenCount(tier.Upper)
		case i > 0:
			line.Range = ">" + customVendorTokenCount(tiers[i-1].Upper)
		}
		lines = append(lines, line)
	}
	return lines
}

func (s *customVendorChangeSummary) add(source, modelName string, old, next model.CustomVendorPrice, pending bool) {
	kind := larknotify.VendorPriceChanged
	if pending {
		s.Pending++
		kind = larknotify.VendorPricePending
	} else {
		s.Applied++
	}
	s.Report.Changes = append(s.Report.Changes, larknotify.VendorPriceChange{
		Kind: kind, Model: modelName, Source: source,
		Old: customVendorCardTiers(old), New: customVendorCardTiers(next),
	})
}

// applyCustomVendorSource stores one source's fresh prices. Nothing is written when
// the source looks broken, and a price that jumps by more than customVendorMaxJump
// waits for review, so a wrong parse cannot silently replace good data.
func applyCustomVendorSource(source string, fetched map[string]customVendorPrice, force bool, now int64, summary *customVendorChangeSummary) error {
	var existing []model.CustomVendorPrice
	if err := model.DB.Where("source = ?", source).Find(&existing).Error; err != nil {
		return err
	}
	current := make(map[string]model.CustomVendorPrice, len(existing))
	for _, row := range existing {
		current[row.ModelName] = row
	}
	if !force && len(existing) >= customVendorGuardMinRows &&
		float64(len(fetched)) < float64(len(existing))*customVendorMinKeepShare {
		return fmt.Errorf("suspect: %d models now, %d before; the page may have changed", len(fetched), len(existing))
	}
	initial := len(existing) == 0
	for name, price := range fetched {
		next := customVendorRowFromPrice(name, source, price, now)
		old, found := current[name]
		if !found {
			if err := model.DB.Create(&next).Error; err != nil {
				return err
			}
			if !initial {
				summary.Report.Changes = append(summary.Report.Changes, larknotify.VendorPriceChange{
					Kind: larknotify.VendorPriceNew, Model: name, Source: source, New: customVendorCardTiers(next),
				})
			}
			continue
		}
		if customVendorSamePrice(old, next) {
			continue
		}
		next.ID = old.ID
		// A provider label that changed carries a new conversion rate: the
		// administrator moved the price, not the vendor.
		if !initial && !force && old.Provider == next.Provider && customVendorIsLargeJump(old, next) {
			if err := recordCustomVendorPendingChange(name, source, old, next, now); err != nil {
				return err
			}
			summary.add(source, name, old, next, true)
			continue
		}
		if err := model.DB.Save(&next).Error; err != nil {
			return err
		}
		if err := model.DB.Create(&model.CustomVendorPriceChange{
			ModelName: name, Source: source, Status: "applied",
			OldPrice: customVendorPriceJSON(old), NewPrice: customVendorPriceJSON(next), CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		summary.add(source, name, old, next, false)
	}
	return nil
}

// recordCustomVendorPendingChange skips a change that is already waiting, or that an
// administrator already rejected.
func recordCustomVendorPendingChange(name, source string, old, next model.CustomVendorPrice, now int64) error {
	newJSON := customVendorPriceJSON(next)
	var count int64
	if err := model.DB.Model(&model.CustomVendorPriceChange{}).
		Where("model_name = ? AND source = ? AND new_price = ? AND status IN ?", name, source, newJSON, []string{"pending", "rejected"}).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return model.DB.Create(&model.CustomVendorPriceChange{
		ModelName: name, Source: source, Status: "pending",
		OldPrice: customVendorPriceJSON(old), NewPrice: newJSON,
		Reason: fmt.Sprintf("price moved by more than %gx", customVendorMaxJump), CreatedAt: now,
	}).Error
}

var (
	customVendorRefreshMu sync.Mutex
	// What the last alert said needs attention, so the daily refresh raises it
	// once and again only when it changes. Guarded by customVendorRefreshMu.
	customVendorAttentionSent string
)

// RefreshCustomVendorPrices reads all sources, stores them and reports
// changes, the models that charge less than their vendor lists and a CNY rate
// the market has moved away from.
func RefreshCustomVendorPrices(ctx context.Context, force bool) error {
	customVendorRefreshMu.Lock()
	defer customVendorRefreshMu.Unlock()
	dropRetiredCustomVendorSources()
	results := fetchCustomVendorSources(ctx)
	var drift *larknotify.VendorRateDrift
	common.OptionMapRWMutex.RLock()
	rate, _ := strconv.ParseFloat(strings.TrimSpace(common.OptionMap[customVendorCNYPerUSDOption]), 64)
	common.OptionMapRWMutex.RUnlock()
	if rate > 0 {
		if market, date, err := fetchCustomMarketCNYPerUSD(ctx, &http.Client{Timeout: 30 * time.Second}); err != nil {
			common.SysLog(common.LogText("market CNY rate not read: %s", err.Error()))
		} else {
			drift = customVendorRateDrift(rate, market, date)
			for i := range results {
				if results[i].Name == "volcengine" {
					results[i].Note = fmt.Sprintf("market rate %s CNY per USD on %s", strconv.FormatFloat(market, 'f', -1, 64), date)
				}
			}
		}
	}
	summary, err := storeCustomVendorResults(results, force, common.GetTimestamp())
	if err != nil {
		return err
	}
	summary.Report.Rate = drift
	if summary.Report.Below, err = customVendorUnderpriced(); err != nil {
		common.SysLog(common.LogText("prices below vendor list prices not checked: %s", err.Error()))
	}
	notifyCustomVendorChanges(ctx, summary)
	return nil
}

// storeCustomVendorResults stores what every source returned and records how
// its refresh went. A source is reported as failed when it stops being
// readable (or fails on its first refresh), not again while it stays broken.
func storeCustomVendorResults(results []customVendorSourceResult, force bool, now int64) (*customVendorChangeSummary, error) {
	summary := &customVendorChangeSummary{}
	var previous []model.CustomVendorPriceSource
	if err := model.DB.Find(&previous).Error; err != nil {
		return nil, err
	}
	broken := make(map[string]bool, len(previous))
	for _, status := range previous {
		broken[status.Source] = status.Status != "ok"
	}
	for _, result := range results {
		status := model.CustomVendorPriceSource{Source: result.Name, RefreshedAt: now}
		err := result.Err
		if err == nil {
			err = applyCustomVendorSource(result.Name, result.Prices, force, now, summary)
		}
		switch {
		case err == nil:
			status.Status, status.Models, status.Message = "ok", len(result.Prices), result.Note
		case strings.HasPrefix(err.Error(), "suspect"):
			status.Status, status.Message = "suspect", err.Error()
		default:
			status.Status, status.Message = "error", err.Error()
		}
		if status.Status != "ok" && !broken[result.Name] {
			summary.Report.Failures = append(summary.Report.Failures, larknotify.VendorPriceFailure{Source: result.Name, Message: status.Message})
		}
		if err := model.DB.Save(&status).Error; err != nil {
			return nil, err
		}
	}
	return summary, nil
}

// dropRetiredCustomVendorSources removes what sources that are no longer read left
// behind, so they stop showing up in the catalog.
func dropRetiredCustomVendorSources() {
	for _, source := range []string{"models.dev"} {
		model.DB.Where("source = ?", source).Delete(&model.CustomVendorPrice{})
		model.DB.Where("source = ?", source).Delete(&model.CustomVendorPriceSource{})
	}
}

// notifyCustomVendorChanges sends the refresh's findings to the Lark alert
// group: prices that changed or are held for review, newly listed models,
// sources that stopped being readable, models charging less than their vendor
// lists and a drifted CNY rate. Models the gateway serves are marked. Without
// a change or a failed source, the card is sent only when what needs attention
// differs from what the last card said.
func notifyCustomVendorChanges(ctx context.Context, summary *customVendorChangeSummary) {
	report := summary.Report
	attention := make([]string, 0, len(report.Below)+1)
	for _, below := range report.Below {
		attention = append(attention, below.Model)
	}
	slices.Sort(attention)
	if report.Rate != nil {
		attention = append(attention, "CNY rate")
	}
	state := strings.Join(attention, "\n")
	raised := state != "" && state != customVendorAttentionSent
	customVendorAttentionSent = state
	if len(report.Changes)+len(report.Failures) == 0 && !raised {
		return
	}
	served := model.GetEnabledModels()
	for i := range report.Changes {
		report.Changes[i].InUse = slices.Contains(served, report.Changes[i].Model)
	}
	if err := service.SendLarkCard(ctx, service.LarkAlertTarget(), larknotify.VendorPriceCard(report, service.LarkTranslator)); err != nil {
		common.SysLog(common.LogText("vendor price change alert not sent: %s", err.Error()))
	}
}

// StartCustomVendorPriceRefresh refreshes the catalog daily on the master node.
func StartCustomVendorPriceRefresh() {
	if !common.IsMasterNode {
		return
	}
	time.Sleep(time.Minute)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := RefreshCustomVendorPrices(ctx, false); err != nil {
			common.SysLog(common.LogText("vendor price refresh failed: %s", err.Error()))
		}
		cancel()
		time.Sleep(customVendorRefreshEvery)
	}
}

// resolveCustomVendorPrices picks, for every model, the price of the highest
// priority source that has one.
func resolveCustomVendorPrices() (map[string]customVendorPrice, map[string]bool, error) {
	var rows []model.CustomVendorPrice
	if err := model.DB.Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	rank := make(map[string]int, len(customVendorSourcePriority))
	for i, name := range customVendorSourcePriority {
		rank[name] = i
	}
	best := make(map[string]model.CustomVendorPrice)
	for _, row := range rows {
		current, ok := best[row.ModelName]
		if !ok || rank[row.Source] < rank[current.Source] ||
			(rank[row.Source] == rank[current.Source] && row.ID < current.ID) {
			best[row.ModelName] = row
		}
	}
	prices := make(map[string]customVendorPrice, len(best))
	manual := make(map[string]bool)
	for name, row := range best {
		prices[name] = customVendorPriceFromRow(row)
		if row.Source == customVendorManualSource {
			manual[name] = true
		}
	}
	// ByteDance prices a model family once; a snapshot this gateway serves
	// (doubao-seed-2-1-pro-260715) carries its family's price.
	for _, name := range model.GetEnabledModels() {
		family := customArkSnapshotDate.ReplaceAllString(name, "")
		price, listed := prices[family]
		if _, own := prices[name]; own || !listed {
			continue
		}
		if strings.HasPrefix(price.Provider, customVendorBytePlusProvider) || strings.HasPrefix(price.Provider, customVendorVolcengineProvider) {
			prices[name] = price
		}
	}
	return prices, manual, nil
}

type customVendorPriceView struct {
	customVendorPrice
	Manual bool `json:"manual,omitempty"`
	// Syncable is set when the price sync offers this price as a model price.
	Syncable bool `json:"syncable,omitempty"`
}

func customVendorSourceMessages() (map[string]string, error) {
	var statuses []model.CustomVendorPriceSource
	if err := model.DB.Find(&statuses).Error; err != nil {
		return nil, err
	}
	messages := make(map[string]string, len(statuses))
	for _, status := range statuses {
		if status.Status == "ok" {
			messages[status.Source] = fmt.Sprintf("ok: %d models", status.Models)
			if status.Message != "" {
				messages[status.Source] += " (" + status.Message + ")"
			}
		} else {
			messages[status.Source] = "error: " + status.Message
		}
	}
	return messages, nil
}

// GetCustomVendorPrices serves the stored catalog, filling it first when empty.
func GetCustomVendorPrices(c *gin.Context) {
	var count int64
	if err := model.DB.Model(&model.CustomVendorPrice{}).Count(&count).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if count == 0 {
		if err := RefreshCustomVendorPrices(c.Request.Context(), false); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	prices, manual, err := resolveCustomVendorPrices()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	sources, err := customVendorSourceMessages()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	data := make(map[string]customVendorPriceView, len(prices))
	for name, price := range prices {
		_, syncable := customVendorSyncExpr(name, price)
		data[name] = customVendorPriceView{customVendorPrice: price, Manual: manual[name], Syncable: syncable}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data, "sources": sources})
}

func RefreshCustomVendorPricesNow(c *gin.Context) {
	force := c.Query("force") == "true"
	if err := RefreshCustomVendorPrices(c.Request.Context(), force); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// GetCustomVendorPriceCatalog lists the source statuses and manual entries.
func GetCustomVendorPriceCatalog(c *gin.Context) {
	var statuses []model.CustomVendorPriceSource
	var manual []model.CustomVendorPrice
	if err := model.DB.Order("source").Find(&statuses).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.DB.Where("source = ?", customVendorManualSource).Order("model_name").Find(&manual).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"sources": statuses, "manual": manual}})
}

type customVendorManualRequest struct {
	ModelName string   `json:"model_name"`
	Input     float64  `json:"input"`
	Output    *float64 `json:"output"`
	CacheRead *float64 `json:"cache_read"`
	Note      string   `json:"note"`
}

func validCustomVendorNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1e6
}

// PutCustomVendorManualPrice pins a price an administrator confirmed.
func PutCustomVendorManualPrice(c *gin.Context) {
	var req customVendorManualRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	name := strings.TrimSpace(req.ModelName)
	if name == "" || req.Input <= 0 || !validCustomVendorNumber(req.Input) ||
		(req.Output != nil && !validCustomVendorNumber(*req.Output)) ||
		(req.CacheRead != nil && !validCustomVendorNumber(*req.CacheRead)) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid model name or price"})
		return
	}
	now := common.GetTimestamp()
	row := customVendorRowFromPrice(name, customVendorManualSource, customVendorPrice{
		Provider: "manual", Input: req.Input, Output: req.Output, CacheRead: req.CacheRead,
	}, now)
	row.Note = strings.TrimSpace(req.Note)
	var existing model.CustomVendorPrice
	err := model.DB.Where("model_name = ? AND source = ?", name, customVendorManualSource).First(&existing).Error
	switch {
	case err == nil:
		row.ID = existing.ID
	case !errors.Is(err, gorm.ErrRecordNotFound):
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Save(&row).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "vendor_price.manual_set", map[string]any{"model": name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteCustomVendorManualPrice(c *gin.Context) {
	name := strings.TrimSpace(c.Query("model"))
	if name == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "model is required"})
		return
	}
	if err := model.DB.Where("model_name = ? AND source = ?", name, customVendorManualSource).
		Delete(&model.CustomVendorPrice{}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "vendor_price.manual_delete", map[string]any{"model": name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// ListCustomVendorPriceChanges returns recent changes, newest first.
func ListCustomVendorPriceChanges(c *gin.Context) {
	query := model.DB.Model(&model.CustomVendorPriceChange{}).Order("id desc").Limit(200)
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	var changes []model.CustomVendorPriceChange
	if err := query.Find(&changes).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].ID > changes[j].ID })
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": changes})
}

func resolveCustomVendorChange(c *gin.Context, accept bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid id"})
		return
	}
	var change model.CustomVendorPriceChange
	if err := model.DB.First(&change, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if change.Status != "pending" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "change is not pending"})
		return
	}
	now := common.GetTimestamp()
	change.ResolvedAt = now
	change.Status = "rejected"
	if accept {
		var next model.CustomVendorPrice
		if err := common.Unmarshal([]byte(change.NewPrice), &next); err != nil {
			common.ApiError(c, err)
			return
		}
		var existing model.CustomVendorPrice
		if err := model.DB.Where("model_name = ? AND source = ?", change.ModelName, change.Source).First(&existing).Error; err == nil {
			next.ID = existing.ID
		} else {
			next.ID = 0
		}
		next.UpdatedAt = now
		if err := model.DB.Save(&next).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		change.Status = "accepted"
	}
	if err := model.DB.Save(&change).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func AcceptCustomVendorPriceChange(c *gin.Context) { resolveCustomVendorChange(c, true) }
func RejectCustomVendorPriceChange(c *gin.Context) { resolveCustomVendorChange(c, false) }

func RegisterCustomVendorPriceRoutes(group *gin.RouterGroup) {
	group.GET("/vendor_prices", GetCustomVendorPrices)
	group.POST("/vendor_prices/refresh", RefreshCustomVendorPricesNow)
	group.GET("/vendor_price_catalog", GetCustomVendorPriceCatalog)
	group.PUT("/vendor_prices/manual", PutCustomVendorManualPrice)
	group.DELETE("/vendor_prices/manual", DeleteCustomVendorManualPrice)
	group.GET("/vendor_price_changes", ListCustomVendorPriceChanges)
	group.POST("/vendor_price_changes/:id/accept", AcceptCustomVendorPriceChange)
	group.POST("/vendor_price_changes/:id/reject", RejectCustomVendorPriceChange)
}
