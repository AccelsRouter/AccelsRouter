// Fork-only: an organization or a person that already exists becomes a
// reseller's customer. Either side may start it — the reseller invites an
// email address, or the customer applies with the reseller's join code — and
// both must consent before anything changes. The platform approves as well
// when the customer has paid the platform before, so a reseller cannot take
// over the platform's own clients unnoticed.
//
// A customer bound this way always pays for itself (FundingMode "direct"):
// its wallet holds its own money, which cannot be mixed with an allowance a
// reseller grants and later reclaims. Only a reseller the platform allows to
// have self-funded customers can therefore bind one.
package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	BindDirectionInvite = "invite"
	BindDirectionApply  = "apply"

	// A request waits on the reseller (an application), then on the customer
	// (an invitation, or an application the reseller answered with an offer),
	// then on the platform when the customer has paid it before.
	BindStatusPendingReseller = "pending_reseller"
	BindStatusPendingCustomer = "pending_customer"
	BindStatusPendingPlatform = "pending_platform"
	BindStatusCompleted       = "completed"
	BindStatusRejected        = "rejected"
	BindStatusRevoked         = "revoked"
	// BindStatusExpired is reported for a request nobody answered in time; the
	// stored status stays the pending one it expired in.
	BindStatusExpired = "expired"

	// LedgerTypeTransfer moves a person's own balance into the wallet of the
	// organization opened for them on joining a reseller (platform → org in the
	// ledger's terms; the money was already the platform's liability).
	LedgerTypeTransfer = "transfer"

	resellerBindTTL = 7 * 24 * time.Hour
)

var (
	ErrBindNotOffered      = common.NewMessage("This distributor cannot take on existing organizations")
	ErrBindRequestGone     = common.NewMessage("This request is no longer open")
	ErrBindOwnerOnly       = common.NewMessage("Only the organization owner can do this")
	ErrBindAlreadyCustomer = common.NewMessage("This organization already belongs to a distributor; leave it first")
	ErrBindOrgNotEligible  = common.NewMessage("Only an active enterprise organization on the standard price group can join a distributor")
	ErrBindHasByok         = common.NewMessage("Remove the BYOK channels first: a distributor's customer cannot use its own upstream keys")
	ErrBindOwnDistributor  = common.NewMessage("An administrator of this distributor cannot become its customer")
	ErrBindRequestPending  = common.NewMessage("There is already an open request; withdraw it or wait for its outcome")
	ErrBindAlreadyInvited  = common.NewMessage("This address already has an open invitation from you")
	ErrJoinCodeInvalid     = common.NewMessage("This distributor link is invalid or no longer accepts applications")
	ErrBindEmailInvalid    = common.NewMessage("Please enter a valid email address")
)

// ResellerBindRequest is one attempt to make an existing organization or
// person a reseller's customer, from the first step to its outcome.
type ResellerBindRequest struct {
	Id            int    `json:"id" gorm:"primarykey"`
	ResellerOrgId int    `json:"reseller_org_id" gorm:"index;not null"`
	Direction     string `json:"direction" gorm:"type:varchar(16)"`
	// CustomerOrgId is the organization being bound; 0 while the customer side
	// has not answered an invitation, and for a person until the organization
	// opened for them exists.
	CustomerOrgId int `json:"customer_org_id" gorm:"index"`
	// CustomerUserId is who consents for the customer: the owner of the
	// organization, or the person.
	CustomerUserId int    `json:"customer_user_id" gorm:"index"`
	InvitedEmail   string `json:"invited_email" gorm:"type:varchar(128);index"`
	// Code opens the confirmation page. It grants nothing by itself: an
	// invitation is answered only by the account of the invited address, an
	// application only by the applicant.
	Code            string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	RetailDiscounts string `json:"-" gorm:"type:text"`
	AllowedModels   string `json:"-" gorm:"type:text"`
	// OrgName names the organization opened for a person on binding.
	OrgName string `json:"org_name" gorm:"type:varchar(128)"`
	// TransferBalance asks to move the person's whole personal balance into
	// that organization's wallet; TransferQuota is what was moved.
	TransferBalance bool `json:"transfer_balance"`
	TransferQuota   int  `json:"transfer_quota"`
	// NeedsReview is decided when the customer consents: it has paid the
	// platform before, so the platform approves the binding too.
	NeedsReview        bool   `json:"needs_review"`
	Status             string `json:"status" gorm:"type:varchar(24);index"`
	Remark             string `json:"remark" gorm:"type:varchar(255)"`
	ReviewNote         string `json:"review_note" gorm:"type:varchar(255)"`
	CreatedBy          int    `json:"created_by"`
	ResellerOperatorId int    `json:"reseller_operator_id"`
	ReviewerId         int    `json:"reviewer_id"`
	// ExpiresAt bounds how long a request waits on the reseller or the
	// customer; 0 while it waits on the platform.
	ExpiresAt   int64 `json:"expires_at"`
	CreatedTime int64 `json:"created_time"`
	ProcessedAt int64 `json:"processed_at"`

	// Computed for the consoles.
	Discounts     map[string]float64 `json:"discounts" gorm:"-"`
	Models        []string           `json:"models" gorm:"-"`
	ResellerName  string             `json:"reseller_name" gorm:"-"`
	CustomerName  string             `json:"customer_name" gorm:"-"`
	CustomerEmail string             `json:"customer_email" gorm:"-"`
}

