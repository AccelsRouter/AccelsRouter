package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
)

// Business development summary (业务信息汇总) — /api/admin/bd/*.
// Routes are guarded by AdminAuth plus bd.read / bd.write; the handlers add
// the row-level rule that a lead or an allocation record can be deleted by
// its creator or by a bd.manage holder.

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
	Owners    []model.BdOwner   `json:"owners"`
	Models    []string          `json:"models"`
	Companies []model.BdCompany `json:"companies"`
	CanWrite  bool              `json:"can_write"`
	CanManage bool              `json:"can_manage"`
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
	common.ApiSuccess(c, bdMeta{
		Owners:    owners,
		Models:    model.ListBdModelNames(),
		Companies: companies,
		CanWrite:  authz.Can(c.GetInt("id"), c.GetInt("role"), authz.BdWrite),
		CanManage: bdCanManage(c),
	})
}

// BdListLeads — GET /api/admin/bd/leads (each with its models)
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
// holders and for the user who created the lead, once no allocation record
// refers to it.
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

// BdListAllocations — GET /api/admin/bd/allocations
func BdListAllocations(c *gin.Context) {
	rows, err := model.ListBdAllocations()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func bdSaveAllocation(c *gin.Context, id int) {
	var a model.BdAllocation
	if err := common.UnmarshalBodyReusable(c, &a); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	a.Id = id
	a.OwnerId, a.OwnerName = bdResolveOwner(a.OwnerId)
	if err := model.SaveBdAllocation(&a, c.GetInt("id")); err != nil {
		bdError(c, err)
		return
	}
	common.ApiSuccess(c, a)
}

// BdCreateAllocation — POST /api/admin/bd/allocations
func BdCreateAllocation(c *gin.Context) { bdSaveAllocation(c, 0) }

// BdUpdateAllocation — PUT /api/admin/bd/allocations/:id
func BdUpdateAllocation(c *gin.Context) {
	if id, ok := bdParamId(c); ok {
		bdSaveAllocation(c, id)
	}
}

// BdDeleteAllocation — DELETE /api/admin/bd/allocations/:id. Allowed for
// bd.manage holders and for the user who created the record.
func BdDeleteAllocation(c *gin.Context) {
	id, ok := bdParamId(c)
	if !ok {
		return
	}
	a, err := model.GetBdAllocation(id)
	if err != nil {
		bdError(c, err)
		return
	}
	if !bdCanManage(c) && a.CreatedBy != c.GetInt("id") {
		common.ApiErrorMsg(c, "only the creator of this record or a business summary manager can delete it")
		return
	}
	if err := model.DeleteBdAllocation(id); err != nil {
		bdError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
