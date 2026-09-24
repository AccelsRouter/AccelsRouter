// Fork-only BYOK ownership mapping (design doc: BYOK = org-supplied provider
// credentials materialized as regular channels serving the org's private
// group, priced by that group's ratio). This table records which channels an
// organization owns so the org console can only ever touch its own channels;
// everything else (key storage, routing, billing) is the existing channel
// machinery, unchanged.
package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

type OrgChannel struct {
	Id          int   `json:"id" gorm:"primarykey"`
	OrgId       int   `json:"org_id" gorm:"index;not null"`
	ChannelId   int   `json:"channel_id" gorm:"uniqueIndex;not null"`
	CreatedTime int64 `json:"created_time"`
}

// OrgPrivateGroup is the routing/pricing group reserved for one org's BYOK
// channels. The group ratio configured for it IS the BYOK service fee.
func OrgPrivateGroup(orgId int) string {
	return fmt.Sprintf("org-%d", orgId)
}

func AddOrgChannel(orgId, channelId int) error {
	return DB.Create(&OrgChannel{OrgId: orgId, ChannelId: channelId, CreatedTime: common.GetTimestamp()}).Error
}

// OrgOwnsChannel is the authorization check for every org-console channel
// operation: an org may only touch channels recorded as its own.
func OrgOwnsChannel(orgId, channelId int) (bool, error) {
	var count int64
	err := DB.Model(&OrgChannel{}).Where("org_id = ? AND channel_id = ?", orgId, channelId).Count(&count).Error
	return count > 0, err
}

func ListOrgChannelIds(orgId int) ([]int, error) {
	var ids []int
	err := DB.Model(&OrgChannel{}).Where("org_id = ?", orgId).Pluck("channel_id", &ids).Error
	return ids, err
}

func RemoveOrgChannel(orgId, channelId int) error {
	result := DB.Where("org_id = ? AND channel_id = ?", orgId, channelId).Delete(&OrgChannel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("channel does not belong to this organization")
	}
	return nil
}
