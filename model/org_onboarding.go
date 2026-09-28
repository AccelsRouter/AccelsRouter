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
	// Names are unique across all organizations; also refuse a name another
	// PENDING application already claims, so the applicant learns now rather
	// than at approval.
	if taken, err := OrgNameTaken(app.OrgName, 0); err != nil {
		return err
	} else if taken {
		return ErrOrgNameTaken
	}
	var sameNamePending int64
	if err := DB.Model(&OrgApplication{}).
		Where("LOWER(org_name) = LOWER(?) AND status = ?", app.OrgName, OrgApplicationPending).
		Count(&sameNamePending).Error; err != nil {
		return err
	}
	if sameNamePending > 0 {
		return ErrOrgNameTaken
	}
	// The reseller-admin role is decoupled from the single-payer OrgAccount, so
	// an existing enterprise member MAY apply to become a reseller (and vice
	// versa). Each role is guarded against duplication on its own table.
	if app.Type == OrgTypeReseller {
		if isAdmin, _ := IsResellerAdmin(app.UserId); isAdmin {
			return errors.New("你已是分销商管理员，无法重复申请")
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
				return errors.New("申请人已是分销商管理员")
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
		// Re-check at approval: another org may have taken the name since the
		// application was filed.
		if taken, err := orgNameTaken(tx, app.OrgName, 0); err != nil {
			return err
		} else if taken {
			return ErrOrgNameTaken
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
	if org.Type == OrgTypeReseller {
		// A reseller admin consumes only through reseller keys (reseller wallet,
		// wholesale price, reseller route): its personal keys stop here.
		if _, dErr := DisablePersonalTokens(org.OwnerUserId); dErr != nil {
			common.SysError(fmt.Sprintf("disable personal tokens for reseller admin %d failed: %s", org.OwnerUserId, dErr.Error()))
		}
	}
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
		Where("id = ? AND org_id = ? AND status IN ?", invId, orgId, []string{OrgInvitationPending, OrgInvitationProvisioned}).
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
				// Name the required address so the invitee can act: this fails
				// whenever they are signed in with a different account than the
				// one the invitation was addressed to.
				return fmt.Errorf("该邀请指定邮箱为 %s，请用该邮箱对应的账号登录后再接受", inv.InvitedEmail)
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
	disablePersonalTokensIfResellerParty(accepted.OrgId, userId)
	return accepted, nil
}

// ---------------------------------------------------------------------------
// Provisioned customer accounts ("invite = open the account")
//
// A distributor invites a customer by email. When that email has no account
// yet, the platform opens one on the spot — user + customer-org membership —
// and mails an activation link that lets the person set a password. The
// invitation row tracks it with status "provisioned" and a 7-day activation
// window the distributor can see and renew. An already-registered email keeps
// the consent flow (log in, then accept), because attaching an existing
// account is that person's decision.
// ---------------------------------------------------------------------------

const (
	OrgInvitationProvisioned = "provisioned"
	orgActivationTTL         = 7 * 24 * time.Hour
)

// ErrEmailAlreadyRegistered tells the caller to fall back to a consent invite.
var ErrEmailAlreadyRegistered = errors.New("email already registered")

// provisionedUsername derives a login name from the email's local part, kept
// to the User.Username length limit and made unique with a numeric suffix.
func provisionedUsername(tx *gorm.DB, email string) (string, error) {
	local := email
	if at := strings.Index(email, "@"); at > 0 {
		local = email[:at]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	base := b.String()
	if len(base) < 3 {
		base = "user" + base
	}
	if len(base) > 16 {
		base = base[:16]
	}
	candidate := base
	for i := 2; i < 200; i++ {
		var n int64
		if err := tx.Unscoped().Model(&User{}).Where("username = ?", candidate).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", errors.New("无法生成唯一用户名")
}

// ProvisionCustomerAccount opens an account for an invited email that has no
// user yet, attaches it to the customer org as an admin, and records a
// provisioned invitation whose code is the activation link. The user cannot
// log in until ActivateProvisionedAccount sets a password.
func ProvisionCustomerAccount(orgId int, email string, createdBy int) (*OrgInvitation, *User, error) {
	email = NormalizeEmail(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, nil, errors.New("受邀邮箱必填")
	}
	if IsEmailAlreadyTaken(email) {
		return nil, nil, ErrEmailAlreadyRegistered
	}
	org, err := GetOrganizationById(orgId)
	if err != nil {
		return nil, nil, err
	}
	if org == nil {
		return nil, nil, errors.New("organization not found")
	}
	var user *User
	var inv *OrgInvitation
	err = DB.Transaction(func(tx *gorm.DB) error {
		username, uErr := provisionedUsername(tx, email)
		if uErr != nil {
			return uErr
		}
		user = &User{
			Username:    username,
			DisplayName: username,
			// Unknown to anyone: the activation link is the only way in.
			Password: common.GetRandomString(24),
			Email:    email,
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
			Group:    "default",
		}
		if err := user.InsertWithTx(tx, 0); err != nil {
			return err
		}
		acc := &OrgAccount{
			OrgId: orgId, UserId: user.Id, Relation: OrgRelationCustomer, Role: OrgRoleAdmin,
			Status: OrgStatusActive, PeriodKey: currentPeriodKey(), CreatedTime: common.GetTimestamp(),
		}
		if err := tx.Create(acc).Error; err != nil {
			return err
		}
		inv = &OrgInvitation{
			OrgId: orgId, Code: common.GetUUID(), Relation: OrgRelationCustomer, Role: OrgRoleAdmin,
			InvitedEmail: email, Status: OrgInvitationProvisioned, CreatedBy: createdBy,
			AcceptedUserId: user.Id, ExpiresAt: time.Now().Add(orgActivationTTL).Unix(),
			CreatedTime: common.GetTimestamp(),
		}
		return tx.Create(inv).Error
	})
	if err != nil {
		return nil, nil, err
	}
	user.FinalizeOAuthUserCreation(0)
	InvalidateOrgPayerCache(user.Id)
	return inv, user, nil
}

// RenewProvisionedInvitation issues a fresh activation code with a new 7-day
// window for a provisioned (possibly expired) invitation of this org.
func RenewProvisionedInvitation(orgId, invId int) (*OrgInvitation, error) {
	var inv OrgInvitation
	err := DB.Where("id = ? AND org_id = ? AND status = ?", invId, orgId, OrgInvitationProvisioned).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("邀请不存在或不在待激活状态")
	}
	if err != nil {
		return nil, err
	}
	inv.Code = common.GetUUID()
	inv.ExpiresAt = time.Now().Add(orgActivationTTL).Unix()
	if err := DB.Model(&OrgInvitation{}).Where("id = ?", inv.Id).
		Updates(map[string]interface{}{"code": inv.Code, "expires_at": inv.ExpiresAt}).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetActivationByCode resolves a live (provisioned, unexpired) activation code.
func GetActivationByCode(code string) (*OrgInvitation, error) {
	inv, err := GetOrgInvitationByCode(code)
	if err != nil {
		return nil, err
	}
	if inv == nil || inv.Status != OrgInvitationProvisioned || inv.ExpiresAt < time.Now().Unix() {
		return nil, errors.New("激活链接无效或已过期，请联系邀请方重新发送")
	}
	return inv, nil
}

// ActivateProvisionedAccount sets the provisioned user's password and marks
// the invitation accepted. The code is single-use.
func ActivateProvisionedAccount(code, password string) (*User, error) {
	if n := len(password); n < 8 || n > 20 {
		return nil, errors.New("密码长度需为 8 到 20 位")
	}
	inv, err := GetActivationByCode(code)
	if err != nil {
		return nil, err
	}
	hashed, err := common.Password2Hash(password)
	if err != nil {
		return nil, err
	}
	var user *User
	err = DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&OrgInvitation{}).
			Where("id = ? AND status = ?", inv.Id, OrgInvitationProvisioned).
			Update("status", OrgInvitationAccepted)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("激活链接已被使用")
		}
		if err := tx.Model(&User{}).Where("id = ?", inv.AcceptedUserId).Update("password", hashed).Error; err != nil {
			return err
		}
		var u User
		if err := tx.Where("id = ?", inv.AcceptedUserId).First(&u).Error; err != nil {
			return err
		}
		user = &u
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = updateUserCache(*user)
	return user, nil
}
