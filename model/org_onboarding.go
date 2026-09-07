// Fork-only self-service onboarding for the organization system: users apply
// to open their OWN org (they become owner — no effect on anyone else), and
// orgs grow by INVITING existing users who accept (consent-gated), closing the
// M1 conscription hole while removing the admin's manual create+attach step.
//
// The two org types are decoupled: each has its own auto-approve policy
// (OrgEnterpriseAutoApprove / OrgResellerAutoApprove options), so enterprise
// can be instant while reseller stays gated behind review.
package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	OrgApplicationPending  = "pending"
	OrgApplicationApproved = "approved"
	OrgApplicationRejected = "rejected"

	OrgInvitationPending  = "pending"
	OrgInvitationAccepted = "accepted"
	OrgInvitationRevoked  = "revoked"

	orgInvitationTTL = 14 * 24 * time.Hour
)

// Decoupled per-type auto-approve policy, set via the option API.
//
// Defaults mirror OpenRouter: opening an ENTERPRISE org is instant self-serve
// (their in-product "Create Organization"). It is low-risk — an org cannot
// spend until it is funded, and billing is key/workspace-scoped — so there is
// nothing for a human to gate. A RESELLER stays review-gated: a reseller can
// allocate credit to OTHER orgs (real trust / margin), which warrants a human
// decision. An admin can still flip either policy via the option API.
var (
	OrgEnterpriseAutoApprove = true
	OrgResellerAutoApprove   = false
	// Default price group assigned to an auto/blank approval. Never a wholesale
	// group by default: granting reseller margin stays an explicit admin act.
	OrgDefaultPriceGroup = "default"
)

// OrgApplication is a user's request to open an organization. Creating one has
// no billing effect; approval creates the Organization with the applicant as
// owner.
type OrgApplication struct {
	Id          int    `json:"id" gorm:"primarykey"`
	UserId      int    `json:"user_id" gorm:"index;not null"`
	Type        string `json:"type" gorm:"type:varchar(16);index"` // enterprise | reseller
	OrgName     string `json:"org_name" gorm:"type:varchar(128)"`
	Contact     string `json:"contact" gorm:"type:varchar(128)"`
	Remark      string `json:"remark" gorm:"type:varchar(255)"`
	Status      string `json:"status" gorm:"type:varchar(16);index"`
	ReviewNote  string `json:"review_note" gorm:"type:varchar(255)"`
	ReviewerId  int    `json:"reviewer_id"`
	OrgId       int    `json:"org_id"` // set on approval
	CreatedTime int64  `json:"created_time"`
	ProcessedAt int64  `json:"processed_at"`
}

// OrgInvitation is a consent token: a target user must accept it before being
// attached to the org, so no one is billed to an org without opting in.
type OrgInvitation struct {
	Id             int    `json:"id" gorm:"primarykey"`
	OrgId          int    `json:"org_id" gorm:"index;not null"`
	Code           string `json:"code" gorm:"type:varchar(64);uniqueIndex"`
	Relation       string `json:"relation" gorm:"type:varchar(16)"`
	Role           string `json:"role" gorm:"type:varchar(16)"`
	MonthlyBudget  int    `json:"monthly_budget"`
	InvitedEmail   string `json:"invited_email" gorm:"type:varchar(128)"`
	Status         string `json:"status" gorm:"type:varchar(16);index"`
	CreatedBy      int    `json:"created_by"`
	AcceptedUserId int    `json:"accepted_user_id"`
	ExpiresAt      int64  `json:"expires_at"`
	CreatedTime    int64  `json:"created_time"`
}

// ---------------------------------------------------------------------------
// Applications
// ---------------------------------------------------------------------------

// CreateOrgApplication records a pending application. A user may hold only one
// pending application and must not already belong to an organization.
func CreateOrgApplication(app *OrgApplication) error {
	app.OrgName = strings.TrimSpace(app.OrgName)
	if err := ValidateOrgName(app.OrgName); err != nil {
		return err
	}
	if app.Type != OrgTypeEnterprise && app.Type != OrgTypeReseller {
		return errors.New("invalid organization type")
	}
	// The reseller-admin role is decoupled from the single-payer OrgAccount, so
	// an existing enterprise member MAY apply to become a reseller (and vice
	// versa). Each role is guarded against duplication on its own table.
	if app.Type == OrgTypeReseller {
		if isAdmin, _ := IsResellerAdmin(app.UserId); isAdmin {
			return errors.New("你已是代理商管理员，无法重复申请")
		}
	} else if existing, _ := GetOrgAccountByUser(app.UserId); existing != nil {
		return errors.New("你已归属某个组织，无法申请开通")
	}
	var pending int64
	if err := DB.Model(&OrgApplication{}).
		Where("user_id = ? AND status = ?", app.UserId, OrgApplicationPending).
		Count(&pending).Error; err != nil {
		return err
	}
	if pending > 0 {
		return errors.New("你已有待审批的申请")
	}
	app.Status = OrgApplicationPending
	app.CreatedTime = common.GetTimestamp()
	return DB.Create(app).Error
}

