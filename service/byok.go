package service

// Fork-only helper: manage the group-ratio entry that prices a BYOK private
// group. The group ratio IS the BYOK fee (0 = free), so a BYOK group must have
// a ratio entry both to be priced correctly and to pass the request-time
// ContainsGroupRatio check. These helpers add the entry on first BYOK channel
// and drop it when the user's last BYOK channel is removed. Writes go through
// the normal option-persist path (model.UpdateOption -> in-memory apply), so
// nothing about the existing group-ratio machinery changes.

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// byokGroupRatioMu serializes the read-modify-write of the shared group-ratio
// option so concurrent BYOK create/delete calls can't clobber each other.
var byokGroupRatioMu sync.Mutex

// EnsureByokGroupRatio registers a ratio for a BYOK private group if it has
// none, defaulting to setting.ByokFeeRatio (0 = free). Idempotent: an existing
// ratio (possibly an admin per-user override) is never overwritten.
func EnsureByokGroupRatio(group string) error {
	if group == "" {
		return nil
	}
	byokGroupRatioMu.Lock()
	defer byokGroupRatioMu.Unlock()
	if ratio_setting.ContainsGroupRatio(group) {
		return nil
	}
	m := ratio_setting.GetGroupRatioCopy()
	m[group] = setting.ByokFeeRatio
	return persistGroupRatioMap(m)
}

// RemoveByokGroupRatio drops a BYOK private group's ratio entry when the user's
// last BYOK channel is deleted — but ONLY if it still equals the current
// default. An admin per-user fee override (value != default) is deliberately
// preserved, so a user cannot reset their fee to 0 by deleting and recreating
// their last channel. A missing entry is a no-op.
func RemoveByokGroupRatio(group string) error {
	if group == "" {
		return nil
	}
	byokGroupRatioMu.Lock()
	defer byokGroupRatioMu.Unlock()
	m := ratio_setting.GetGroupRatioCopy()
	cur, ok := m[group]
	if !ok || cur != setting.ByokFeeRatio {
		// Absent, or an admin override — leave it in place.
		return nil
	}
	delete(m, group)
	return persistGroupRatioMap(m)
}

func persistGroupRatioMap(m map[string]float64) error {
	b, err := common.Marshal(m)
	if err != nil {
		return err
	}
	return model.UpdateOption("GroupRatio", string(b))
}
