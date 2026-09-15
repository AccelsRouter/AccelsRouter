package model

import (
	"errors"

	"gorm.io/gorm"
)

// BillingModeGroup (default) is today's behavior: the user's Group field
// determines pricing (ratio_setting.GetGroupRatio) and channel selection
// (GetRandomSatisfiedChannel). BillingModeChannelPricing is the
// alternative: the user's Group no longer takes effect at all; instead
// they're bound directly to one or more specific channels
// (UserChannelBinding), each with its own billing ratio, and channel
// selection only ever considers those bound channels (see
// GetChannelPricingChannel). Top-ups for channel-pricing-mode users are
// always at a fixed 1:1 rate — see controller.getPayMoney.
const (
	BillingModeGroup          = "group"
	BillingModeChannelPricing = "channel_pricing"
)

// UserChannelBinding represents one (user, channel) pairing used by
// "channel pricing mode" billing (see User.BillingMode). Each binding
// carries its own billing ratio: when a request from User is actually
// routed to Channel, this ratio replaces the group ratio in the normal
// billing formula (quota = modelPrice * quotaPerUnit * ratio). A user can
// have many bindings (multiple channels, each with its own ratio);
// binding/unbinding doesn't check or care about the channel's current
// status — a broken or disabled channel can still be bound, it just won't
// be picked for live traffic (see GetChannelPricingChannel).
type UserChannelBinding struct {
	Id        int     `json:"id"`
	UserId    int     `json:"user_id" gorm:"uniqueIndex:idx_user_channel_binding"`
	ChannelId int     `json:"channel_id" gorm:"uniqueIndex:idx_user_channel_binding"`
	Ratio     float64 `json:"ratio" gorm:"type:decimal(10,4);default:1"`
	CreatedAt int64   `json:"created_at" gorm:"autoCreateTime"`
}

func (UserChannelBinding) TableName() string {
	return "user_channel_bindings"
}

// UpsertUserChannelBinding binds channelId to userId with the given ratio.
// If a binding already exists for this (userId, channelId) pair, its ratio
// is updated in place instead of creating a duplicate row.
func UpsertUserChannelBinding(userId int, channelId int, ratio float64) error {
	if ratio <= 0 {
		ratio = 1
	}
	var existing UserChannelBinding
	err := DB.Where("user_id = ? AND channel_id = ?", userId, channelId).First(&existing).Error
	if err == nil {
		return DB.Model(&existing).Update("ratio", ratio).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	binding := &UserChannelBinding{UserId: userId, ChannelId: channelId, Ratio: ratio}
	return DB.Create(binding).Error
}

// DeleteUserChannelBinding removes a single (userId, channelId) binding.
// Not an error if no such binding exists.
func DeleteUserChannelBinding(userId int, channelId int) error {
	return DB.Where("user_id = ? AND channel_id = ?", userId, channelId).Delete(&UserChannelBinding{}).Error
}

// GetUserChannelBindings lists all channel bindings for userId, newest
// first. Used to render the binding list in the user edit UI.
func GetUserChannelBindings(userId int) ([]*UserChannelBinding, error) {
	var bindings []*UserChannelBinding
	err := DB.Where("user_id = ?", userId).Order("id desc").Find(&bindings).Error
	return bindings, err
}

// GetUserChannelBindingRatio returns the ratio configured for (userId,
// channelId). found is false if no such binding exists — callers should
// treat that as "this channel isn't actually bound to this user" rather
// than silently defaulting to 1.
func GetUserChannelBindingRatio(userId int, channelId int) (ratio float64, found bool) {
	var binding UserChannelBinding
	err := DB.Where("user_id = ? AND channel_id = ?", userId, channelId).First(&binding).Error
	if err != nil {
		return 0, false
	}
	return binding.Ratio, true
}

// getUserBoundChannelIds returns just the channel IDs userId is bound to,
// in a stable (ascending) order — used by GetChannelPricingChannel so that
// "retry" can walk the list deterministically.
func getUserBoundChannelIds(userId int) ([]int, error) {
	var ids []int
	err := DB.Model(&UserChannelBinding{}).
		Where("user_id = ?", userId).
		Order("channel_id asc").
		Pluck("channel_id", &ids).Error
	return ids, err
}
