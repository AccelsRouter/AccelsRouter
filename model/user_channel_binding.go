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

// UserChannelBinding represents one (user, channel, model) triple used by
// "channel pricing mode" billing (see User.BillingMode). The ratio is
// configured per model, not per channel: a channel that serves several
// models can bill each of them at a different rate for the same user, so
// binding channel 5 for gpt-4o and binding channel 5 for gpt-3.5-turbo are
// two independent rows, each with its own ratio. "This channel is bound to
// this user" is derived, not stored directly — it's true whenever at least
// one row exists for (UserId, ChannelId), for any model.
//
// When a request from User for Model is actually routed to Channel, this
// row's ratio replaces the group ratio in the normal billing formula
// (quota = modelPrice * quotaPerUnit * ratio). Binding/unbinding doesn't
// check or care about the channel's current status, or whether the channel
// is even configured to serve that model — a broken, disabled, or
// mismatched binding can still exist, it just won't be picked for live
// traffic (see GetChannelPricingChannel).
type UserChannelBinding struct {
	Id        int     `json:"id"`
	UserId    int     `json:"user_id" gorm:"uniqueIndex:idx_user_channel_binding,priority:1"`
	ChannelId int     `json:"channel_id" gorm:"uniqueIndex:idx_user_channel_binding,priority:2"`
	ModelName string  `json:"model_name" gorm:"type:varchar(255);uniqueIndex:idx_user_channel_binding,priority:3"`
	Ratio     float64 `json:"ratio" gorm:"type:decimal(10,4);default:1"`
	CreatedAt int64   `json:"created_at" gorm:"autoCreateTime"`
}

func (UserChannelBinding) TableName() string {
	return "user_channel_bindings"
}

// UpsertUserChannelBinding binds channelId to userId for modelName with the
// given ratio. If a binding already exists for this exact (userId,
// channelId, modelName) triple, its ratio is updated in place instead of
// creating a duplicate row — a different model on the same channel is a
// separate row with its own ratio, untouched by this call.
func UpsertUserChannelBinding(userId int, channelId int, modelName string, ratio float64) error {
	if ratio <= 0 {
		ratio = 1
	}
	var existing UserChannelBinding
	err := DB.Where("user_id = ? AND channel_id = ? AND model_name = ?", userId, channelId, modelName).First(&existing).Error
	if err == nil {
		return DB.Model(&existing).Update("ratio", ratio).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	binding := &UserChannelBinding{UserId: userId, ChannelId: channelId, ModelName: modelName, Ratio: ratio}
	return DB.Create(binding).Error
}

// DeleteUserChannelBinding removes a single (userId, channelId, modelName)
// binding. Not an error if no such binding exists. Other models still
// bound on the same channel are untouched.
func DeleteUserChannelBinding(userId int, channelId int, modelName string) error {
	return DB.Where("user_id = ? AND channel_id = ? AND model_name = ?", userId, channelId, modelName).Delete(&UserChannelBinding{}).Error
}

// DeleteUserChannelBindingsForChannel removes every model binding userId
// has on channelId in one go — the "fully unbind this channel" action, as
// opposed to DeleteUserChannelBinding's "unbind just this one model".
func DeleteUserChannelBindingsForChannel(userId int, channelId int) error {
	return DB.Where("user_id = ? AND channel_id = ?", userId, channelId).Delete(&UserChannelBinding{}).Error
}

// GetUserChannelBindings lists every (channel, model) binding for userId,
// newest first. Used to render the binding list in the user edit UI — a
// channel bound for three models appears as three rows, one per model.
func GetUserChannelBindings(userId int) ([]*UserChannelBinding, error) {
	var bindings []*UserChannelBinding
	err := DB.Where("user_id = ?", userId).Order("id desc").Find(&bindings).Error
	return bindings, err
}

// GetUserChannelBindingRatio returns the ratio configured for (userId,
// channelId, modelName). found is false if no such binding exists —
// callers should treat that as "this channel/model pair isn't actually
// bound to this user" rather than silently defaulting to 1.
func GetUserChannelBindingRatio(userId int, channelId int, modelName string) (ratio float64, found bool) {
	var binding UserChannelBinding
	err := DB.Where("user_id = ? AND channel_id = ? AND model_name = ?", userId, channelId, modelName).First(&binding).Error
	if err != nil {
		return 0, false
	}
	return binding.Ratio, true
}

// getUserBoundChannelIds returns the distinct channel IDs userId is bound
// to for ANY model, in a stable (ascending) order. A channel counts as
// bound here even if only one of its models has a binding row — this is
// "which channels does this user touch at all", not "which channel serves
// a specific model"; see getUserBoundChannelIdsForModel for the latter.
func getUserBoundChannelIds(userId int) ([]int, error) {
	var ids []int
	err := DB.Model(&UserChannelBinding{}).
		Where("user_id = ?", userId).
		Distinct("channel_id").
		Order("channel_id asc").
		Pluck("channel_id", &ids).Error
	return ids, err
}

// getUserBoundChannelIdsForModel returns the channel IDs userId has an
// explicit binding row for modelName specifically, in a stable (ascending)
// order — used by GetChannelPricingChannel so that "retry" can walk the
// list deterministically. Unlike getUserBoundChannelIds, a channel bound
// only for a different model is excluded: the per-model ratio row is what
// actually authorizes routing that model to that channel.
func getUserBoundChannelIdsForModel(userId int, modelName string) ([]int, error) {
	var ids []int
	err := DB.Model(&UserChannelBinding{}).
		Where("user_id = ? AND model_name = ?", userId, modelName).
		Order("channel_id asc").
		Pluck("channel_id", &ids).Error
	return ids, err
}