// ResellerJoinCode is the code a reseller hands out so an organization or a
// person can apply to become its customer.
type ResellerJoinCode struct {
	Id            int    `json:"id" gorm:"primarykey"`
	ResellerOrgId int    `json:"reseller_org_id" gorm:"uniqueIndex;not null"`
	Code          string `json:"code" gorm:"type:varchar(32);uniqueIndex;not null"`
	Enabled       bool   `json:"enabled"`
	CreatedTime   int64  `json:"created_time"`
	UpdatedTime   int64  `json:"updated_time"`
}

var openBindStatuses = []string{BindStatusPendingReseller, BindStatusPendingCustomer, BindStatusPendingPlatform}

// openBindRequests narrows a query to the requests still awaiting someone.
func openBindRequests(tx *gorm.DB) *gorm.DB {
	return tx.Model(&ResellerBindRequest{}).
		Where("status IN ?", openBindStatuses).
		Where("(expires_at = 0 OR expires_at > ?)", time.Now().Unix())
}

func (r *ResellerBindRequest) isOpen() bool {
	return (r.Status == BindStatusPendingReseller || r.Status == BindStatusPendingCustomer || r.Status == BindStatusPendingPlatform) &&
		(r.ExpiresAt == 0 || r.ExpiresAt > time.Now().Unix())
}

// describeBindRequests fills the computed fields the consoles show: the offer
// in parsed form, the names of both sides, and "expired" for a request nobody
// answered in time.
func describeBindRequests(rows []*ResellerBindRequest) {
	orgIds := make([]int, 0, len(rows)*2)
	userIds := make([]int, 0, len(rows))
	for _, r := range rows {
		orgIds = append(orgIds, r.ResellerOrgId, r.CustomerOrgId)
		userIds = append(userIds, r.CustomerUserId)
	}
	names := map[int]string{}
	if len(orgIds) > 0 {
		var orgs []Organization
		if err := DB.Select("id", "name").Where("id IN ?", orgIds).Find(&orgs).Error; err == nil {
			for _, o := range orgs {
				names[o.Id] = o.Name
			}
		}
	}
	emails := UserDisplayLabelsByIds(userIds)
	for _, r := range rows {
		r.Discounts = ParseRetailDiscounts(r.RetailDiscounts)
		r.Models = make([]string, 0)
		for m := range parseAllowedModels(r.AllowedModels) {
			r.Models = append(r.Models, m)
		}
		r.ResellerName = names[r.ResellerOrgId]
		r.CustomerName = names[r.CustomerOrgId]
		if r.CustomerName == "" {
			r.CustomerName = r.OrgName
		}
		r.CustomerEmail = emails[r.CustomerUserId]
		if r.CustomerEmail == "" {
			r.CustomerEmail = r.InvitedEmail
		}
		if !r.isOpen() && (r.Status == BindStatusPendingReseller || r.Status == BindStatusPendingCustomer) {
			r.Status = BindStatusExpired
		}
	}
}

// bindableReseller is the reseller a request names, when it may still take on
// an existing organization: active, and allowed self-funded customers.
func bindableReseller(tx *gorm.DB, resellerOrgId int) (*Organization, error) {
	var reseller Organization
	err := tx.Where("id = ?", resellerOrgId).First(&reseller).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBindNotOffered
	}
	if err != nil {
		return nil, err
	}
	if reseller.Type != OrgTypeReseller || reseller.Status != OrgStatusActive || !reseller.DirectPayEnabled {
		return nil, ErrBindNotOffered
	}
	return &reseller, nil
}

