package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// CustomNotification is one thing a user is told about in the web console: a
// top-up that arrived, someone joining or leaving the organization they run,
// the answer to a request of theirs. A row belongs to one user; what concerns
// several people is written once for each. Message is a common.Message as
// JSON, an English sentence with its values, which the console translates.
type CustomNotification struct {
	ID        int    `json:"id" gorm:"primaryKey"`
	UserId    int    `json:"-" gorm:"index:idx_custom_notification_user,priority:1"`
	Message   string `json:"-" gorm:"type:text"`
	CreatedAt int64  `json:"created_at"`
	// ReadAt is when the user saw it; 0 until then.
	ReadAt int64 `json:"read_at" gorm:"index:idx_custom_notification_user,priority:2"`
}

// The newest notifications a user is shown.
const customNotificationListLimit = 50

// notifyUsers writes message into the notifications of each user.
func notifyUsers(tx *gorm.DB, userIds []int, message *common.Message) error {
	encoded, err := common.Marshal(message)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	var rows []CustomNotification
	for _, userId := range slices.Compact(slices.Sorted(slices.Values(userIds))) {
		if userId > 0 {
			rows = append(rows, CustomNotification{UserId: userId, Message: string(encoded), CreatedAt: now})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

// NotifyUsers tells each user message. Best-effort: what it reports has
// already happened, so a failure is logged and never returned.
func NotifyUsers(userIds []int, message *common.Message) {
	if err := notifyUsers(DB, userIds, message); err != nil {
		common.SysError(common.LogText("failed to record notification: %s", err.Error()))
	}
}

// orgAdminUserIds lists who runs an organization: its owner and admins, and
// for a reseller the people holding its reseller-admin link. Suspended ones
// are left out; a row from before the status column existed has none.
func orgAdminUserIds(tx *gorm.DB, orgId int) ([]int, error) {
	var members []OrgAccount
	err := tx.Select("user_id", "status").
		Where("org_id = ? AND role IN ?", orgId, []string{OrgRoleOwner, OrgRoleAdmin}).
		Find(&members).Error
	if err != nil {
		return nil, err
	}
	var resellerAdmins []ResellerAdmin
	if err := tx.Select("user_id", "status").Where("reseller_org_id = ?", orgId).Find(&resellerAdmins).Error; err != nil {
		return nil, err
	}
	var ids []int
	for _, member := range members {
		if member.Status != OrgStatusSuspended {
			ids = append(ids, member.UserId)
		}
	}
	for _, admin := range resellerAdmins {
		if admin.Status != OrgStatusSuspended {
			ids = append(ids, admin.UserId)
		}
	}
	return ids, nil
}

// NotifyOrgAdmins tells the people who run an organization message, except the
// one who did what it reports (0: nobody is left out). Best-effort.
func NotifyOrgAdmins(orgId, exceptUserId int, message *common.Message) {
	admins, err := orgAdminUserIds(DB, orgId)
	if err != nil {
		common.SysError(common.LogText("failed to record notification: %s", err.Error()))
		return
	}
	NotifyUsers(slices.DeleteFunc(admins, func(id int) bool { return id == exceptUserId }), message)
}

// NotifyOrgAdminsOnce is NotifyOrgAdmins for a condition that keeps recurring,
// such as an empty wallet refusing every request: someone who has not read
// the last such message yet is not told again.
func NotifyOrgAdminsOnce(orgId int, message *common.Message) {
	admins, err := orgAdminUserIds(DB, orgId)
	if err != nil || len(admins) == 0 {
		return
	}
	var unread []CustomNotification
	if err := DB.Where("user_id IN ? AND read_at = ?", admins, 0).Find(&unread).Error; err != nil {
		return
	}
	for _, row := range unread {
		var told common.Message
		// The same sentence with the same values reads the same.
		if common.UnmarshalJsonStr(row.Message, &told) == nil && told.Error() == message.Error() {
			admins = slices.DeleteFunc(admins, func(id int) bool { return id == row.UserId })
		}
	}
	NotifyUsers(admins, message)
}

// NotifyPlatformAdmins tells the platform's administrators message: something
// awaits their review. Best-effort.
func NotifyPlatformAdmins(message *common.Message) {
	var admins []int
	err := DB.Model(&User{}).
		Where("role >= ? AND status = ?", common.RoleAdminUser, common.UserStatusEnabled).
		Pluck("id", &admins).Error
	if err != nil {
		common.SysError(common.LogText("failed to record notification: %s", err.Error()))
		return
	}
	NotifyUsers(admins, message)
}

// CustomNotificationView is a notification as the console receives it.
type CustomNotificationView struct {
	ID        int             `json:"id"`
	Message   *common.Message `json:"message"`
	CreatedAt int64           `json:"created_at"`
	ReadAt    int64           `json:"read_at"`
}

// ListCustomNotifications returns the user's newest notifications, newest
// first, and how many of all of them are unread.
func ListCustomNotifications(userId int) ([]CustomNotificationView, int64, error) {
	var rows []CustomNotification
	err := DB.Where("user_id = ?", userId).Order("id DESC").Limit(customNotificationListLimit).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	var unread int64
	err = DB.Model(&CustomNotification{}).Where("user_id = ? AND read_at = ?", userId, 0).Count(&unread).Error
	if err != nil {
		return nil, 0, err
	}
	views := make([]CustomNotificationView, 0, len(rows))
	for _, row := range rows {
		view := CustomNotificationView{ID: row.ID, CreatedAt: row.CreatedAt, ReadAt: row.ReadAt, Message: &common.Message{}}
		if err := common.UnmarshalJsonStr(row.Message, view.Message); err != nil {
			continue
		}
		views = append(views, view)
	}
	return views, unread, nil
}

// MarkCustomNotificationsRead marks the user's notifications up to one as
// read: the newest the user was shown, so one that arrived since stays unread.
func MarkCustomNotificationsRead(userId, upToId int) error {
	return DB.Model(&CustomNotification{}).
		Where("user_id = ? AND id <= ? AND read_at = ?", userId, upToId, 0).
		Update("read_at", common.GetTimestamp()).Error
}
