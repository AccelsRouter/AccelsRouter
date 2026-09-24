package controller

import (
	"github.com/gin-gonic/gin"
)

// Reseller keys: API keys that belong to the RESELLER organization itself.
// They are bound to the reseller org's default workspace, so every call bills
// the reseller wallet at the reseller's wholesale price and takes the reseller
// upstream route (see GetOrgPayerInfo / tryOrgBillingSession) — exactly what a
// customer call costs and does. Keys are org-scoped: every active reseller
// admin sees and manages all of them. Reseller parties have no personal keys
// (see AddToken / DisablePersonalTokens), so this is their only key surface.

// ListMyResellerKeys — GET /api/reseller/keys
func ListMyResellerKeys(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	listOrgKeys(c, reseller.Id, 0)
}

// CreateMyResellerKey — POST /api/reseller/keys
func CreateMyResellerKey(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	createOrgKey(c, reseller, "reseller.key.create")
}

// GetMyResellerKey — POST /api/reseller/keys/:token_id/key
func GetMyResellerKey(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	revealOrgKey(c, reseller.Id, 0)
}

// DeleteMyResellerKey — DELETE /api/reseller/keys/:token_id
func DeleteMyResellerKey(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	deleteOrgKey(c, reseller, 0, "reseller.key.delete")
}

// UpdateMyResellerKey — PUT /api/reseller/keys/:token_id
func UpdateMyResellerKey(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	updateOrgKey(c, reseller, 0, "reseller.key.update")
}

// GetMyResellerKeyModels — GET /api/reseller/keys/models
func GetMyResellerKeyModels(c *gin.Context) {
	reseller, ok := callerReseller(c)
	if !ok {
		return
	}
	orgKeyModels(c, reseller)
}
