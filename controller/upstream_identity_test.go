package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The identity verdict: a genuine answer matches the baseline token count,
// echoes the requested model and carries the family's id shape; a swapped
// model fails on the tokenizer count even when the proxy forges the name;
// without a baseline the best the probe can say is "unverified".
func TestEvaluateIdentity(t *testing.T) {
	genuine := func() *dto.OpenAITextResponse {
		r := &dto.OpenAITextResponse{Id: "msg_01ABC", Model: "claude-fable-5-20260601"}
		r.PromptTokens = 118
		return r
	}
	baseline := &model.ModelFingerprint{PromptTokens: 118, SourceChannelId: 1}

	checks, verdict := evaluateIdentity("claude", "claude-fable-5", genuine(), baseline, 0)
	assert.Equal(t, model.IdentityVerdictVerified, verdict)
	for _, c := range checks {
		assert.Equal(t, "pass", c.Status, c.Name)
	}

	t.Run("tokenizer mismatch is suspect even with a forged model name", func(t *testing.T) {
		r := genuine()
		r.PromptTokens = 97 // a different tokenizer counted the canary
		checks, verdict := evaluateIdentity("claude", "claude-fable-5", r, baseline, 0)
		assert.Equal(t, model.IdentityVerdictSuspect, verdict)
		require.Len(t, checks, 3)
		assert.Equal(t, "fail", checks[2].Status)
		assert.Equal(t, "does not match the baseline", checks[2].Note)
	})
	t.Run("small slack for an injected system prompt", func(t *testing.T) {
		r := genuine()
		r.PromptTokens = 120
		_, verdict := evaluateIdentity("claude", "claude-fable-5", r, baseline, 0)
		assert.Equal(t, model.IdentityVerdictVerified, verdict)
	})
	t.Run("another model answered", func(t *testing.T) {
		r := genuine()
		r.Model = "deepseek-v4-flash"
		_, verdict := evaluateIdentity("claude", "claude-fable-5", r, baseline, 0)
		assert.Equal(t, model.IdentityVerdictSuspect, verdict)
	})
	t.Run("wrong id shape for the family", func(t *testing.T) {
		r := genuine()
		r.Id = "chatcmpl-xyz"
		checks, verdict := evaluateIdentity("claude", "claude-fable-5", r, baseline, 0)
		assert.Equal(t, model.IdentityVerdictSuspect, verdict)
		assert.Equal(t, "fail", checks[1].Status)
	})
	t.Run("no baseline yet: no token row, unverified", func(t *testing.T) {
		checks, verdict := evaluateIdentity("claude", "claude-fable-5", genuine(), nil, 0)
		assert.Equal(t, model.IdentityVerdictUnverified, verdict)
		for _, c := range checks {
			assert.NotEqual(t, "prompt_tokens", c.Name)
		}
	})
	t.Run("OpenAI model without a baseline uses the local tokenizer count", func(t *testing.T) {
		r := &dto.OpenAITextResponse{Id: "chatcmpl-abc", Model: "gpt-5.6-terra-2026-07-09"}
		r.PromptTokens = 94
		checks, verdict := evaluateIdentity("openai", "gpt-5.6-terra", r, nil, 91)
		assert.Equal(t, model.IdentityVerdictVerified, verdict)
		assert.Equal(t, "prompt_tokens", checks[2].Name)
		assert.Equal(t, "pass", checks[2].Status)
		r.PromptTokens = 130 // another tokenizer counted the canary
		_, verdict = evaluateIdentity("openai", "gpt-5.6-terra", r, nil, 91)
		assert.Equal(t, model.IdentityVerdictSuspect, verdict)
	})
	t.Run("family follows the model name, then the channel type", func(t *testing.T) {
		assert.Equal(t, "claude", identityFamily("claude-opus-4-8", 1), "OpenAI-compatible proxy fronting Claude")
		assert.Equal(t, "openai", identityFamily("gpt-5.5", 14))
		assert.Equal(t, "gemini", identityFamily("gemini-3-pro", 1))
		assert.Equal(t, "claude", identityFamily("my-alias", 33), "unknown name: Bedrock channel")
		assert.Equal(t, "generic", identityFamily("deepseek-v4-flash", 1))
	})
	t.Run("Bedrock ids are Anthropic ids", func(t *testing.T) {
		r := genuine()
		r.Id = "msg_bdrk_01XYZ"
		_, verdict := evaluateIdentity("claude", "claude-fable-5", r, baseline, 0)
		assert.Equal(t, model.IdentityVerdictVerified, verdict)
	})
	t.Run("generic family skips the id check", func(t *testing.T) {
		r := genuine()
		r.Id = "whatever"
		checks, verdict := evaluateIdentity("generic", "claude-fable-5", r, baseline, 0)
		assert.Equal(t, model.IdentityVerdictVerified, verdict)
		for _, c := range checks {
			assert.NotEqual(t, "response_id", c.Name)
		}
	})
}