// BindCandidate is who would become the reseller's customer: an organization,
// through its owner, or a person who has no organization and gets one.
type BindCandidate struct {
	UserId int
	Org    *Organization
}

// resolveBindCandidate finds what the user would bring to the reseller and
// refuses what cannot be bound: an organization the user does not own, one
// that is not an ordinary active enterprise, one that already has a reseller,
// one with its own upstream keys, and the reseller's own administrators.
func resolveBindCandidate(tx *gorm.DB, resellerOrgId, userId int) (*BindCandidate, error) {
	var ownAdmin int64
	if err := tx.Model(&ResellerAdmin{}).Where("user_id = ? AND reseller_org_id = ?", userId, resellerOrgId).Count(&ownAdmin).Error; err != nil {
		return nil, err
	}
	if ownAdmin > 0 {
		return nil, ErrBindOwnDistributor
	}
	var accounts []OrgAccount
	if err := tx.Where("user_id = ?", userId).Limit(1).Find(&accounts).Error; err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		var byok int64
		if err := tx.Model(&UserChannel{}).Where("user_id = ?", userId).Count(&byok).Error; err != nil {
			return nil, err
		}
		if byok > 0 {
			return nil, ErrBindHasByok
		}
		return &BindCandidate{UserId: userId}, nil
	}
	acc := accounts[0]
	if acc.Role != OrgRoleOwner || acc.Status == OrgStatusSuspended {
		return nil, ErrBindOwnerOnly
	}
	var org Organization
	if err := tx.Where("id = ?", acc.OrgId).First(&org).Error; err != nil {
		return nil, err
	}
	if org.Type != OrgTypeEnterprise || org.Status != OrgStatusActive || (org.PriceGroup != "" && org.PriceGroup != "default") {
		return nil, ErrBindOrgNotEligible
	}
	var linked int64
	if err := tx.Model(&ResellerCustomerLink{}).Where("customer_org_id = ?", org.Id).Count(&linked).Error; err != nil {
		return nil, err
	}
	if linked > 0 {
		return nil, ErrBindAlreadyCustomer
	}
	var byok int64
	if err := tx.Model(&OrgChannel{}).Where("org_id = ?", org.Id).Count(&byok).Error; err != nil {
		return nil, err
	}
	if byok > 0 {
		return nil, ErrBindHasByok
	}
	return &BindCandidate{UserId: userId, Org: &org}, nil
}

// bindNeedsPlatformReview reports whether the candidate has paid the platform
// before: an organization whose wallet was ever credited by the platform or
// still holds a balance, a person with a completed payment or a balance to
// bring along.
func bindNeedsPlatformReview(tx *gorm.DB, cand *BindCandidate, transferBalance bool) (bool, error) {
	if cand.Org != nil {
		if cand.Org.WalletQuota > 0 {
			return true, nil
		}
		var credited int64
		err := tx.Model(&CreditLedger{}).
			Where("to_org_id = ? AND type IN ?", cand.Org.Id, []string{LedgerTypePurchase, LedgerTypeTopUp}).
			Count(&credited).Error
		return credited > 0, err
	}
	if transferBalance {
		var user User
		if err := tx.Select("id", "quota").Where("id = ?", cand.UserId).First(&user).Error; err != nil {
			return false, err
		}
		if user.Quota > 0 {
			return true, nil
		}
	}
	var paid int64
	err := tx.Model(&TopUp{}).
		Where("user_id = ? AND status = ? AND money > 0", cand.UserId, common.TopUpStatusSuccess).
		Count(&paid).Error
	return paid > 0, err
}

// ResellerBindOutlook is what binding would mean for the user, shown before
// they consent.
type ResellerBindOutlook struct {
	// HasOrg: an organization is bound; otherwise one is opened for the person.
	HasOrg  bool   `json:"has_org"`
	OrgName string `json:"org_name"`
	// FundingChanges: the organization starts paying by online top-up.
	FundingChanges bool `json:"funding_changes"`
	// NeedsReview: the platform approves this binding whatever the person
	// does with a personal balance; bringing one along needs approval too.
	NeedsReview bool `json:"needs_review"`
	// Members whose personal API keys stop working, and how many keys that is.
	Members      int `json:"members"`
	PersonalKeys int `json:"personal_keys"`
	// PersonalQuota is the person's own balance, which can move to the wallet.
	PersonalQuota int `json:"personal_quota"`
}

