package model

// Fork-only organization audit log: an append-only trail of the sensitive
// mutations on an org (membership, budgets, credit movement, workspaces, BYOK,
// SSO domains). It answers "who changed what, when" for an org owner/admin and
// the platform admin. Writes are best-effort and never block the mutation they
// record — an audit failure must not fail a legitimate action.

import (
	"strconv"
	"strings"

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
	// TargetName is the name of the organization, member or workspace Target
	// points at by id, while it still exists. Computed, non-persisted.
	TargetName string `json:"target_name,omitempty" gorm:"-"`
}

// RecordOrgAudit appends one audit row. Best-effort: a write failure is logged
// and swallowed so it can never break the action being audited.
func RecordOrgAudit(orgId, actorUserId int, action, target, detail string) {
	if orgId <= 0 || action == "" {
		return
	}
	// Cut on a character boundary: half a multi-byte character is not valid
	// text, and PostgreSQL refuses the row.
	if len(detail) > 512 {
		detail = strings.ToValidUTF8(detail[:512], "")
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
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	// Name what the rows point at. Best-effort: a row keeps its bare target
	// when the lookup fails or the target is gone.
	ids := map[string][]int{}
	for _, row := range rows {
		kind, text, _ := strings.Cut(row.Target, ":")
		if id, err := strconv.Atoi(text); err == nil && id > 0 {
			ids[kind] = append(ids[kind], id)
		}
	}
	names := map[string]string{}
	for _, named := range []struct {
		kind   string
		table  any
		column string
	}{
		{"org", &Organization{}, "name"},
		{"user", &User{}, "username"},
		{"workspace", &Workspace{}, "name"},
	} {
		if len(ids[named.kind]) == 0 {
			continue
		}
		var found []struct {
			Id   int
			Name string
		}
		if err := DB.Model(named.table).Select("id, "+named.column+" AS name").Where("id IN ?", ids[named.kind]).Find(&found).Error; err != nil {
			common.SysError("org audit target names: " + err.Error())
			continue
		}
		for _, item := range found {
			names[named.kind+":"+strconv.Itoa(item.Id)] = item.Name
		}
	}
	for _, row := range rows {
		row.TargetName = names[row.Target]
	}
	return rows, total, nil
}
