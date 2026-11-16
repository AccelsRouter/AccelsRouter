package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindFixture is a reseller allowed to have self-funded customers, and helpers
// for the people and organizations it tries to bind.
type bindFixture struct {
	t        *testing.T
	reseller *Organization
}

func newBindFixture(t *testing.T) *bindFixture {
	t.Helper()
	migrateOrgTables(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Token{}, &TopUp{}, &UserChannel{}, &ResellerBindRequest{}, &ResellerJoinCode{}))
	for _, table := range []string{"reseller_bind_requests", "reseller_join_codes", "user_channels", "org_channels"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	reseller := mustCreateOrg(t, "Bind Reseller", OrgTypeReseller, 0)
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]any{"direct_pay_enabled": true}))
	return &bindFixture{t: t, reseller: reseller}
}

func (f *bindFixture) user(name string, quota int) *User {
	f.t.Helper()
	u := &User{Username: name, Email: name + "@bind.test", AffCode: name + "-aff", Quota: quota}
	require.NoError(f.t, DB.Create(u).Error)
	return u
}

// ownedOrg is an enterprise organization with its owner.
func (f *bindFixture) ownedOrg(name string) (*Organization, *User) {
	f.t.Helper()
	owner := f.user(name+"-owner", 0)
	org := &Organization{Name: name, Type: OrgTypeEnterprise, OwnerUserId: owner.Id, PriceGroup: "default"}
	require.NoError(f.t, CreateOrganization(org))
	require.NoError(f.t, AttachOrgAccount(&OrgAccount{OrgId: org.Id, UserId: owner.Id, Relation: OrgRelationMember, Role: OrgRoleOwner}))
	return org, owner
}

func (f *bindFixture) invite(email string) *ResellerBindRequest {
	f.t.Helper()
	req, err := CreateResellerBindInvite(f.reseller.Id, 1, email, `{"gpt-":0.9}`, `["gpt-4o"]`)
	require.NoError(f.t, err)
	return req
}

func (f *bindFixture) resellerOf(orgId int) int {
	id, _ := ResellerOrgIdForCustomer(orgId)
	return id
}

