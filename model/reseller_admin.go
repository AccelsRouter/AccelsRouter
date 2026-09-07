// Fork-only: the reseller-admin ROLE, decoupled from the single-payer
// OrgAccount. A reseller org does not consume inference itself — it only funds
// downstream customer orgs — so its admin does not need to be an OrgAccount
// (the paying binding, which is UNIQUE per user). Modeling the reseller admin
// as a management link keeps OrgAccount.UserId UNIQUE intact, so ONE person can
// be an enterprise OrgAccount member AND a reseller admin at the same time
// while request-time billing stays unambiguously single-payer.
package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ResellerAdmin binds a user to the reseller org it administers. UserId is
// UNIQUE: a user administers at most one reseller org. This is NOT an
// OrgAccount and never participates in payer resolution (GetOrgPayerInfo), so
// it does not consume the user's single-payer slot.
type ResellerAdmin struct {
	Id            int    `json:"id" gorm:"primarykey"`
	UserId        int    `json:"user_id" gorm:"uniqueIndex;not null"`
	ResellerOrgId int    `json:"reseller_org_id" gorm:"index;not null"`
	Status        string `json:"status" gorm:"type:varchar(16);index"` // active | suspended
	CreatedTime   int64  `json:"created_time"`
}

// GetResellerAdminOrg returns the reseller org a user administers, or (nil, nil)
// when the user is not a reseller admin. It is the single seam the reseller
// console uses to resolve the caller's reseller org.
func GetResellerAdminOrg(userId int) (*Organization, error) {
	var link ResellerAdmin
	err := DB.Where("user_id = ?", userId).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A suspended admin link keeps no authority: suspension is the per-admin
	// containment lever (offboard/contain one admin without suspending the
	// whole org or banning the user). Empty status = active (pre-status rows).
	if link.Status == OrgStatusSuspended {
		return nil, nil
	}
	org, err := GetOrganizationById(link.ResellerOrgId)
	if err != nil {
		return nil, err
	}
	// Guard against a dangling link (org deleted / retyped): only a live
	// reseller org grants console access.
	if org == nil || org.Type != OrgTypeReseller {
		return nil, nil
	}
	return org, nil
}

// IsResellerAdmin reports whether the user already administers a reseller org.
func IsResellerAdmin(userId int) (bool, error) {
	var count int64
	err := DB.Model(&ResellerAdmin{}).Where("user_id = ?", userId).Count(&count).Error
	return count > 0, err
}

// ListResellerAdmins returns the users who administer a reseller org.
func ListResellerAdmins(resellerOrgId int) ([]ResellerAdmin, error) {
	var rows []ResellerAdmin
	err := DB.Where("reseller_org_id = ?", resellerOrgId).Order("id ASC").Find(&rows).Error
	return rows, err
}

// SetResellerAdminStatus suspends or reactivates a reseller-admin link. Scoping
// by both ids prevents touching an admin of a different org.
func SetResellerAdminStatus(resellerOrgId, userId int, status string) error {
	if status != OrgStatusActive && status != OrgStatusSuspended {
		return errors.New("invalid status")
	}
	result := DB.Model(&ResellerAdmin{}).
		Where("reseller_org_id = ? AND user_id = ?", resellerOrgId, userId).
		Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("该用户不是此代理商的管理员")
	}
	return nil
}

// RemoveResellerAdmin revokes a user's reseller-admin role for a specific
// reseller org. This is the per-admin offboarding / containment lever: it
// severs one admin's access without suspending the whole org or banning the
// user. Scoping the delete by both ids prevents removing an admin of a
// different org.
func RemoveResellerAdmin(resellerOrgId, userId int) error {
	result := DB.Where("reseller_org_id = ? AND user_id = ?", resellerOrgId, userId).Delete(&ResellerAdmin{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("该用户不是此代理商的管理员")
	}
	return nil
}

// backfillResellerAdmins migrates pre-decoupling reseller owners: any reseller
// org whose admin was recorded as an OrgAccount is converted to a ResellerAdmin
// link, and that OrgAccount is removed so the user's single-payer slot is freed
// (letting them also join an enterprise org). Idempotent: a reseller org that
// already has a link is skipped.
func backfillResellerAdmins() error {
	var resellers []Organization
	if err := DB.Where("type = ?", OrgTypeReseller).Find(&resellers).Error; err != nil {
		return err
	}
	for _, org := range resellers {
		var linked int64
		if err := DB.Model(&ResellerAdmin{}).Where("reseller_org_id = ?", org.Id).Count(&linked).Error; err != nil {
			return err
		}
		if linked > 0 {
			continue
		}
		var accs []OrgAccount
		if err := DB.Where("org_id = ?", org.Id).Find(&accs).Error; err != nil {
			return err
		}
		for _, acc := range accs {
			if acc.Role == OrgRoleOwner || acc.Role == OrgRoleAdmin {
				if err := DB.Create(&ResellerAdmin{UserId: acc.UserId, ResellerOrgId: org.Id, Status: OrgStatusActive, CreatedTime: common.GetTimestamp()}).Error; err != nil {
					return err
				}
			}
			if err := DB.Delete(&OrgAccount{}, acc.Id).Error; err != nil {
				return err
			}
			InvalidateOrgPayerCache(acc.UserId)
		}
	}
	return nil
}
