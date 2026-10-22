package controller

import (
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
)

// Business development summary (业务信息汇总) — /api/admin/bd/*.
// Routes are guarded by AdminAuth plus bd.read / bd.write / bd.manage; the
// handlers below add the row-level rules (lead deletion by its creator, the
// supplier fields of sale records reserved for bd.manage).

func bdCanManage(c *gin.Context) bool {
	return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.BdManage)
}

func bdParamId(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid id")
		return 0, false
	}
	return id, true
}

// bdResolveOwner fills the owner's display name from the user table; an
// unknown or non-admin user clears the owner.
func bdResolveOwner(ownerId int) (int, string) {
	if ownerId <= 0 {
		return 0, ""
	}
	u, err := model.GetUserById(ownerId, false)
	if err != nil || u == nil || u.Role < common.RoleAdminUser {
		return 0, ""
	}
	return u.Id, model.BdUserName(u)
}

func bdError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrBdNotFound) {
		common.ApiErrorMsg(c, "record not found")
		return
	}
	common.ApiError(c, err)
}

type bdMeta struct {
	Owners     []model.BdOwner   `json:"owners"`
	Models     []string          `json:"models"`
	Companies  []model.BdCompany `json:"companies"`
	Channels   []model.BdPick    `json:"channels"`    // supplier binding options
	Orgs       []model.BdPick    `json:"orgs"`        // customer binding options
	BoundUsers []model.BdPick    `json:"bound_users"` // names of users already bound
	CanWrite   bool              `json:"can_write"`
	CanManage  bool              `json:"can_manage"`
}

// BdGetMeta — GET /api/admin/bd/meta: owner candidates (admins who can read
// this module), model names for suggestions, coded companies and the caller's
// abilities.
func BdGetMeta(c *gin.Context) {
	users, err := model.ListBdOwnerCandidates()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	owners := make([]model.BdOwner, 0, len(users))
	for i := range users {
		if authz.Can(users[i].Id, users[i].Role, authz.BdRead) {
			owners = append(owners, model.BdOwner{Id: users[i].Id, Name: model.BdUserName(&users[i])})
		}
	}
	companies, err := model.ListBdCompanies()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channels, err := model.ListBdChannelPicks()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	orgs, err := model.ListBdOrgPicks()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var userIds []int
	for _, co := range companies {
		userIds = append(userIds, co.UserIds...)
	}
	boundUsers, err := model.BdUserPicksByIds(userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, bdMeta{
		Owners:     owners,
		Models:     model.ListBdModelNames(),
		Companies:  companies,
		Channels:   channels,
		Orgs:       orgs,
		BoundUsers: boundUsers,
		CanWrite:   authz.Can(c.GetInt("id"), c.GetInt("role"), authz.BdWrite),
		CanManage:  bdCanManage(c),
	})
}

// BdGetUsage — GET /api/admin/bd/usage?tz_offset=480: this calendar month's
// platform consumption (USD, list price) per bound supplier / customer, in
// the admin's time zone.
func BdGetUsage(c *gin.Context) {
	loc := reconLocation(c)
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	usage, err := model.GetBdUsage(start.Unix(), start.AddDate(0, 1, 0).Unix())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, usage)
}

// BdSearchUsers — GET /api/admin/bd/users?q=: platform users for binding a
// customer.
func BdSearchUsers(c *gin.Context) {
	picks, err := model.SearchBdUserPicks(c.Query("q"), 20)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, picks)
}

// BdListLeads — GET /api/admin/bd/leads
func BdListLeads(c *gin.Context) {
	rows, err := model.ListBdLeads()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func bdSaveLead(c *gin.Context, id int) {
	var lead model.BdLead
	if err := common.UnmarshalBodyReusable(c, &lead); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	lead.Id = id
	if id > 0 {
		old, err := model.GetBdLead(id)
		if err != nil {
			bdError(c, err)
			return
		}
		lead.Side = old.Side
	}
	lead.OwnerId, lead.OwnerName = bdResolveOwner(lead.OwnerId)
	if err := model.SaveBdLead(&lead, c.GetInt("id")); err != nil {
		bdError(c, err)
		return
	}
	common.ApiSuccess(c, lead)
}

// BdCreateLead — POST /api/admin/bd/leads
func BdCreateLead(c *gin.Context) { bdSaveLead(c, 0) }

// BdUpdateLead — PUT /api/admin/bd/leads/:id
func BdUpdateLead(c *gin.Context) {
	if id, ok := bdParamId(c); ok {
		bdSaveLead(c, id)
	}
}

// BdDeleteLead — DELETE /api/admin/bd/leads/:id. Allowed for bd.manage
// holders and for the user who created the lead.
func BdDeleteLead(c *gin.Context) {
	id, ok := bdParamId(c)
	if !ok {
		return
	}
	lead, err := model.GetBdLead(id)
	if err != nil {
		bdError(c, err)
		return
	}
	if !bdCanManage(c) && lead.CreatedBy != c.GetInt("id") {
		common.ApiErrorMsg(c, "only the creator of this lead or a business summary manager can delete it")
		return
	}
	if err := model.DeleteBdLead(id); err != nil {
		bdError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// BdListDeals — GET /api/admin/bd/deals
func BdListDeals(c *gin.Context) {
	rows, err := model.ListBdDeals()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func bdSaveDeal(c *gin.Context, id int) {
	var deal model.BdDeal
	if err := common.UnmarshalBodyReusable(c, &deal); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	deal.Id = id
	if id > 0 {
		old, err := model.GetBdDeal(id)
		if err != nil {
			bdError(c, err)
			return
		}
		deal.Kind = old.Kind
	}
	deal.OwnerId, deal.OwnerName = bdResolveOwner(deal.OwnerId)
	if err := model.SaveBdDeal(&deal, c.GetInt("id"), bdCanManage(c)); err != nil {
		bdError(c, err)
		return
	}
	common.ApiSuccess(c, deal)
}

// BdCreateDeal — POST /api/admin/bd/deals
func BdCreateDeal(c *gin.Context) { bdSaveDeal(c, 0) }

// BdUpdateDeal — PUT /api/admin/bd/deals/:id
func BdUpdateDeal(c *gin.Context) {
	if id, ok := bdParamId(c); ok {
		bdSaveDeal(c, id)
	}
}

// BdDeleteDeal — DELETE /api/admin/bd/deals/:id (bd.manage, enforced by the
// route).
func BdDeleteDeal(c *gin.Context) {
	id, ok := bdParamId(c)
	if !ok {
		return
	}
	if err := model.DeleteBdDeal(id); err != nil {
		bdError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
