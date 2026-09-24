package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAutoModelTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

// stubAvailability replaces the channel-availability probe; restore on cleanup.
func stubAvailability(t *testing.T, available map[string]bool) {
	t.Helper()
	orig := groupModelAvailable
	groupModelAvailable = func(group, model, requestPath string) bool {
		return available[group+"/"+model]
	}
	t.Cleanup(func() { groupModelAvailable = orig })
}

func setupAutoModels(t *testing.T, jsonStr string) {
	t.Helper()
	require.NoError(t, setting.UpdateAutoModelConfigs(jsonStr))
	t.Cleanup(func() { _ = setting.UpdateAutoModelConfigs("[]") })
}

// Billing safety: resolution must yield a concrete, currently-available model
// or an explicit rejection — the virtual name must never fall through.
func TestResolveAutoModel(t *testing.T) {
	setupAutoModels(t, `[{"name":"auto-cn","models":["m-a","m-b","m-c"]}]`)

	t.Run("non-auto model passes through untouched", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		resolved, isAuto := ResolveAutoModel(c, "default", "gpt-4o", "/v1/chat/completions")
		assert.False(t, isAuto)
		assert.Empty(t, resolved)
	})

	t.Run("skips unavailable candidates in order", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		stubAvailability(t, map[string]bool{"default/m-b": true, "default/m-c": true})
		resolved, isAuto := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
		assert.True(t, isAuto)
		assert.Equal(t, "m-b", resolved)
		assert.Equal(t, "auto-cn", common.GetContextKeyString(c, constant.ContextKeyAutoModelOriginal))
		assert.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyAutoModelIndex))
	})

	t.Run("no candidate available -> isAuto with empty resolution", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		stubAvailability(t, map[string]bool{})
		resolved, isAuto := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
		assert.True(t, isAuto, "caller must know this WAS an auto model and reject")
		assert.Empty(t, resolved)
	})
}

// The failover cursor must be strictly monotonic and terminate: with N
// candidates there can be at most N-1 advances, regardless of errors.
func TestAdvanceAutoModel(t *testing.T) {
	setupAutoModels(t, `[{"name":"auto-cn","models":["m-a","m-b","m-c"]}]`)

	t.Run("advances past unavailable candidates and then exhausts", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		stubAvailability(t, map[string]bool{"default/m-a": true, "default/m-c": true})
		resolved, isAuto := ResolveAutoModel(c, "default", "auto-cn", "/v1/chat/completions")
		require.True(t, isAuto)
		require.Equal(t, "m-a", resolved)

		next, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
		require.True(t, ok)
		assert.Equal(t, "m-c", next, "m-b is unavailable and must be skipped")

		_, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok, "pool exhausted; cursor must not wrap")
		_, ok = AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok, "repeated calls after exhaustion stay exhausted")
	})

	t.Run("no-op for requests that were never auto", func(t *testing.T) {
		c := newAutoModelTestContext(t)
		_, ok := AdvanceAutoModel(c, "default", "/v1/chat/completions")
		assert.False(t, ok)
	})
}
