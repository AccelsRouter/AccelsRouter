// Fork-local availability probe for auto virtual models (see
// setting/auto_model.go). Lives in package model to reuse the in-memory
// channel cache guarded by channelSyncLock.
package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// GroupModelHasEnabledChannel reports whether at least one enabled channel can
// serve model for group (path-aware, mirroring GetRandomSatisfiedChannel's
// lookup: exact model name first, then the normalized matching name). Used by
// auto-model resolution to skip candidates that cannot be served right now.
func GroupModelHasEnabledChannel(group string, model string, requestPath string) bool {
	if group == "" || model == "" {
		return false
	}
	if !common.MemoryCacheEnabled {
		var count int64
		DB.Table("abilities").
			Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
			Count(&count)
		return count > 0
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if len(filterChannelsByRequestPathAndModel(group2model2channels[group][model], requestPath, model)) > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(model)
	if normalized == model {
		return false
	}
	return len(filterChannelsByRequestPathAndModel(group2model2channels[group][normalized], requestPath, model)) > 0
}
