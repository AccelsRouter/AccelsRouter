package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

// TestOrgWalletFundingCharge_Discount locks the invariant that a reseller
// customer's per-model retail discount scales the org-wallet charge (and only
// that), truncating via the shared quota helper, never over-charging, and never
// producing a credit from a positive charge.
func TestOrgWalletFundingCharge_Discount(t *testing.T) {
	cases := []struct {
		name  string
		ratio float64
		std   int
		want  int
	}{
		{"no discount (1.0) passes through", 1.0, 1000, 1000},
		{"zero ratio treated as no discount", 0, 1000, 1000},
		{"negative ratio treated as no discount", -0.5, 1000, 1000},
		{"ratio >1 treated as no discount", 1.5, 1000, 1000},
		{"deepseek 4折", 0.4, 1000, 400},
		{"9折", 0.9, 1000, 900},
		{"rounds to nearest (6.8 -> 7)", 0.4, 17, 7},   // was 6 under truncation
		{"rounds down below .5 (1.2 -> 1)", 0.4, 3, 1}, // 1.2 -> 1
		{"tiny charge rounds to 0", 0.4, 1, 0},         // 0.4 -> 0
		{"zero stays zero", 0.4, 0, 0},
		{"refund delta discounts symmetrically", 0.4, -1000, -400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &OrgWalletFunding{discountRatio: tc.ratio}
			got := o.charge(tc.std)
			assert.Equal(t, tc.want, got)
			// A positive standard charge must never become a credit (negative).
			if tc.std > 0 {
				assert.GreaterOrEqual(t, got, 0)
				assert.LessOrEqual(t, got, tc.std, "discount must never exceed standard")
			}
		})
	}
}

// A log row carries the ratios its request was charged at: the discount for
// every viewer, the reseller's wholesale under admin_info only (the customer
// never receives that), and for a task row the discount even when it is 1, so
// rows written before tasks recorded theirs can be told apart.
func TestOrgChargeIsRecordedOnTheLog(t *testing.T) {
	text := model.NewLogOther()
	attachOrgRetailDiscount(&relaycommon.RelayInfo{OrgId: 9, ResellerOrgId: 3, OrgDiscountRatio: 0.9, OrgWholesaleRatio: 0.7}, text, 500)
	assert.Equal(t, map[string]any{
		"org_discount_ratio": 0.9, "org_charged_quota": 450,
		"admin_info": map[string]any{"org_wholesale_ratio": 0.7, "org_cost_quota": 350},
	}, text.Snapshot())

	own := model.NewLogOther()
	attachOrgRetailDiscount(&relaycommon.RelayInfo{OrgId: 3, ResellerOrgId: 3, OrgDiscountRatio: 0.8, OrgWholesaleRatio: 0.8}, own, 1000)
	assert.Equal(t, map[string]any{"org_discount_ratio": 0.8, "org_charged_quota": 800}, own.Snapshot(), "a reseller's own call is one debit")

	undiscounted := model.NewLogOther()
	attachOrgRetailDiscount(&relaycommon.RelayInfo{OrgId: 9, OrgDiscountRatio: 1}, undiscounted, 500)
	assert.Empty(t, undiscounted.Snapshot())

	task := &model.Task{}
	task.PrivateData.OrgFunding = &model.TaskOrgFunding{OrgId: 9, DiscountRatio: 1, ResellerOrgId: 3, WholesaleRatio: 0.7}
	settled := model.NewLogOther()
	attachTaskOrgCharge(task, settled, 200)
	assert.Equal(t, map[string]any{
		"org_discount_ratio": 1.0, "org_charged_quota": 200,
		"admin_info": map[string]any{"org_wholesale_ratio": 0.7, "org_cost_quota": 140},
	}, settled.Snapshot())

	personal := model.NewLogOther()
	attachTaskOrgCharge(&model.Task{}, personal, 200)
	assert.Empty(t, personal.Snapshot(), "a task billed to a personal balance records nothing")
}
