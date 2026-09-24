package model

import (
	"errors"

	"gorm.io/gorm"
)

// ChannelModel holds per-(channel, model) attributes that don't apply
// uniformly across everything a channel serves. It started as just a
// priority override (see PriorityForModel) — a channel that supports both
// gpt-4o and gpt-3.5-turbo may want gpt-4o preferred over other channels
// serving it while gpt-3.5-turbo isn't, which a single channel-wide
// Priority field can't express. Deliberately its own table rather than a
// JSON blob on Channel: a table lets future per-(channel, model)
// attributes (e.g. a dedicated price or its own rate limit) be added as
// plain columns instead of growing an ever-more-nested serialized blob.
//
// A row existing here is an override, not a binding — Channel.Models (the
// comma-separated list) still decides which models a channel serves at
// all. A model with no row here just uses the channel's own base
// Priority; deleting a row reverts that model to that default rather than
// un-serving it.
type ChannelModel struct {
	Id        int    `json:"id"`
	ChannelId int    `json:"channel_id" gorm:"uniqueIndex:idx_channel_model,priority:1"`
	ModelName string `json:"model_name" gorm:"type:varchar(255);uniqueIndex:idx_channel_model,priority:2"`
	Priority  int64  `json:"priority" gorm:"type:bigint;default:0"`
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime"`
}

func (ChannelModel) TableName() string {
	return "channel_models"
}

// UpsertChannelModelPriority sets (or updates) channelId's priority
// override for modelName.
func UpsertChannelModelPriority(channelId int, modelName string, priority int64) error {
	var existing ChannelModel
	err := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName).First(&existing).Error
	if err == nil {
		return DB.Model(&existing).Update("priority", priority).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return DB.Create(&ChannelModel{ChannelId: channelId, ModelName: modelName, Priority: priority}).Error
}

// DeleteChannelModelPriority removes channelId's override for modelName,
// reverting that model back to the channel's own base Priority. Not an
// error if no such override exists.
func DeleteChannelModelPriority(channelId int, modelName string) error {
	return DB.Where("channel_id = ? AND model_name = ?", channelId, modelName).Delete(&ChannelModel{}).Error
}

// GetChannelModelPriorities returns every priority override configured
// for channelId, as a model name -> priority map — the shape
// Channel.PriorityForModel expects. Empty, not an error, when the channel
// has no overrides.
func GetChannelModelPriorities(channelId int) (map[string]int64, error) {
	var rows []ChannelModel
	if err := DB.Where("channel_id = ?", channelId).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		result[r.ModelName] = r.Priority
	}
	return result, nil
}

// ListChannelModelPriorities lists channelId's raw override rows, newest
// first — used by the admin UI's per-channel model priority editor.
func ListChannelModelPriorities(channelId int) ([]*ChannelModel, error) {
	var rows []*ChannelModel
	err := DB.Where("channel_id = ?", channelId).Order("id desc").Find(&rows).Error
	return rows, err
}
