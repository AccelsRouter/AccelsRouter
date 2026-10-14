package controller

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// AdminInflight — GET /api/admin/inflight. Relay requests in progress right
// now across every node, added up per user, organization, model and channel,
// plus the last hour's concurrency peaks. Read-only; nothing is stored.
func AdminInflight(c *gin.Context) {
	snapshots := service.InflightSnapshots(c.Request.Context())
	type userRow struct {
		UserId   int    `json:"user_id"`
		Username string `json:"username"`
		OrgId    int    `json:"org_id,omitempty"`
		OrgName  string `json:"org_name,omitempty"`
		Inflight int    `json:"inflight"`
		Peak     int    `json:"peak"`
	}
	type countRow struct {
		Key      string `json:"key"`
		Name     string `json:"name,omitempty"`
		Inflight int    `json:"inflight"`
	}
	users := map[int]*userRow{}
	orgs := map[int]*countRow{}
	models := map[string]*countRow{}
	channels := map[int]*countRow{}
	entries := make([]service.InflightEntry, 0)
	nodes := make([]gin.H, 0, len(snapshots))
	peakTotal := 0
	for _, snap := range snapshots {
		nodes = append(nodes, gin.H{"node": snap.Node, "inflight": len(snap.Entries), "taken_at": snap.TakenAt, "peak": snap.PeakTotal})
		// Peaks happen on each node separately; their sum is the conservative
		// upper bound of the fleet-wide peak.
		peakTotal += snap.PeakTotal
		for userId, peak := range snap.PeakUsers {
			row := users[common.String2Int(userId)]
			if row == nil {
				row = &userRow{UserId: common.String2Int(userId)}
				users[row.UserId] = row
			}
			row.Peak += peak
		}
		for _, entry := range snap.Entries {
			entries = append(entries, entry)
			row := users[entry.UserId]
			if row == nil {
				row = &userRow{UserId: entry.UserId}
				users[entry.UserId] = row
			}
			row.Inflight++
			row.Username = entry.Username
			row.OrgId = entry.OrgId
			if entry.OrgId > 0 {
				orgRow := orgs[entry.OrgId]
				if orgRow == nil {
					orgRow = &countRow{Key: common.Interface2String(entry.OrgId)}
					orgs[entry.OrgId] = orgRow
				}
				orgRow.Inflight++
			}
			modelRow := models[entry.Model]
			if modelRow == nil {
				modelRow = &countRow{Key: entry.Model}
				models[entry.Model] = modelRow
			}
			modelRow.Inflight++
			channelRow := channels[entry.ChannelId]
			if channelRow == nil {
				channelRow = &countRow{Key: common.Interface2String(entry.ChannelId)}
				channels[entry.ChannelId] = channelRow
			}
			channelRow.Inflight++
		}
	}
	// Names for rows that only carry ids (peak-only users, orgs, channels).
	for userId, row := range users {
		if row.Username == "" {
			if user, err := model.GetUserById(userId, false); err == nil && user != nil {
				row.Username = user.Username
			}
		}
	}
	orgNames := map[int]string{}
	for orgId, row := range orgs {
		if org, err := model.GetOrganizationById(orgId); err == nil && org != nil {
			row.Name = org.Name
			orgNames[orgId] = org.Name
		}
	}
	for _, row := range users {
		if row.OrgId > 0 {
			row.OrgName = orgNames[row.OrgId]
		}
	}
	for channelId, row := range channels {
		if channelId > 0 {
			if channel, err := model.CacheGetChannel(channelId); err == nil && channel != nil {
				row.Name = channel.Name
			}
		}
	}
	userRows := make([]userRow, 0, len(users))
	for _, row := range users {
		userRows = append(userRows, *row)
	}
	sort.Slice(userRows, func(i, j int) bool {
		if userRows[i].Inflight != userRows[j].Inflight {
			return userRows[i].Inflight > userRows[j].Inflight
		}
		return userRows[i].Peak > userRows[j].Peak
	})
	sortedCounts := func(rows map[int]*countRow) []countRow {
		out := make([]countRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, *row)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Inflight > out[j].Inflight })
		return out
	}
	modelRows := make([]countRow, 0, len(models))
	for _, row := range models {
		modelRows = append(modelRows, *row)
	}
	sort.Slice(modelRows, func(i, j int) bool { return modelRows[i].Inflight > modelRows[j].Inflight })
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartedAt < entries[j].StartedAt })
	common.ApiSuccess(c, gin.H{
		"taken_at":   common.GetTimestamp(),
		"total":      len(entries),
		"peak_total": peakTotal,
		"nodes":      nodes,
		"users":      userRows,
		"orgs":       sortedCounts(orgs),
		"models":     modelRows,
		"channels":   sortedCounts(channels),
		"entries":    entries,
	})
}
