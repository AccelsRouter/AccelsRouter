// Fork-only personal BYOK console: an individual user brings its own upstream
// provider credentials, materialized as normal channels serving the user's
// private group (user-<id>), priced by that group's ratio (the BYOK fee,
// default 0 = free). A BYOK key is a token in that private group; it routes
// only to the user's own BYOK channels. Gated globally by PersonalByokEnabled.
package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// maxPersonalByokChannels bounds how many BYOK provider channels one user may
// own, so the feature can't be used to accumulate unbounded channels/abilities.
const maxPersonalByokChannels = 20

// personalByokUser gates the feature and returns the caller's user id.
func personalByokUser(c *gin.Context) (int, bool) {
	if !setting.PersonalByokEnabled {
		common.ApiErrorMsg(c, "个人 BYOK 功能未开启")
		return 0, false
	}
	return c.GetInt("id"), true
}

type personalByokRequest struct {
	Name    string `json:"name"`
	Type    int    `json:"type"`
	Key     string `json:"key"`
	BaseURL string `json:"base_url"`
	Models  string `json:"models"`
}

// CreateMyPersonalByok — POST /api/personal_byok/channels
func CreateMyPersonalByok(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	var req personalByokRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Models) == "" {
		common.ApiErrorMsg(c, "name, key and models are required")
		return
	}
	if count, err := model.CountUserChannels(userId); err != nil {
		common.ApiError(c, err)
		return
	} else if count >= maxPersonalByokChannels {
		common.ApiErrorMsg(c, fmt.Sprintf("已达到 BYOK 渠道数量上限 (%d)", maxPersonalByokChannels))
		return
	}
	// Encrypt the user's upstream credential at rest (AES-GCM). It is decrypted
	// only in the relay path for this user's own requests; no admin/API path
	// returns it. See common/crypto_byok.go.
	encKey, err := common.EncryptByokSecret(strings.TrimSpace(req.Key))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group := model.UserPrivateGroup(userId)
	channel := &model.Channel{
		Type:   req.Type,
		Name:   "[BYOK] " + req.Name,
		Key:    encKey,
		Models: req.Models,
		Group:  group, // isolated to this user's private group only
		Status: common.ChannelStatusEnabled,
	}
	if baseURL := strings.TrimSpace(req.BaseURL); baseURL != "" {
		channel.BaseURL = &baseURL
	}
	if err := channel.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.AddUserChannel(userId, channel.Id); err != nil {
		_ = channel.Delete() // best-effort cleanup of the orphaned channel
		common.ApiError(c, err)
		return
	}
	// Register the private group's ratio (= BYOK fee, default 0) so the request
	// path prices it correctly and the auth group-ratio check passes.
	if err := service.EnsureByokGroupRatio(group); err != nil {
		common.SysError("personal byok: ensure group ratio failed: " + err.Error())
	}
	common.ApiSuccess(c, gin.H{"channel_id": channel.Id, "group": group})
}

// ListMyPersonalByok — GET /api/personal_byok/channels
func ListMyPersonalByok(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	ids, err := model.ListUserChannelIds(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	out := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		ch, err := model.GetChannelById(id, false)
		if err != nil || ch == nil {
			continue
		}
		out = append(out, gin.H{
			"channel_id": ch.Id,
			"name":       ch.Name,
			"type":       ch.Type,
			"models":     ch.Models,
			"status":     ch.Status,
			"key_masked": maskChannelKey(ch.Key),
		})
	}
	common.ApiSuccess(c, out)
}

