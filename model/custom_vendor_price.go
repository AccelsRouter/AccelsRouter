package model

// CustomVendorPrice is the latest list price one source reports for a model.
// Prices are USD per 1M tokens. Tiers holds a JSON list of input-length tiers
// when the vendor prices by prompt length. The "manual" source is an
// administrator's entry and always wins over fetched ones.
type CustomVendorPrice struct {
	ID             int      `json:"id" gorm:"primaryKey"`
	ModelName      string   `json:"model_name" gorm:"size:191;uniqueIndex:idx_custom_vendor_price_model_source,priority:1"`
	Source         string   `json:"source" gorm:"size:64;uniqueIndex:idx_custom_vendor_price_model_source,priority:2"`
	Provider       string   `json:"provider" gorm:"size:191"`
	InputPrice     float64  `json:"input"`
	OutputPrice    *float64 `json:"output"`
	CacheReadPrice *float64 `json:"cache_read"`
	Tiers          string   `json:"tiers" gorm:"type:text"`
	Note           string   `json:"note" gorm:"type:text"`
	UpdatedAt      int64    `json:"updated_at"`
}

// CustomVendorPriceChange records a price a source reported that differs from the
// stored one. A change that looks like a parsing mistake (a jump of more than 3x)
// stays "pending" and is not applied until an administrator accepts it.
type CustomVendorPriceChange struct {
	ID         int    `json:"id" gorm:"primaryKey"`
	ModelName  string `json:"model_name" gorm:"size:191;index"`
	Source     string `json:"source" gorm:"size:64"`
	Status     string `json:"status" gorm:"size:16;index"` // applied | pending | accepted | rejected
	OldPrice   string `json:"old_price" gorm:"type:text"`
	NewPrice   string `json:"new_price" gorm:"type:text"`
	Reason     string `json:"reason" gorm:"type:text"`
	CreatedAt  int64  `json:"created_at"`
	ResolvedAt int64  `json:"resolved_at"`
}

// CustomVendorPriceSource is how the last refresh of one source went.
type CustomVendorPriceSource struct {
	Source      string `json:"source" gorm:"primaryKey;size:64"`
	Status      string `json:"status" gorm:"size:16"` // ok | error | suspect
	Message     string `json:"message" gorm:"type:text"`
	Models      int    `json:"models"`
	RefreshedAt int64  `json:"refreshed_at"`
}
