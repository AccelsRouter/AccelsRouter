// Fork-only personal BYOK ownership mapping. A personal BYOK channel is a
// normal upstream channel serving a private group reserved for one user
// (user-<id>), priced by that group's ratio (the BYOK fee, default 0). This
// table records which channels a user owns so the personal-BYOK console can
// only ever touch its own channels. Everything else — key storage, routing,
// billing — is the existing channel/group machinery, unchanged; the only new
// routing seam is IsOwnByokGroup, which lets a user's own request use its own
// private group without that group being a globally-usable group.
package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

type UserChannel struct {
	Id          int   `json:"id" gorm:"primarykey"`
	UserId      int   `json:"user_id" gorm:"index;not null"`
	ChannelId   int   `json:"channel_id" gorm:"uniqueIndex;not null"`
	CreatedTime int64 `json:"created_time"`
}

// UserPrivateGroup is the routing/pricing group reserved for one user's BYOK
// channels. It is NEVER added to the global usable-group config, so no other
// user can select it; the owner reaches it only through IsOwnByokGroup.
func UserPrivateGroup(userId int) string {
	return fmt.Sprintf("user-%d", userId)
}

// IsOwnByokGroup reports whether group is the caller's own BYOK private group.
// This is the single, isolation-safe seam the auth middleware uses to let a
// user route to its own BYOK channels: a user can only ever match its OWN id,
// so it can never reach another user's private group.
func IsOwnByokGroup(userId int, group string) bool {
	return group != "" && group == UserPrivateGroup(userId)
}

func AddUserChannel(userId, channelId int) error {
	return DB.Create(&UserChannel{UserId: userId, ChannelId: channelId, CreatedTime: common.GetTimestamp()}).Error
}

// UserOwnsChannel is the authorization check for every personal-BYOK channel
// operation: a user may only touch channels recorded as its own.
func UserOwnsChannel(userId, channelId int) (bool, error) {
	var count int64
	err := DB.Model(&UserChannel{}).Where("user_id = ? AND channel_id = ?", userId, channelId).Count(&count).Error
	return count > 0, err
}

func ListUserChannelIds(userId int) ([]int, error) {
	var ids []int
	err := DB.Model(&UserChannel{}).Where("user_id = ?", userId).Pluck("channel_id", &ids).Error
	return ids, err
}

func RemoveUserChannel(userId, channelId int) error {
	result := DB.Where("user_id = ? AND channel_id = ?", userId, channelId).Delete(&UserChannel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("channel does not belong to this user")
	}
	return nil
}

// CountUserChannels reports how many BYOK channels a user still owns (used to
// decide when the private group's ratio entry can be cleaned up).
func CountUserChannels(userId int) (int64, error) {
	var count int64
	err := DB.Model(&UserChannel{}).Where("user_id = ?", userId).Count(&count).Error
	return count, err
}

// GetUserByokChannelForModel returns the id of an enabled BYOK channel the user
// owns that serves the given model, if any. This is the seam for transparent
// BYOK: a request made with the user's NORMAL key is routed to the user's own
// BYOK channel (their upstream key) whenever they have one for the model —
// matching OpenRouter's one-key experience, no separate BYOK key required. The
// channel lives in the user's private group, so pricing switches to that
// group's ratio (the BYOK fee, default 0) once the caller adopts it.
func GetUserByokChannelForModel(userId int, modelName string) (int, bool) {
	ids, err := ListUserChannelIds(userId)
	if err != nil || len(ids) == 0 {
		return 0, false
	}
	group := UserPrivateGroup(userId)
	for _, id := range ids {
		if IsChannelEnabledForGroupModel(group, modelName, id) {
			return id, true
		}
	}
	return 0, false
}