// ResellerBindOutlookFor resolves what binding to the reseller would change for
// the user, or the reason it cannot happen.
func ResellerBindOutlookFor(resellerOrgId, userId int) (*ResellerBindOutlook, error) {
	if _, err := bindableReseller(DB, resellerOrgId); err != nil {
		return nil, err
	}
	cand, err := resolveBindCandidate(DB, resellerOrgId, userId)
	if err != nil {
		return nil, err
	}
	outlook := &ResellerBindOutlook{}
	memberIds := []int{userId}
	if cand.Org != nil {
		outlook.HasOrg = true
		outlook.OrgName = cand.Org.Name
		outlook.FundingChanges = cand.Org.FundingMode != OrgFundingDirect
		if err := DB.Model(&OrgAccount{}).Where("org_id = ?", cand.Org.Id).Pluck("user_id", &memberIds).Error; err != nil {
			return nil, err
		}
	} else {
		var user User
		if err := DB.Select("id", "quota").Where("id = ?", userId).First(&user).Error; err != nil {
			return nil, err
		}
		outlook.PersonalQuota = max(user.Quota, 0)
	}
	outlook.Members = len(memberIds)
	var keys int64
	if err := DB.Model(&Token{}).
		Where("user_id IN ? AND status = ?", memberIds, common.TokenStatusEnabled).
		Where("id NOT IN (?)", DB.Table("workspace_tokens").Select("token_id")).
		Count(&keys).Error; err != nil {
		return nil, err
	}
	outlook.PersonalKeys = int(keys)
	if outlook.NeedsReview, err = bindNeedsPlatformReview(DB, cand, false); err != nil {
		return nil, err
	}
	return outlook, nil
}

// ---------------------------------------------------------------------------
// Join code
// ---------------------------------------------------------------------------

// GetResellerJoinCode returns the reseller's join code, issuing one (enabled)
// the first time it is asked for.
func GetResellerJoinCode(resellerOrgId int) (*ResellerJoinCode, error) {
	var codes []ResellerJoinCode
	if err := DB.Where("reseller_org_id = ?", resellerOrgId).Limit(1).Find(&codes).Error; err != nil {
		return nil, err
	}
	if len(codes) > 0 {
		return &codes[0], nil
	}
	now := common.GetTimestamp()
	code := &ResellerJoinCode{ResellerOrgId: resellerOrgId, Code: common.GetRandomString(16), Enabled: true, CreatedTime: now, UpdatedTime: now}
	if err := DB.Create(code).Error; err != nil {
		return nil, err
	}
	return code, nil
}

// UpdateResellerJoinCode switches the join code on or off, or replaces it with
// a fresh one so links already handed out stop working.
func UpdateResellerJoinCode(resellerOrgId int, enabled *bool, rotate bool) (*ResellerJoinCode, error) {
	code, err := GetResellerJoinCode(resellerOrgId)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"updated_time": common.GetTimestamp()}
	if enabled != nil {
		code.Enabled = *enabled
		fields["enabled"] = *enabled
	}
	if rotate {
		code.Code = common.GetRandomString(16)
		fields["code"] = code.Code
	}
	if err := DB.Model(&ResellerJoinCode{}).Where("id = ?", code.Id).Updates(fields).Error; err != nil {
		return nil, err
	}
	return code, nil
}

// ResellerOrgIdByJoinCode resolves a join code that still accepts applications.
func ResellerOrgIdByJoinCode(code string) (int, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, ErrJoinCodeInvalid
	}
	var codes []ResellerJoinCode
	if err := DB.Where("code = ?", code).Limit(1).Find(&codes).Error; err != nil {
		return 0, err
	}
	if len(codes) == 0 || !codes[0].Enabled {
		return 0, ErrJoinCodeInvalid
	}
	return codes[0].ResellerOrgId, nil
}

// ---------------------------------------------------------------------------
// The two ways a request starts
// ---------------------------------------------------------------------------

// CreateResellerBindInvite records a reseller's invitation to the organization
// or person behind an email address, with the offer it makes them. Nothing
// about the address is looked up, so the reseller learns nothing about who
// is on the platform.
func CreateResellerBindInvite(resellerOrgId, operatorId int, email, discounts, models string) (*ResellerBindRequest, error) {
	email = NormalizeEmail(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, ErrBindEmailInvalid
	}
	var req *ResellerBindRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := bindableReseller(tx, resellerOrgId); err != nil {
			return err
		}
		var pending int64
		if err := openBindRequests(tx).
			Where("reseller_org_id = ? AND invited_email = ?", resellerOrgId, email).
			Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return ErrBindAlreadyInvited
		}
		now := time.Now()
		req = &ResellerBindRequest{
			ResellerOrgId: resellerOrgId, Direction: BindDirectionInvite, InvitedEmail: email,
			Code: common.GetUUID(), RetailDiscounts: discounts, AllowedModels: models,
			Status: BindStatusPendingCustomer, CreatedBy: operatorId, ResellerOperatorId: operatorId,
			ExpiresAt: now.Add(resellerBindTTL).Unix(), CreatedTime: now.Unix(),
		}
		return tx.Create(req).Error
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}