// DeleteMyPersonalByok — DELETE /api/personal_byok/channels/:channel_id
func DeleteMyPersonalByok(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	channelId, _ := strconv.Atoi(c.Param("channel_id"))
	owns, err := model.UserOwnsChannel(userId, channelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !owns {
		common.ApiErrorMsg(c, "channel does not belong to you")
		return
	}
	ch, err := model.GetChannelById(channelId, false)
	if err != nil || ch == nil {
		common.ApiErrorMsg(c, "channel not found")
		return
	}
	if err := ch.Delete(); err != nil {
		common.ApiError(c, err)
		return
	}
	_ = model.RemoveUserChannel(userId, channelId)
	// When the user's last BYOK channel is gone, drop the private group's ratio.
	if count, cErr := model.CountUserChannels(userId); cErr == nil && count == 0 {
		if rErr := service.RemoveByokGroupRatio(model.UserPrivateGroup(userId)); rErr != nil {
			common.SysError("personal byok: remove group ratio failed: " + rErr.Error())
		}
	}
	common.ApiSuccess(c, nil)
}

type personalByokKeyRequest struct {
	Name string `json:"name"`
}

// CreateMyPersonalByokKey — POST /api/personal_byok/keys
// Provisions a token in the user's BYOK private group so requests route to the
// user's own BYOK channels (and are priced by the BYOK fee). Returned once.
func CreateMyPersonalByokKey(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	var req personalByokKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		common.ApiErrorMsg(c, "key name is required")
		return
	}
	// Enforce the same per-user token cap as the normal token-create path, so
	// this route can't be used to exceed it.
	maxTokens := operation_setting.GetMaxUserTokens()
	if count, err := model.CountUserTokens(userId); err != nil {
		common.ApiError(c, err)
		return
	} else if int(count) >= maxTokens {
		common.ApiErrorMsg(c, fmt.Sprintf("已达到最大令牌数量限制 (%d)", maxTokens))
		return
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token := &model.Token{
		UserId:         userId,
		Name:           strings.TrimSpace(req.Name),
		Key:            key,
		Group:          model.UserPrivateGroup(userId),
		CreatedTime:    common.GetTimestamp(),
		AccessedTime:   common.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Status:         common.TokenStatusEnabled,
	}
	if err := token.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"token_id": token.Id, "key": "sk-" + key})
}

// ListMyPersonalByokKeys — GET /api/personal_byok/keys
func ListMyPersonalByokKeys(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	// Fetch by user_id (not reserved) and filter the private group in Go to
	// avoid raw SQL against the reserved `group` column across dialects.
	var tokens []model.Token
	if err := model.DB.Where("user_id = ?", userId).Order("id DESC").Find(&tokens).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	group := model.UserPrivateGroup(userId)
	out := make([]gin.H, 0)
	for _, t := range tokens {
		if t.Group != group {
			continue
		}
		out = append(out, gin.H{
			"token_id":   t.Id,
			"name":       t.Name,
			"status":     t.Status,
			"key_masked": "sk-" + maskChannelKey(t.Key),
		})
	}
	common.ApiSuccess(c, out)
}

// GetMyByokFallback — GET /api/personal_byok/fallback
// Reports whether the user has opted in to falling back to platform channels
// (billed at the platform rate) when their own BYOK channel is unavailable.
func GetMyByokFallback(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	s, err := model.GetUserSetting(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"enabled": s.ByokFallbackToPlatform})
}

// SetMyByokFallback — PUT /api/personal_byok/fallback
// Toggles the platform-fallback opt-in. Default is off (fail closed): a failing
// BYOK channel returns an error instead of silently spending platform quota.
func SetMyByokFallback(c *gin.Context) {
	userId, ok := personalByokUser(c)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	// Read-modify-write the full setting so unrelated preferences are preserved.
	s, err := model.GetUserSetting(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	s.ByokFallbackToPlatform = req.Enabled
	if err := model.UpdateUserSetting(userId, s); err != nil {
		common.ApiError(c, err)
		return
	}
	// Invalidate the main user cache so the relay hot path (which reads the
	// setting from the cached user) picks up the change on the next request —
	// especially important when DISABLING, so fallback stops promptly.
	_ = model.InvalidateUserCache(userId)
	common.ApiSuccess(c, gin.H{"enabled": req.Enabled})
}
