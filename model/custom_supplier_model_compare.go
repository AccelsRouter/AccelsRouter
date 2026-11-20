package model

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// SupplierModelSearchLog records that a user looked up a model on a day. One
// row per user, model and day keeps repeated clicks from inflating rankings.
type SupplierModelSearchLog struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"uniqueIndex:uk_supplier_search_user_model_day,priority:1"`
	ModelName   string `json:"model_name" gorm:"type:varchar(128);uniqueIndex:uk_supplier_search_user_model_day,priority:2;index:idx_supplier_search_day_model,priority:2"`
	Day         int    `json:"day" gorm:"uniqueIndex:uk_supplier_search_user_model_day,priority:3;index:idx_supplier_search_day_model,priority:1"`
	CreatedTime int64  `json:"created_time" gorm:"type:bigint"`
}

type SupplierModelCatalogItem struct {
	ModelName     string   `json:"model_name"`
	SearchCount   int      `json:"search_count"`
	SupplierCount int      `json:"supplier_count"`
	BestDiscount  *float64 `json:"best_discount_pct"`
}

type SupplierModelOffer struct {
	SupplierId     int      `json:"supplier_id"`
	SupplierCode   string   `json:"supplier_code"`
	SupplierName   string   `json:"supplier_name"`
	SupplierStatus string   `json:"supplier_status"`
	Variant        string   `json:"variant"`
	Source         string   `json:"source"`
	DiscountPct    *float64 `json:"discount_pct"`
	Concurrency    *int     `json:"concurrency"`
	Rpm            *int     `json:"rpm"`
	Tpm            *int64   `json:"tpm"`
	Remark         string   `json:"remark"`
	Best           bool     `json:"best"`
}

func supplierDay(t time.Time) int {
	local := t.In(supplierContractZone)
	return local.Year()*10000 + int(local.Month())*100 + local.Day()
}

func RecordSupplierModelSearch(userID int, modelName string) error {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	now := time.Now()
	entry := SupplierModelSearchLog{UserId: userID, ModelName: modelName, Day: supplierDay(now), CreatedTime: now.Unix()}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry).Error
}

// supplierModelOffers groups the enabled model rows of every supplier that is
// not terminated by model name.
func supplierModelOffers() (map[string][]SupplierModelOffer, error) {
	var suppliers []Supplier
	if err := DB.Select("id", "code", "short_name", "legal_name", "status").Where("status <> ?", SupplierStatusTerminated).Find(&suppliers).Error; err != nil {
		return nil, err
	}
	byID := make(map[int]Supplier, len(suppliers))
	ids := make([]int, 0, len(suppliers))
	for _, s := range suppliers {
		byID[s.Id] = s
		ids = append(ids, s.Id)
	}
	offers := map[string][]SupplierModelOffer{}
	if len(ids) == 0 {
		return offers, nil
	}
	var models []SupplierModel
	if err := DB.Where("supplier_id IN ? AND enabled = ? AND model_name <> ?", ids, true, "").Find(&models).Error; err != nil {
		return nil, err
	}
	for _, m := range models {
		s := byID[m.SupplierId]
		name := s.ShortName
		if name == "" {
			name = s.LegalName
		}
		offers[m.ModelName] = append(offers[m.ModelName], SupplierModelOffer{
			SupplierId: s.Id, SupplierCode: s.Code, SupplierName: name, SupplierStatus: s.Status,
			Variant: m.Variant, Source: m.Source, DiscountPct: m.DiscountPct,
			Concurrency: m.Concurrency, Rpm: m.Rpm, Tpm: m.Tpm, Remark: m.Remark,
		})
	}
	return offers, nil
}

func supplierModelSearchCounts(days int) (map[string]int, error) {
	var rows []struct {
		ModelName string
		Hits      int
	}
	fromDay := supplierDay(time.Now().AddDate(0, 0, -(days - 1)))
	err := DB.Model(&SupplierModelSearchLog{}).Select("model_name, COUNT(*) AS hits").Where("day >= ?", fromDay).Group("model_name").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.ModelName] = row.Hits
	}
	return counts, nil
}

func bestDiscount(offers []SupplierModelOffer) *float64 {
	var best *float64
	for _, offer := range offers {
		if offer.DiscountPct != nil && (best == nil || *offer.DiscountPct < *best) {
			value := *offer.DiscountPct
			best = &value
		}
	}
	return best
}

// SupplierModelCatalog ranks offered models by searches in the last days and
// then by name. A keyword searches every offered model instead of the top list.
func SupplierModelCatalog(keyword string, days, limit int) ([]SupplierModelCatalogItem, error) {
	offers, err := supplierModelOffers()
	if err != nil {
		return nil, err
	}
	counts, err := supplierModelSearchCounts(days)
	if err != nil {
		return nil, err
	}
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	items := make([]SupplierModelCatalogItem, 0, len(offers))
	for name, modelOffers := range offers {
		if keyword != "" && !strings.Contains(strings.ToLower(name), keyword) {
			continue
		}
		items = append(items, SupplierModelCatalogItem{
			ModelName:     name,
			SearchCount:   counts[name],
			SupplierCount: len(modelOffers),
			BestDiscount:  bestDiscount(modelOffers),
		})
	}
	slices.SortFunc(items, func(a, b SupplierModelCatalogItem) int {
		return cmp.Or(cmp.Compare(b.SearchCount, a.SearchCount), strings.Compare(a.ModelName, b.ModelName))
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// CompareSupplierModel lists the suppliers offering exactly modelName. sortBy
// is "discount" (lowest first, unknown last), "rpm" or "tpm" (highest first).
func CompareSupplierModel(modelName, sortBy string) ([]SupplierModelOffer, error) {
	offers, err := supplierModelOffers()
	if err != nil {
		return nil, err
	}
	rows := offers[strings.TrimSpace(modelName)]
	if rows == nil {
		return []SupplierModelOffer{}, nil
	}
	best := bestDiscount(rows)
	for i := range rows {
		rows[i].Best = best != nil && rows[i].DiscountPct != nil && *rows[i].DiscountPct == *best
	}
	slices.SortStableFunc(rows, func(a, b SupplierModelOffer) int {
		switch sortBy {
		case "rpm":
			return cmp.Compare(derefOr(b.Rpm, -1), derefOr(a.Rpm, -1))
		case "tpm":
			return cmp.Compare(derefOr(b.Tpm, -1), derefOr(a.Tpm, -1))
		default:
			return cmp.Compare(derefOr(a.DiscountPct, 1e9), derefOr(b.DiscountPct, 1e9))
		}
	})
	return rows, nil
}

func derefOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
