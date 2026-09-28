package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// UpstreamPriceSource is the admin-configured price source of one channel for
// the upstream monitor: an explicit pricing endpoint (when the channel's base
// URL exposes none) and/or manually imported prices used when nothing can be
// fetched. Own table; the channels table is not touched.
type UpstreamPriceSource struct {
	Id        int    `json:"id" gorm:"primarykey"`
	ChannelId int    `json:"channel_id" gorm:"uniqueIndex;not null"`
	PriceURL  string `json:"price_url" gorm:"type:varchar(512)"`
	// ManualPrices is normalized JSON {"model_ratio":{},"completion_ratio":{},"model_price":{}}.
	ManualPrices string `json:"manual_prices" gorm:"type:text"`
	UpdatedBy    int    `json:"updated_by"`
	UpdatedTime  int64  `json:"updated_time"`
}

func (UpstreamPriceSource) TableName() string { return "upstream_price_sources" }

func GetUpstreamPriceSource(channelId int) (*UpstreamPriceSource, error) {
	var row UpstreamPriceSource
	err := DB.Where("channel_id = ?", channelId).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListUpstreamPriceSources returns every configured source keyed by channel.
func ListUpstreamPriceSources() (map[int]*UpstreamPriceSource, error) {
	var rows []UpstreamPriceSource
	if err := DB.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int]*UpstreamPriceSource, len(rows))
	for i := range rows {
		out[rows[i].ChannelId] = &rows[i]
	}
	return out, nil
}

// SetUpstreamPriceSource upserts a channel's source; clearing both fields
// deletes the row.
func SetUpstreamPriceSource(channelId int, priceURL, manualPrices string, operatorId int) error {
	priceURL = strings.TrimSpace(priceURL)
	manualPrices = strings.TrimSpace(manualPrices)
	if priceURL == "" && manualPrices == "" {
		return DB.Where("channel_id = ?", channelId).Delete(&UpstreamPriceSource{}).Error
	}
	fields := map[string]interface{}{
		"price_url":     priceURL,
		"manual_prices": manualPrices,
		"updated_by":    operatorId,
		"updated_time":  common.GetTimestamp(),
	}
	res := DB.Model(&UpstreamPriceSource{}).Where("channel_id = ?", channelId).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	return DB.Create(&UpstreamPriceSource{
		ChannelId: channelId, PriceURL: priceURL, ManualPrices: manualPrices,
		UpdatedBy: operatorId, UpdatedTime: common.GetTimestamp(),
	}).Error
}