// otherOpenBindRequest reports whether the customer side already has another
// request under way: one the user started or answered, or one for the
// organization.
func otherOpenBindRequest(tx *gorm.DB, exceptId, userId, orgId int) (bool, error) {
	q := openBindRequests(tx).Where("id <> ?", exceptId)
	if orgId > 0 {
		q = q.Where("(customer_user_id = ? OR customer_org_id = ?)", userId, orgId)
	} else {
		q = q.Where("customer_user_id = ?", userId)
	}
	var n int64
	err := q.Count(&n).Error
	return n > 0, err
}

// ApplyToReseller records an application to become the customer of the
// reseller a join code belongs to. The reseller answers it with an offer,
// which the applicant then confirms.
func ApplyToReseller(joinCode string, userId int, remark string) (*ResellerBindRequest, error) {
	resellerOrgId, err := ResellerOrgIdByJoinCode(joinCode)
	if err != nil {
		return nil, err
	}
	var req *ResellerBindRequest
	err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := bindableReseller(tx, resellerOrgId); err != nil {
			return err
		}
		cand, err := resolveBindCandidate(tx, resellerOrgId, userId)
		if err != nil {
			return err
		}
		orgId := 0
		if cand.Org != nil {
			orgId = cand.Org.Id
		}
		if pending, err := otherOpenBindRequest(tx, 0, userId, orgId); err != nil {
			return err
		} else if pending {
			return ErrBindRequestPending
		}
		now := time.Now()
		req = &ResellerBindRequest{
			ResellerOrgId: resellerOrgId, Direction: BindDirectionApply,
			CustomerOrgId: orgId, CustomerUserId: userId, Code: common.GetUUID(),
			Status: BindStatusPendingReseller, Remark: strings.TrimSpace(remark), CreatedBy: userId,
			ExpiresAt: now.Add(resellerBindTTL).Unix(), CreatedTime: now.Unix(),
		}
		return tx.Create(req).Error
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}

// ---------------------------------------------------------------------------
// Answers
// ---------------------------------------------------------------------------

// lockOpenBindRequest loads a request for update and refuses one that is not
// waiting in the given status any more.
func lockOpenBindRequest(tx *gorm.DB, status string, query string, args ...any) (*ResellerBindRequest, error) {
	var req ResellerBindRequest
	err := lockForUpdate(tx).Where(query, args...).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBindRequestGone
	}
	if err != nil {
		return nil, err
	}
	if req.Status != status || !req.isOpen() {
		return nil, ErrBindRequestGone
	}
	return &req, nil
}

// closeBindRequest ends a request without binding anything.
func closeBindRequest(tx *gorm.DB, req *ResellerBindRequest, status, note string, fields map[string]any) error {
	updates := map[string]any{"status": status, "review_note": note, "processed_at": common.GetTimestamp()}
	for k, v := range fields {
		updates[k] = v
	}
	req.Status = status
	return tx.Model(&ResellerBindRequest{}).Where("id = ?", req.Id).Updates(updates).Error
}

