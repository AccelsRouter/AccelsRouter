package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The audit row of an organization update says what changed, not everything
// the form resubmitted, and always fits the row.
func TestOrgUpdateAuditDetailSaysWhatChanged(t *testing.T) {
	before := &model.Organization{
		Name: "wahaha", PriceGroup: "default", Status: model.OrgStatusActive,
		AllowedModels: `["qwen3.8-flash","glm-5-2"]`, WholesaleRatios: `{"qwen":0.8}`,
	}
	detail := func(fields map[string]any) map[string]any {
		var changes map[string]any
		text := orgUpdateAuditDetail(before, fields)
		require.LessOrEqual(t, len(text), orgAuditDetailLimit)
		require.NoError(t, common.UnmarshalJsonStr(text, &changes))
		return changes
	}

	assert.Empty(t, detail(map[string]any{
		"name": "wahaha", "price_group": "default", "status": model.OrgStatusActive, "remark": "",
		"allowed_models": `["glm-5-2","qwen3.8-flash"]`, "wholesale_ratios": `{"qwen":0.8}`,
	}), "a form saved unchanged records no change")

	assert.Equal(t, map[string]any{
		"status":           model.OrgStatusSuspended,
		"models_added":     []any{"wan3.0-video"},
		"models_removed":   []any{"glm-5-2"},
		"wholesale_ratios": map[string]any{"qwen": 0.7},
	}, detail(map[string]any{
		"name": "wahaha", "status": model.OrgStatusSuspended,
		"allowed_models": `["qwen3.8-flash","wan3.0-video"]`, "wholesale_ratios": `{"qwen":0.7}`,
	}))

	assert.Equal(t, map[string]any{"models_unrestricted": true}, detail(map[string]any{"allowed_models": ""}))

	unrestricted := &model.Organization{Name: "open"}
	var limited map[string]any
	require.NoError(t, common.UnmarshalJsonStr(orgUpdateAuditDetail(unrestricted, map[string]any{"allowed_models": `["glm-5-2"]`}), &limited))
	assert.Equal(t, map[string]any{"models_limited": []any{"glm-5-2"}}, limited)

	// Lists too long for the row give way to their length; a long remark to a flag.
	many := make([]string, 60)
	for i := range many {
		many[i] = "model-with-a-long-name-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	stored, err := model.MarshalAllowedModels(append([]string{"qwen3.8-flash", "glm-5-2"}, many...))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"models_added": float64(60), "remark_changed": true},
		detail(map[string]any{"allowed_models": stored, "remark": strings.Repeat("备注", 200)}))
}

// The activation code of an account opened by invitation sets that account's
// password, so it reaches the inviter only when no email could carry it, and
// no invitation list hands it to whoever reads the list. A join code, which
// works only for the invited address after sign-in, stays.
func TestActivationCodesStayWithTheInvitedMailbox(t *testing.T) {
	assert.Empty(t, activationCodeForInviter("secret-code", true))
	assert.Equal(t, "secret-code", activationCodeForInviter("secret-code", false))

	rows := withoutActivationCodes([]*model.OrgInvitation{
		{Id: 1, Code: "activation", Status: model.OrgInvitationProvisioned},
		{Id: 2, Code: "join", Status: "pending"},
	})
	assert.Empty(t, rows[0].Code)
	assert.Equal(t, "join", rows[1].Code)
}
