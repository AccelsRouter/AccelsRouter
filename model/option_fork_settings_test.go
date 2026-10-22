package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fork's payment, onboarding and BYOK options are saved through the
// generic option API; each key must reach its runtime setting, otherwise the
// admin toggle persists but never takes effect (WonderGate top-up stayed
// hidden for weeks after the mapping was dropped).
func TestUpdateOptionMapAppliesForkSettings(t *testing.T) {
	originalMap := common.OptionMap
	common.OptionMap = map[string]string{}
	previous := struct {
		wg, sandbox, byok, enterprise bool
		merchant, group, currency     string
		method                        string
		fee, unit                     float64
		min                           int
	}{setting.WonderGateEnabled, setting.WonderGateSandbox, setting.PersonalByokEnabled, OrgEnterpriseAutoApprove,
		setting.WonderGateSandboxMerchantId, OrgDefaultPriceGroup, setting.WonderGateCurrency, setting.WonderGateTransactionMethod,
		setting.ByokFeeRatio, setting.WonderGateUnitPrice, setting.WonderGateMinTopUp}
	t.Cleanup(func() {
		common.OptionMap = originalMap
		setting.WonderGateEnabled, setting.WonderGateSandbox = previous.wg, previous.sandbox
		setting.PersonalByokEnabled, OrgEnterpriseAutoApprove = previous.byok, previous.enterprise
		setting.WonderGateSandboxMerchantId, OrgDefaultPriceGroup, setting.WonderGateCurrency = previous.merchant, previous.group, previous.currency
		setting.ByokFeeRatio, setting.WonderGateUnitPrice, setting.WonderGateMinTopUp = previous.fee, previous.unit, previous.min
		setting.WonderGateTransactionMethod = previous.method
	})

	for key, value := range map[string]string{
		"WonderGateEnabled":           "true",
		"WonderGateSandbox":           "false",
		"WonderGateSandboxMerchantId": "m-sandbox",
		"WonderGateUnitPrice":         "7.25",
		"WonderGateMinTopUp":          "3",
		"WonderGateCurrency":          "HKD",
		"WonderGateTransactionMethod": "AlipayHK",
		"PersonalByokEnabled":         "true",
		"ByokFeeRatio":                "0.15",
		"OrgEnterpriseAutoApprove":    "false",
		"OrgDefaultPriceGroup":        "wholesale",
	} {
		require.NoError(t, updateOptionMap(key, value), key)
		assert.Equal(t, value, common.OptionMap[key], key)
	}
	assert.True(t, setting.WonderGateEnabled)
	assert.False(t, setting.WonderGateSandbox)
	assert.Equal(t, "m-sandbox", setting.WonderGateSandboxMerchantId)
	assert.Equal(t, 7.25, setting.WonderGateUnitPrice)
	assert.Equal(t, 3, setting.WonderGateMinTopUp)
	assert.Equal(t, "HKD", setting.WonderGateCurrency)
	assert.Equal(t, "AlipayHK", setting.WonderGateTransactionMethod)
	assert.True(t, setting.PersonalByokEnabled)
	assert.Equal(t, 0.15, setting.ByokFeeRatio)
	assert.False(t, OrgEnterpriseAutoApprove)
	assert.Equal(t, "wholesale", OrgDefaultPriceGroup)
}