// OfferResellerBinding is the reseller's yes to an application: the offer it
// makes, which the applicant confirms within a new seven-day window.
func OfferResellerBinding(resellerOrgId, reqId, operatorId int, discounts, models string) (*ResellerBindRequest, error) {
	var req *ResellerBindRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if req, err = lockOpenBindRequest(tx, BindStatusPendingReseller, "id = ? AND reseller_org_id = ?", reqId, resellerOrgId); err != nil {
			return err
		}
		if _, err := bindableReseller(tx, resellerOrgId); err != nil {
			return err
		}
		req.Status = BindStatusPendingCustomer
		req.RetailDiscounts, req.AllowedModels = discounts, models
		req.ExpiresAt = time.Now().Add(resellerBindTTL).Unix()
		return tx.Model(&ResellerBindRequest{}).Where("id = ?", req.Id).Updates(map[string]any{
			"status": req.Status, "retail_discounts": discounts, "allowed_models": models,
			"reseller_operator_id": operatorId, "expires_at": req.ExpiresAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}

// CloseResellerBindRequestAsReseller is the reseller's no: it rejects an
// application, or withdraws an offer the customer has not answered yet.
func CloseResellerBindRequestAsReseller(resellerOrgId, reqId, operatorId int, note string) (*ResellerBindRequest, error) {
	var req *ResellerBindRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		var loaded ResellerBindRequest
		err := lockForUpdate(tx).Where("id = ? AND reseller_org_id = ?", reqId, resellerOrgId).First(&loaded).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBindRequestGone
		}
		if err != nil {
			return err
		}
		req = &loaded
		status := BindStatusRevoked
		if req.Status == BindStatusPendingReseller {
			status = BindStatusRejected
		} else if req.Status != BindStatusPendingCustomer {
			return ErrBindRequestGone
		}
		return closeBindRequest(tx, req, status, note, map[string]any{"reseller_operator_id": operatorId})
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}

// CloseResellerBindRequestAsCustomer is the customer's no: the applicant (or
// whoever consented for the customer) withdraws a request at any open stage,
// and the invited address turns an invitation down.
func CloseResellerBindRequestAsCustomer(reqId, userId int) (*ResellerBindRequest, error) {
	var req *ResellerBindRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		var loaded ResellerBindRequest
		err := lockForUpdate(tx).Where("id = ?", reqId).First(&loaded).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBindRequestGone
		}
		if err != nil {
			return err
		}
		req = &loaded
		if !req.isOpen() {
			return ErrBindRequestGone
		}
		if req.CustomerUserId == userId {
			return closeBindRequest(tx, req, BindStatusRevoked, "", nil)
		}
		// An invitation nobody has answered belongs to the invited address.
		if req.Direction != BindDirectionInvite || req.CustomerUserId != 0 {
			return ErrBindRequestGone
		}
		var user User
		if err := tx.Select("id", "email").Where("id = ?", userId).First(&user).Error; err != nil {
			return ErrBindRequestGone
		}
		if NormalizeEmail(user.Email) != req.InvitedEmail {
			return ErrBindRequestGone
		}
		return closeBindRequest(tx, req, BindStatusRejected, "", map[string]any{"customer_user_id": userId})
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}

// AcceptResellerBinding is the customer's yes to an offer. The binding takes
// effect at once, unless the customer has paid the platform before: then it
// waits for the platform. orgName and transferBalance apply to a person, for
// whom an organization is opened.
func AcceptResellerBinding(code string, userId int, orgName string, transferBalance bool) (*ResellerBindRequest, error) {
	var req *ResellerBindRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if req, err = lockOpenBindRequest(tx, BindStatusPendingCustomer, "code = ?", strings.TrimSpace(code)); err != nil {
			return err
		}
		if req.Direction == BindDirectionInvite {
			var user User
			if err := tx.Select("id", "email").Where("id = ?", userId).First(&user).Error; err != nil {
				return err
			}
			if NormalizeEmail(user.Email) != req.InvitedEmail {
				return common.NewMessage("This invitation is for {{email}}; sign in with that account to answer it", map[string]any{"email": req.InvitedEmail})
			}
		} else if req.CustomerUserId != userId {
			return ErrBindRequestGone
		}
		if _, err := bindableReseller(tx, req.ResellerOrgId); err != nil {
			return err
		}
		cand, err := resolveBindCandidate(tx, req.ResellerOrgId, userId)
		if err != nil {
			return err
		}
		req.CustomerUserId = userId
		req.CustomerOrgId, req.OrgName, req.TransferBalance = 0, "", false
		if cand.Org != nil {
			req.CustomerOrgId = cand.Org.Id
		} else {
			req.OrgName = strings.TrimSpace(orgName)
			if err := ValidateOrgName(req.OrgName); err != nil {
				return err
			}
			if taken, err := orgNameTaken(tx, req.OrgName, 0); err != nil {
				return err
			} else if taken {
				return ErrOrgNameTaken
			}
			req.TransferBalance = transferBalance
		}
		if pending, err := otherOpenBindRequest(tx, req.Id, userId, req.CustomerOrgId); err != nil {
			return err
		} else if pending {
			return ErrBindRequestPending
		}
		if req.NeedsReview, err = bindNeedsPlatformReview(tx, cand, req.TransferBalance); err != nil {
			return err
		}
		consent := map[string]any{
			"customer_user_id": req.CustomerUserId, "customer_org_id": req.CustomerOrgId,
			"org_name": req.OrgName, "transfer_balance": req.TransferBalance, "needs_review": req.NeedsReview,
		}
		if req.NeedsReview {
			req.Status, req.ExpiresAt = BindStatusPendingPlatform, 0
			consent["status"], consent["expires_at"] = req.Status, 0
			return tx.Model(&ResellerBindRequest{}).Where("id = ?", req.Id).Updates(consent).Error
		}
		if err := tx.Model(&ResellerBindRequest{}).Where("id = ?", req.Id).Updates(consent).Error; err != nil {
			return err
		}
		return activateResellerBinding(tx, req)
	})
	if err != nil {
		return nil, err
	}
	settleResellerBinding(req)
	return req, nil
}