func GetOrgApplicationById(id int) (*OrgApplication, error) {
	var app OrgApplication
	err := DB.Where("id = ?", id).First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &app, err
}

// GetLatestOrgApplicationByUser returns the user's most recent application (for
// the self status view).
func GetLatestOrgApplicationByUser(userId int) (*OrgApplication, error) {
	var app OrgApplication
	err := DB.Where("user_id = ?", userId).Order("id DESC").First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &app, err
}

func ListOrgApplications(status, typ string, offset, limit int) ([]*OrgApplication, int64, error) {
	q := DB.Model(&OrgApplication{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*OrgApplication
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// ApproveOrgApplication atomically flips a pending application to approved,
// creates the organization, and attaches the applicant as owner. Guards
// re-run: only a pending row is processed, and the applicant must still be
// unmanaged.
func ApproveOrgApplication(appId, reviewerId int, priceGroup, note string) (*Organization, error) {
	if priceGroup == "" {
		priceGroup = OrgDefaultPriceGroup
	}
	var org *Organization
	err := DB.Transaction(func(tx *gorm.DB) error {
		var app OrgApplication
		if err := lockForUpdate(tx).Where("id = ?", appId).First(&app).Error; err != nil {
			return errors.New("application not found")
		}
		if app.Status != OrgApplicationPending {
			return errors.New("申请已被处理")
		}
		// Re-run the type-specific duplication guard inside the transaction.
		// Reseller admin is decoupled from the paying OrgAccount, so a reseller
		// approval is NOT blocked by an existing enterprise membership.
		if app.Type == OrgTypeReseller {
			var already int64
			if err := tx.Model(&ResellerAdmin{}).Where("user_id = ?", app.UserId).Count(&already).Error; err != nil {
				return err
			}
			if already > 0 {
				return errors.New("申请人已是代理商管理员")
			}
		} else {
			var managed int64
			if err := tx.Model(&OrgAccount{}).Where("user_id = ?", app.UserId).Count(&managed).Error; err != nil {
				return err
			}
			if managed > 0 {
				return errors.New("申请人已归属某个组织")
			}
		}
		newOrg := &Organization{
			Name:        app.OrgName,
			Type:        app.Type,
			Status:      OrgStatusActive,
			PriceGroup:  priceGroup,
			OwnerUserId: app.UserId,
			CreatedTime: common.GetTimestamp(),
			UpdatedTime: common.GetTimestamp(),
		}
		if err := tx.Create(newOrg).Error; err != nil {
			return err
		}
		if newOrg.Type == OrgTypeReseller {
			// A reseller admin is a management role, not a paying OrgAccount:
			// the applicant keeps its single-payer slot free (so it can also be
			// an enterprise member). See model/reseller_admin.go.
			if err := tx.Create(&ResellerAdmin{
				UserId: app.UserId, ResellerOrgId: newOrg.Id, Status: OrgStatusActive, CreatedTime: common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
		} else {
			ownerAcc := &OrgAccount{
				OrgId: newOrg.Id, UserId: app.UserId, Relation: OrgRelationMember, Role: OrgRoleOwner,
				Status: OrgStatusActive, PeriodKey: currentPeriodKey(), CreatedTime: common.GetTimestamp(),
			}
			if err := tx.Create(ownerAcc).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&OrgApplication{}).Where("id = ?", appId).Updates(map[string]interface{}{
			"status":       OrgApplicationApproved,
			"reviewer_id":  reviewerId,
			"review_note":  note,
			"org_id":       newOrg.Id,
			"processed_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		org = newOrg
		return nil
	})
	if err != nil {
		return nil, err
	}
	InvalidateOrgPayerCache(org.OwnerUserId)
	return org, nil
}

func RejectOrgApplication(appId, reviewerId int, note string) error {
	result := DB.Model(&OrgApplication{}).
		Where("id = ? AND status = ?", appId, OrgApplicationPending).
		Updates(map[string]interface{}{
			"status":       OrgApplicationRejected,
			"reviewer_id":  reviewerId,
			"review_note":  note,
			"processed_at": common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("申请不存在或已被处理")
	}
	return nil
}

// OrgTypeAutoApproves reports whether applications of a type skip review.
func OrgTypeAutoApproves(typ string) bool {
	switch typ {
	case OrgTypeEnterprise:
		return OrgEnterpriseAutoApprove
	case OrgTypeReseller:
		return OrgResellerAutoApprove
	}
	return false
}

// ---------------------------------------------------------------------------
// Invitations (consent-gated attach)
// ---------------------------------------------------------------------------

func CreateOrgInvitation(inv *OrgInvitation) error {
	if inv.Relation != OrgRelationMember && inv.Relation != OrgRelationCustomer {
		return errors.New("invalid relation")
	}
	if inv.Role != OrgRoleAdmin && inv.Role != OrgRoleMember {
		return errors.New("invalid role")
	}
	if inv.MonthlyBudget < 0 {
		return errors.New("budget cannot be negative")
	}
	// The invited email is required and scopes the invite to one person: only a
	// user whose account email matches may accept it (enforced in Accept). An
	// unenforced, optional email would be meaningless.
	inv.InvitedEmail = NormalizeEmail(strings.TrimSpace(inv.InvitedEmail))
	if inv.InvitedEmail == "" || !strings.Contains(inv.InvitedEmail, "@") {
		return errors.New("受邀邮箱必填")
	}
	inv.Code = common.GetUUID()
	inv.Status = OrgInvitationPending
	inv.ExpiresAt = time.Now().Add(orgInvitationTTL).Unix()
	inv.CreatedTime = common.GetTimestamp()
	return DB.Create(inv).Error
}

func ListOrgInvitations(orgId int) ([]*OrgInvitation, error) {
	var rows []*OrgInvitation
	err := DB.Where("org_id = ?", orgId).Order("id DESC").Find(&rows).Error
	return rows, err
}

func RevokeOrgInvitation(orgId, invId int) error {
	result := DB.Model(&OrgInvitation{}).
		Where("id = ? AND org_id = ? AND status = ?", invId, orgId, OrgInvitationPending).
		Update("status", OrgInvitationRevoked)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("邀请不存在或已被处理")
	}
	return nil
}

// GetOrgInvitationByCode returns a pending, unexpired invitation (for the
// accept-preview screen).
func GetOrgInvitationByCode(code string) (*OrgInvitation, error) {
	var inv OrgInvitation
	err := DB.Where("code = ?", code).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &inv, err
}

// AcceptOrgInvitation binds the accepting user to the org, atomically consuming
// the invitation. The USER performs this (consent). Fails if the invitation is
// not pending/expired or the user already belongs to an organization.
func AcceptOrgInvitation(code string, userId int) (*OrgInvitation, error) {
	var accepted *OrgInvitation
	err := DB.Transaction(func(tx *gorm.DB) error {
		var inv OrgInvitation
		if err := lockForUpdate(tx).Where("code = ?", code).First(&inv).Error; err != nil {
			return errors.New("邀请不存在")
		}
		if inv.Status != OrgInvitationPending {
			return errors.New("邀请已失效")
		}
		if inv.ExpiresAt > 0 && time.Now().Unix() > inv.ExpiresAt {
			return errors.New("邀请已过期")
		}
		var managed int64
		if err := tx.Model(&OrgAccount{}).Where("user_id = ?", userId).Count(&managed).Error; err != nil {
			return err
		}
		if managed > 0 {
			return errors.New("你已归属某个组织，请先解绑")
		}
		// Enforce the invite's email scope: only the invited address may accept.
		// (Legacy invitations with an empty email are unscoped for compatibility.)
		if inv.InvitedEmail != "" {
			var u User
			if err := tx.Select("email").Where("id = ?", userId).First(&u).Error; err != nil {
				return errors.New("用户不存在")
			}
			if !strings.EqualFold(NormalizeEmail(u.Email), inv.InvitedEmail) {
				return errors.New("该邀请指定的邮箱与你的账号邮箱不一致")
			}
		}
		acc := &OrgAccount{
			OrgId: inv.OrgId, UserId: userId, Relation: inv.Relation, Role: inv.Role,
			MonthlyBudget: inv.MonthlyBudget, Status: OrgStatusActive,
			PeriodKey: currentPeriodKey(), CreatedTime: common.GetTimestamp(),
		}
		if err := tx.Create(acc).Error; err != nil {
			return fmt.Errorf("attach failed: %w", err)
		}
		if err := tx.Model(&OrgInvitation{}).Where("id = ?", inv.Id).Updates(map[string]interface{}{
			"status":           OrgInvitationAccepted,
			"accepted_user_id": userId,
		}).Error; err != nil {
			return err
		}
		accepted = &inv
		return nil
	})
	if err != nil {
		return nil, err
	}
	InvalidateOrgPayerCache(userId)
	return accepted, nil
}
