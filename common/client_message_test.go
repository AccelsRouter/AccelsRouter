package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientFacingMessageKeepsReasonDropsIdentity(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"No available channel for model gpt-4o under group reseller-6 (distributor)", "No available channel for model gpt-4o"},
		{"No available channel for model m under group reseller-6: the model is claimed by a task plugin, which has no enabled channel serving it (distributor)", "No available channel for model m: the model is claimed by a task plugin, which has no enabled channel serving it"},
		{"分组 auto 下模型 claude-fable-5 的可用渠道不存在（retry） (request id: rFENgDfC)", "模型 claude-fable-5 的可用渠道不存在"},
		{"分組 reseller-6 下模型 x 無可用管道（distributor）", "模型 x 無可用管道"},
		{"prompt is too long: 213 tokens > 200 maximum (request id: 20261016...)", "prompt is too long: 213 tokens > 200 maximum"},
		{"Rate limit reached for gpt-4o. request ID: req_abc123XYZ", "Rate limit reached for gpt-4o."},
		{"invalid_api_key", "invalid_api_key"},
		{"bad response status code 502", "bad response status code 502"},
		{"(request id: only)", "(request id: only)"},
	} {
		assert.Equal(t, tc.want, ClientFacingMessage(tc.in), tc.in)
	}
}
