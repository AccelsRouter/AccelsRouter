package model

// Fork-only organization audit log: an append-only trail of the sensitive
// mutations on an org (membership, budgets, credit movement, workspaces, BYOK,
// SSO domains). It answers "who changed what, when" for an org owner/admin and
// the platform admin. Writes are best-effort and never block the mutation they
// record — an audit failure must not fail a legitimate action.

import (
	"github.com/QuantumNous/new-api/common"
)

// OrgAuditLog is one recorded action. Detail is a short human-readable summary
// (not a full diff) so the table stays cheap and cross-DB safe.
type OrgAuditLog struct {
	Id          int    `json:"id" gorm:"primarykey"`
	OrgId       int    `json:"org_id" gorm:"index;not null"`
	ActorUserId int    `json:"actor_user_id" gorm:"index"` // 0 = platform/system
	Action      string `json:"action" gorm:"type:varchar(48);index"`
	Target      string `json:"target" gorm:"type:varchar(128)"`
	Detail      string `json:"detail" gorm:"type:varchar(512)"`
	CreatedTime int64  `json:"created_time" gorm:"index"`
}

// RecordOrgAudit appends one audit row. Best-effort: a write failure is logged
// and swallowed so it can never break the action being audited.
func RecordOrgAudit(orgId, actorUserId int, action, target, detail string) {
	if orgId <= 0 || action == "" {
		return
	}
	if len(detail) > 512 {
		detail = detail[:512]
	}
	row := &OrgAuditLog{
		OrgId:       orgId,
		ActorUserId: actorUserId,
		Action:      action,
		Target:      target,
		Detail:      detail,
		CreatedTime: common.GetTimestamp(),
	}
	if err := DB.Create(row).Error; err != nil {
		common.SysError("org audit write failed: " + err.Error())
	}
}

// ListOrgAuditLogs returns one page of an org's audit trail, newest first.
func ListOrgAuditLogs(orgId, offset, limit int) ([]*OrgAuditLog, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := DB.Model(&OrgAuditLog{}).Where("org_id = ?", orgId)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*OrgAuditLog
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}