// An organization that never paid the platform is bound the moment its owner
// accepts: it becomes the reseller's customer under the offer, pays for itself
// from then on, and its members' personal keys stop.
func TestResellerInviteBindsAnOrganizationOnItsOwnersConsent(t *testing.T) {
	f := newBindFixture(t)
	org, owner := f.ownedOrg("Bind Fresh")
	personalKey := &Token{UserId: owner.Id, Key: "bind-fresh-personal-key", Status: common.TokenStatusEnabled, Name: "personal"}
	require.NoError(t, DB.Create(personalKey).Error)
	req := f.invite("  " + "Bind Fresh-owner@BIND.test ")
	assert.Equal(t, "bind fresh-owner@bind.test", req.InvitedEmail)

	_, err := CreateResellerBindInvite(f.reseller.Id, 1, owner.Email, "", "")
	require.ErrorIs(t, err, ErrBindAlreadyInvited)

	stranger := f.user("bind-stranger", 0)
	_, err = AcceptResellerBinding(req.Code, stranger.Id, "", false)
	require.Error(t, err, "only the invited address answers an invitation")
	assert.Zero(t, f.resellerOf(org.Id))

	done, err := AcceptResellerBinding(req.Code, owner.Id, "", false)
	require.NoError(t, err)
	assert.Equal(t, BindStatusCompleted, done.Status)
	assert.False(t, done.NeedsReview)
	assert.Equal(t, f.reseller.Id, f.resellerOf(org.Id))
	bound, err := GetOrganizationById(org.Id)
	require.NoError(t, err)
	assert.Equal(t, OrgFundingDirect, bound.FundingMode)
	assert.Equal(t, map[string]float64{"gpt-": 0.9}, ParseRetailDiscounts(bound.RetailDiscounts))
	assert.Equal(t, []string{"gpt-4o"}, bound.AllowedModelList())
	var key Token
	require.NoError(t, DB.First(&key, personalKey.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, key.Status)

	_, err = AcceptResellerBinding(req.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindRequestGone, "an answered request cannot be answered again")
}

// An organization that has paid the platform before waits for the platform,
// and nothing changes until it approves; a rejection leaves it as it was.
func TestResellerBindingOfAPayingOrganizationWaitsForThePlatform(t *testing.T) {
	f := newBindFixture(t)
	org, owner := f.ownedOrg("Bind Paying")
	require.NoError(t, PlatformCreditOrg(org.Id, 5000, 1, "bind-paying-credit", "offline payment"))

	req, err := AcceptResellerBinding(f.invite(owner.Email).Code, owner.Id, "", false)
	require.NoError(t, err)
	assert.Equal(t, BindStatusPendingPlatform, req.Status)
	assert.True(t, req.NeedsReview)
	assert.Zero(t, f.resellerOf(org.Id))
	pending, total, err := ListResellerBindRequestsForReview(BindStatusPendingPlatform, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Equal(t, "Bind Paying", pending[0].CustomerName)

	// A second reseller cannot line up behind a request under review.
	other := mustCreateOrg(t, "Bind Other Reseller", OrgTypeReseller, 0)
	require.NoError(t, UpdateOrganizationFields(other.Id, map[string]any{"direct_pay_enabled": true}))
	rival, err := CreateResellerBindInvite(other.Id, 1, owner.Email, "", "")
	require.NoError(t, err)
	_, err = AcceptResellerBinding(rival.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindRequestPending)

	rejected, err := ReviewResellerBinding(req.Id, 9, false, "keeps its direct contract")
	require.NoError(t, err)
	assert.Equal(t, BindStatusRejected, rejected.Status)
	assert.Zero(t, f.resellerOf(org.Id))
	untouched, err := GetOrganizationById(org.Id)
	require.NoError(t, err)
	assert.Empty(t, untouched.FundingMode)

	again, err := AcceptResellerBinding(f.invite(owner.Email).Code, owner.Id, "", false)
	require.NoError(t, err)
	approved, err := ReviewResellerBinding(again.Id, 9, true, "")
	require.NoError(t, err)
	assert.Equal(t, BindStatusCompleted, approved.Status)
	assert.Equal(t, f.reseller.Id, f.resellerOf(org.Id))
	kept, err := GetOrganizationById(org.Id)
	require.NoError(t, err)
	assert.Equal(t, 5000, kept.WalletQuota, "the organization keeps its own money")
	assert.Equal(t, OrgFundingDirect, kept.FundingMode)
}

// What cannot be bound is refused before anything is written.
func TestResellerBindingRefusals(t *testing.T) {
	f := newBindFixture(t)

	plain := mustCreateOrg(t, "Bind Plain Reseller", OrgTypeReseller, 0)
	_, err := CreateResellerBindInvite(plain.Id, 1, "anyone@bind.test", "", "")
	require.ErrorIs(t, err, ErrBindNotOffered, "a reseller without self-funded customers cannot bind one")

	org, owner := f.ownedOrg("Bind Guarded")
	member := f.user("bind-member", 0)
	require.NoError(t, AttachOrgAccount(&OrgAccount{OrgId: org.Id, UserId: member.Id, Relation: OrgRelationMember, Role: OrgRoleAdmin}))
	_, err = AcceptResellerBinding(f.invite(member.Email).Code, member.Id, "", false)
	require.ErrorIs(t, err, ErrBindOwnerOnly)

	require.NoError(t, AddOrgChannel(org.Id, 77001))
	ownerInvite := f.invite(owner.Email)
	_, err = AcceptResellerBinding(ownerInvite.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindHasByok)
	require.NoError(t, RemoveOrgChannel(org.Id, 77001))

	require.NoError(t, UpdateOrganizationFields(org.Id, map[string]any{"price_group": "negotiated"}))
	_, err = AcceptResellerBinding(ownerInvite.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindOrgNotEligible)
	require.NoError(t, UpdateOrganizationFields(org.Id, map[string]any{"price_group": "default"}))

	// The reseller's own administrator cannot become its customer.
	require.NoError(t, DB.Create(&ResellerAdmin{UserId: owner.Id, ResellerOrgId: f.reseller.Id, Status: OrgStatusActive}).Error)
	_, err = AcceptResellerBinding(ownerInvite.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindOwnDistributor)
	require.NoError(t, DB.Where("user_id = ?", owner.Id).Delete(&ResellerAdmin{}).Error)

	// An invitation nobody answered in time is gone, and is listed as expired.
	require.NoError(t, DB.Model(&ResellerBindRequest{}).Where("id = ?", ownerInvite.Id).Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	_, err = AcceptResellerBinding(ownerInvite.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindRequestGone)
	listed, err := ListResellerBindRequests(f.reseller.Id)
	require.NoError(t, err)
	statuses := map[int]string{}
	for _, row := range listed {
		statuses[row.Id] = row.Status
	}
	assert.Equal(t, BindStatusExpired, statuses[ownerInvite.Id])

	// The reseller withdraws an offer; the customer turns one down.
	withdrawn := f.invite(owner.Email)
	_, err = CloseResellerBindRequestAsReseller(f.reseller.Id, withdrawn.Id, 1, "")
	require.NoError(t, err)
	_, err = AcceptResellerBinding(withdrawn.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindRequestGone)
	declined := f.invite(owner.Email)
	_, err = CloseResellerBindRequestAsCustomer(declined.Id, member.Id)
	require.ErrorIs(t, err, ErrBindRequestGone, "someone else's invitation is not theirs to decline")
	closed, err := CloseResellerBindRequestAsCustomer(declined.Id, owner.Id)
	require.NoError(t, err)
	assert.Equal(t, BindStatusRejected, closed.Status)

	// An organization that already has a reseller must leave it first.
	bound, err := AcceptResellerBinding(f.invite(owner.Email).Code, owner.Id, "", false)
	require.NoError(t, err)
	require.Equal(t, BindStatusCompleted, bound.Status)
	_, err = AcceptResellerBinding(f.invite(owner.Email).Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindAlreadyCustomer)
}

// The other direction: a customer applies with the reseller's join code, the
// reseller answers with an offer, and the applicant confirms it.
func TestApplyingToAResellerWithItsJoinCode(t *testing.T) {
	f := newBindFixture(t)
	org, owner := f.ownedOrg("Bind Applicant")

	code, err := GetResellerJoinCode(f.reseller.Id)
	require.NoError(t, err)
	same, err := GetResellerJoinCode(f.reseller.Id)
	require.NoError(t, err)
	assert.Equal(t, code.Code, same.Code)

	off := false
	_, err = UpdateResellerJoinCode(f.reseller.Id, &off, false)
	require.NoError(t, err)
	_, err = ApplyToReseller(code.Code, owner.Id, "")
	require.ErrorIs(t, err, ErrJoinCodeInvalid)
	on := true
	rotated, err := UpdateResellerJoinCode(f.reseller.Id, &on, true)
	require.NoError(t, err)
	_, err = ApplyToReseller(code.Code, owner.Id, "")
	require.ErrorIs(t, err, ErrJoinCodeInvalid, "a replaced code stops working")

	application, err := ApplyToReseller(rotated.Code, owner.Id, "we use about 2k a month")
	require.NoError(t, err)
	assert.Equal(t, BindStatusPendingReseller, application.Status)
	assert.Equal(t, org.Id, application.CustomerOrgId)
	_, err = ApplyToReseller(rotated.Code, owner.Id, "")
	require.ErrorIs(t, err, ErrBindRequestPending)
	_, err = AcceptResellerBinding(application.Code, owner.Id, "", false)
	require.ErrorIs(t, err, ErrBindRequestGone, "there is no offer to accept yet")

	offered, err := OfferResellerBinding(f.reseller.Id, application.Id, 1, `{"claude-":0.95}`, "")
	require.NoError(t, err)
	assert.Equal(t, BindStatusPendingCustomer, offered.Status)
	waiting, err := OpenResellerBindRequestsOfUser(owner.Id)
	require.NoError(t, err)
	require.Len(t, waiting, 1)
	assert.Equal(t, map[string]float64{"claude-": 0.95}, waiting[0].Discounts)

	outsider := f.user("bind-outsider", 0)
	_, err = AcceptResellerBinding(application.Code, outsider.Id, "Outsider Org", false)
	require.ErrorIs(t, err, ErrBindRequestGone, "an application is confirmed by the applicant alone")

	done, err := AcceptResellerBinding(application.Code, owner.Id, "", false)
	require.NoError(t, err)
	assert.Equal(t, BindStatusCompleted, done.Status)
	assert.Equal(t, f.reseller.Id, f.resellerOf(org.Id))

	// The applicant may withdraw while the reseller has not answered.
	_, second := f.ownedOrg("Bind Second Applicant")
	pending, err := ApplyToReseller(rotated.Code, second.Id, "")
	require.NoError(t, err)
	_, err = CloseResellerBindRequestAsCustomer(pending.Id, second.Id)
	require.NoError(t, err)
	_, err = OfferResellerBinding(f.reseller.Id, pending.Id, 1, "", "")
	require.ErrorIs(t, err, ErrBindRequestGone)
}

// A person without an organization gets one on binding and owns it. A balance
// they bring along moves into its wallet in full, after the platform approves.
func TestBindingAPersonOpensTheirOrganization(t *testing.T) {
	f := newBindFixture(t)

	newcomer := f.user("bind-newcomer", 0)
	invite := f.invite(newcomer.Email)
	_, err := AcceptResellerBinding(invite.Code, newcomer.Id, "x", false)
	require.Error(t, err, "the organization needs a valid name")
	done, err := AcceptResellerBinding(invite.Code, newcomer.Id, "Newcomer Studio", false)
	require.NoError(t, err)
	require.Equal(t, BindStatusCompleted, done.Status)
	acc, err := GetOrgAccountByUser(newcomer.Id)
	require.NoError(t, err)
	require.NotNil(t, acc)
	assert.Equal(t, OrgRoleOwner, acc.Role)
	opened, err := GetOrganizationById(acc.OrgId)
	require.NoError(t, err)
	assert.Equal(t, "Newcomer Studio", opened.Name)
	assert.Equal(t, OrgFundingDirect, opened.FundingMode)
	assert.Equal(t, newcomer.Id, opened.OwnerUserId)
	assert.Equal(t, f.reseller.Id, f.resellerOf(opened.Id))

	saver := f.user("bind-saver", 4200)
	outlook, err := ResellerBindOutlookFor(f.reseller.Id, saver.Id)
	require.NoError(t, err)
	assert.False(t, outlook.HasOrg)
	assert.Equal(t, 4200, outlook.PersonalQuota)
	assert.False(t, outlook.NeedsReview, "a balance that was never paid for needs approval only when it is brought along")

	waiting, err := AcceptResellerBinding(f.invite(saver.Email).Code, saver.Id, "Saver Labs", true)
	require.NoError(t, err)
	require.Equal(t, BindStatusPendingPlatform, waiting.Status)
	none, err := GetOrgAccountByUser(saver.Id)
	require.NoError(t, err)
	assert.Nil(t, none, "nothing is opened before the platform approves")

	approved, err := ReviewResellerBinding(waiting.Id, 9, true, "")
	require.NoError(t, err)
	assert.Equal(t, 4200, approved.TransferQuota)
	saverAcc, err := GetOrgAccountByUser(saver.Id)
	require.NoError(t, err)
	require.NotNil(t, saverAcc)
	wallet, err := GetOrganizationById(saverAcc.OrgId)
	require.NoError(t, err)
	assert.Equal(t, 4200, wallet.WalletQuota)
	var left User
	require.NoError(t, DB.Select("id", "quota").First(&left, saver.Id).Error)
	assert.Zero(t, left.Quota)
	var moved CreditLedger
	require.NoError(t, DB.Where("to_org_id = ? AND type = ?", wallet.Id, LedgerTypeTransfer).First(&moved).Error)
	assert.Equal(t, 4200, moved.Quota)
}
