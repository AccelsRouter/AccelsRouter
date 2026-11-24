package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What happens to a user's money or organization reaches the notifications of
// the people it concerns, once each, and stays unread until they have seen it.
func TestCustomNotifications(t *testing.T) {
	migrateOrgTables(t)
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&CustomNotification{}))
	require.NoError(t, DB.Exec("DELETE FROM custom_notifications").Error)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	textsOf := func(userId int) []string {
		rows, _, err := ListCustomNotifications(userId)
		require.NoError(t, err)
		texts := make([]string, 0, len(rows))
		for _, row := range rows {
			texts = append(texts, row.Message.Error())
		}
		return texts
	}

	// An organization that pays for itself, with an owner, a second admin, a
	// suspended admin and a plain member.
	reseller := mustCreateOrg(t, "acme-reseller", OrgTypeReseller, 1000)
	require.NoError(t, UpdateOrganizationFields(reseller.Id, map[string]any{"direct_pay_enabled": true}))
	org, err := CreateResellerCustomer(reseller.Id, "direct-one", "default", OrgFundingDirect, 0, 1)
	require.NoError(t, err)
	people := []struct {
		id           int
		name, role   string
		accountState string
	}{
		{821, "owner", OrgRoleOwner, OrgStatusActive},
		{822, "admin", OrgRoleAdmin, OrgStatusActive},
		{823, "suspended", OrgRoleAdmin, OrgStatusSuspended},
		{824, "member", OrgRoleMember, OrgStatusActive},
	}
	for _, person := range people {
		require.NoError(t, DB.Create(&User{Id: person.id, Username: person.name, Email: person.name + "@example.com", Status: common.UserStatusEnabled, AffCode: person.name}).Error)
		require.NoError(t, AttachOrgAccount(&OrgAccount{OrgId: org.Id, UserId: person.id, Relation: OrgRelationCustomer, Role: person.role}))
		require.NoError(t, UpdateOrgAccountFields(org.Id, person.id, map[string]any{"status": person.accountState}))
	}

	// The owner pays online: the owner reads the top-up record, the other
	// admin reads who paid, and a repeated callback tells nobody again.
	order := createEpayTestOrder(t, 821, "EPAYNOTIFY", PaymentProviderEpay, common.TopUpStatusPending)
	for range 2 {
		_, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"Online top-up succeeded, amount added: ＄2.000000, payment amount: 10.000000"}, textsOf(821))
	assert.Equal(t, []string{"owner@example.com topped up organization direct-one by ＄2.000000"}, textsOf(822))
	assert.Empty(t, textsOf(823), "a suspended admin runs nothing")
	assert.Empty(t, textsOf(824), "a member does not run the organization")

	// An administrator's adjustment of a balance is told to its owner.
	require.NoError(t, RecordCreditGrantTopUp(824, 1500, ""))
	require.NoError(t, RecordCreditDeductionTopUp(824, 500, ""))
	assert.Equal(t, []string{
		"An administrator deducted ＄1.000000 from your balance",
		"An administrator added ＄3.000000 to your balance",
	}, textsOf(824), "newest first")

	// Whoever did it is not told, and a reseller's admins are those holding its
	// reseller-admin link.
	left := common.NewMessage("{{user}} left organization {{org}}", map[string]any{"user": "member@example.com", "org": org.Name})
	NotifyOrgAdmins(org.Id, 822, left)
	assert.Contains(t, textsOf(821), left.Error())
	assert.NotContains(t, textsOf(822), left.Error())
	require.NoError(t, DB.Create(&ResellerAdmin{UserId: 824, ResellerOrgId: reseller.Id, Status: OrgStatusActive}).Error)
	applied := common.NewMessage("{{user}} applied to become your customer", map[string]any{"user": "someone@example.com"})
	NotifyOrgAdmins(reseller.Id, 0, applied)
	assert.Equal(t, applied.Error(), textsOf(824)[0])

	// A condition that keeps recurring is told once until it has been read.
	empty := common.NewMessage("Requests are being refused: the wallet of organization {{org}} is empty", map[string]any{"org": org.Name})
	NotifyOrgAdminsOnce(org.Id, empty)
	NotifyOrgAdminsOnce(org.Id, empty)
	assert.Equal(t, []string{empty.Error(), left.Error(), "Online top-up succeeded, amount added: ＄2.000000, payment amount: 10.000000"}, textsOf(821))

	// Reading goes up to the newest notification that was shown: one that
	// arrived since stays unread, and it belongs to its user alone.
	shown, unread, err := ListCustomNotifications(821)
	require.NoError(t, err)
	assert.Equal(t, int64(3), unread)
	NotifyUsers([]int{821, 821, 0}, left)
	require.NoError(t, MarkCustomNotificationsRead(821, shown[0].ID))
	require.NoError(t, MarkCustomNotificationsRead(822, shown[0].ID+1))
	after, unread, err := ListCustomNotifications(821)
	require.NoError(t, err)
	require.Len(t, after, 4, "a user named twice is told once")
	assert.Equal(t, int64(1), unread)
	assert.Zero(t, after[0].ReadAt)
	assert.NotZero(t, after[1].ReadAt)
	NotifyOrgAdminsOnce(org.Id, empty)
	assert.Equal(t, empty.Error(), textsOf(821)[0], "read, so the next time it is told again")

	// A notification that cannot be written leaves the payment whole.
	require.NoError(t, DB.Migrator().DropTable(&CustomNotification{}))
	t.Cleanup(func() { require.NoError(t, DB.AutoMigrate(&CustomNotification{})) })
	second := createEpayTestOrder(t, 821, "EPAYNOTIFY2", PaymentProviderEpay, common.TopUpStatusPending)
	_, err = RechargeEpay(second.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	fresh, err := GetOrganizationById(org.Id)
	require.NoError(t, err)
	assert.Equal(t, 4*500, fresh.WalletQuota)
}