// ReviewResellerBinding is the platform's answer for a customer that has paid
// it before: approving binds, rejecting ends the request.
func ReviewResellerBinding(reqId, reviewerId int, approve bool, note string) (*ResellerBindRequest, error) {
	var req *ResellerBindRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if req, err = lockOpenBindRequest(tx, BindStatusPendingPlatform, "id = ?", reqId); err != nil {
			return err
		}
		if !approve {
			return closeBindRequest(tx, req, BindStatusRejected, note, map[string]any{"reviewer_id": reviewerId})
		}
		if err := tx.Model(&ResellerBindRequest{}).Where("id = ?", req.Id).
			Updates(map[string]any{"reviewer_id": reviewerId, "review_note": note}).Error; err != nil {
			return err
		}
		return activateResellerBinding(tx, req)
	})
	if err != nil {
		return nil, err
	}
	settleResellerBinding(req)
	return req, nil
}

// activateResellerBinding makes the binding real, inside the caller's
// transaction: a person gets an organization (and, if asked, their balance in
// its wallet), the organization starts paying for itself under the reseller's
// offer, and the link that makes it the reseller's customer is written. The
// customer side is resolved again, so whatever changed since it consented —
// it joined another reseller, gave up ownership — stops the binding here.
func activateResellerBinding(tx *gorm.DB, req *ResellerBindRequest) error {
	if _, err := bindableReseller(tx, req.ResellerOrgId); err != nil {
		return err
	}
	cand, err := resolveBindCandidate(tx, req.ResellerOrgId, req.CustomerUserId)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	if cand.Org != nil {
		if cand.Org.Id != req.CustomerOrgId {
			return ErrBindRequestGone
		}
		// Queue behind anything else changing this organization.
		var locked Organization
		if err := lockForUpdate(tx).Select("id").Where("id = ?", cand.Org.Id).First(&locked).Error; err != nil {
			return err
		}
	} else {
		if req.CustomerOrgId != 0 {
			return ErrBindRequestGone
		}
		if taken, err := orgNameTaken(tx, req.OrgName, 0); err != nil {
			return err
		} else if taken {
			return ErrOrgNameTaken
		}
		org := &Organization{
			Name: req.OrgName, Type: OrgTypeEnterprise, Status: OrgStatusActive, PriceGroup: "default",
			OwnerUserId: req.CustomerUserId, CreatedTime: now, UpdatedTime: now,
		}
		if err := tx.Create(org).Error; err != nil {
			return err
		}
		if err := tx.Create(&OrgAccount{
			OrgId: org.Id, UserId: req.CustomerUserId, Relation: OrgRelationMember, Role: OrgRoleOwner,
			Status: OrgStatusActive, PeriodKey: currentPeriodKey(), CreatedTime: now,
		}).Error; err != nil {
			return err
		}
		req.CustomerOrgId = org.Id
		if req.TransferBalance {
			if err := movePersonalBalanceToOrg(tx, req); err != nil {
				return err
			}
		}
	}
	if err := tx.Model(&Organization{}).Where("id = ?", req.CustomerOrgId).Updates(map[string]any{
		"funding_mode": OrgFundingDirect, "retail_discounts": req.RetailDiscounts,
		"allowed_models": req.AllowedModels, "updated_time": now,
	}).Error; err != nil {
		return err
	}
	if err := tx.Create(&ResellerCustomerLink{ResellerOrgId: req.ResellerOrgId, CustomerOrgId: req.CustomerOrgId, CreatedTime: now}).Error; err != nil {
		return err
	}
	req.Status = BindStatusCompleted
	return tx.Model(&ResellerBindRequest{}).Where("id = ?", req.Id).Updates(map[string]any{
		"status": req.Status, "customer_org_id": req.CustomerOrgId, "transfer_quota": req.TransferQuota,
		"expires_at": 0, "processed_at": now,
	}).Error
}

