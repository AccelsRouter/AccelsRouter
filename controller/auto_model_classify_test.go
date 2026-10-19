package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func classifyRequest(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/option/auto_model/classify", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	ClassifyAutoModelText(c)
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return rec.Code, out
}

func TestClassifyAutoModelText(t *testing.T) {
	code, out := classifyRequest(t, `{"text":"请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, out["success"])
	data := out["data"].(map[string]any)
	assert.Equal(t, "reasoning", data["tier"])
	assert.Equal(t, "reasoning_override", data["cause"])
	assert.NotEmpty(t, data["signals"])
	assert.Len(t, data["dimensions"], 7)

	// unsaved overrides are applied
	_, out = classifyRequest(t, `{"text":"帮我把这段话润色一下，让它读起来更自然流畅，语气正式一些，但是不要改变原意，也不要增加新的内容","classifier":{"noSignalTier":"simple"}}`)
	assert.Equal(t, "simple", out["data"].(map[string]any)["tier"])

	// tools raise the floor
	_, out = classifyRequest(t, `{"text":"你好","has_tools":true}`)
	assert.Equal(t, "medium", out["data"].(map[string]any)["tier"])
}

func TestClassifyAutoModelTextRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"not json":           `{nope`,
		"unknown override":   `{"text":"x","classifier":{"noSignalTeir":"simple"}}`,
		"invalid boundaries": `{"text":"x","classifier":{"boundaries":{"simpleMedium":0.5,"mediumComplex":0.4,"complexReasoning":0.7}}}`,
		"text too long":      `{"text":"` + strings.Repeat("a", maxClassifyTextBytes+1) + `"}`,
	} {
		code, out := classifyRequest(t, body)
		assert.Equal(t, http.StatusBadRequest, code, name)
		assert.Equal(t, false, out["success"], name)
	}
}
