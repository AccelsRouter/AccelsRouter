package service

import (
	"testing"

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
		{"truncates, never rounds up", 0.4, 3, 1}, // 1.2 -> 1
		{"tiny charge truncates to 0", 0.4, 1, 0}, // 0.4 -> 0
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