// movePersonalBalanceToOrg moves everything the person holds into the wallet
// of the organization just opened for them. The balance is read under a row
// lock and taken with a conditional update, so it cannot go negative.
func movePersonalBalanceToOrg(tx *gorm.DB, req *ResellerBindRequest) error {
	var user User
	if err := lockForUpdate(tx).Select("id", "quota").Where("id = ?", req.CustomerUserId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota <= 0 {
		return nil
	}
	if err := common.ValidateWalletQuota(user.Quota); err != nil {
		return err
	}
	taken := tx.Model(&User{}).Where("id = ? AND quota >= ?", user.Id, user.Quota).
		Update("quota", gorm.Expr("quota - ?", user.Quota))
	if taken.Error != nil {
		return taken.Error
	}
	if taken.RowsAffected != 1 {
		return ErrBindRequestGone
	}
	if err := tx.Model(&Organization{}).Where("id = ?", req.CustomerOrgId).
		Update("wallet_quota", gorm.Expr("wallet_quota + ?", user.Quota)).Error; err != nil {
		return err
	}
	req.TransferQuota = user.Quota
	return insertLedger(tx, 0, req.CustomerOrgId, user.Quota, user.Id, LedgerTypeTransfer, "", "personal balance moved in on joining a distributor")
}

// settleResellerBinding does what follows a committed binding: the members now
// belong to a reseller's customer, so their payer records are re-read, their
// personal keys stop, and a moved balance leaves the quota cache.
func settleResellerBinding(req *ResellerBindRequest) {
	if req.Status != BindStatusCompleted {
		return
	}
	InvalidateOrgPayerCacheForOrg(req.CustomerOrgId)
	var memberIds []int
	if err := DB.Model(&OrgAccount{}).Where("org_id = ?", req.CustomerOrgId).Pluck("user_id", &memberIds).Error; err != nil {
		common.SysError("reseller binding: list members failed: " + err.Error())
	}
	for _, memberId := range memberIds {
		if _, err := DisablePersonalTokens(memberId); err != nil {
			common.SysError("reseller binding: disable personal tokens failed: " + err.Error())
		}
	}
	if req.TransferQuota > 0 {
		if err := cacheDecrUserQuota(req.CustomerUserId, int64(req.TransferQuota)); err != nil {
			common.SysLog("reseller binding: quota cache decrement failed: " + err.Error())
		}
	}
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GetResellerBindRequestByCode returns the request a confirmation link opens,
// or nil.
func GetResellerBindRequestByCode(code string) (*ResellerBindRequest, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	var rows []*ResellerBindRequest
	if err := DB.Where("code = ?", code).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	describeBindRequests(rows)
	return rows[0], nil
}

// GetResellerBindRequestById returns one request with its computed fields, or
// nil.
func GetResellerBindRequestById(id int) (*ResellerBindRequest, error) {
	var rows []*ResellerBindRequest
	if err := DB.Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	describeBindRequests(rows)
	return rows[0], nil
}

// ListResellerBindRequests returns a reseller's requests, newest first.
func ListResellerBindRequests(resellerOrgId int) ([]*ResellerBindRequest, error) {
	rows := make([]*ResellerBindRequest, 0)
	if err := DB.Where("reseller_org_id = ?", resellerOrgId).Order("id DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	describeBindRequests(rows)
	return rows, nil
}

// ListResellerBindRequestsForReview returns the requests of every reseller in
// one status (all when empty), newest first, for the platform.
func ListResellerBindRequestsForReview(status string, offset, limit int) ([]*ResellerBindRequest, int64, error) {
	q := DB.Model(&ResellerBindRequest{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]*ResellerBindRequest, 0)
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	describeBindRequests(rows)
	return rows, total, nil
}

// OpenResellerBindRequestsOfUser returns what is waiting on or for the user:
// requests they started or answered, and invitations addressed to their email.
func OpenResellerBindRequestsOfUser(userId int) ([]*ResellerBindRequest, error) {
	var user User
	if err := DB.Select("id", "email").Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}
	q := openBindRequests(DB)
	if email := NormalizeEmail(user.Email); email != "" {
		q = q.Where("(customer_user_id = ? OR (customer_user_id = 0 AND direction = ? AND invited_email = ?))", userId, BindDirectionInvite, email)
	} else {
		q = q.Where("customer_user_id = ?", userId)
	}
	rows := make([]*ResellerBindRequest, 0)
	if err := q.Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	describeBindRequests(rows)
	return rows, nil
}
