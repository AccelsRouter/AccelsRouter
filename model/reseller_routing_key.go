package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// GetAffinityKey is the sticky counterpart of GetNextEnabledKey, used only for
// reseller-routed requests carrying an affinity hash. A multi-key channel
// returns the enabled key the hash selects instead of rotating, so the same
// customer+model keeps hitting the same upstream account — and its provider-side
// prompt cache. Disabled keys are skipped with exactly GetNextEnabledKey's
// status semantics (a key missing from the status list counts as enabled), and
// the channel's polling index is never touched, so rotation for every other
// request is unaffected. A single-key channel behaves identically to
// GetNextEnabledKey (including BYOK decryption).
func (channel *Channel) GetAffinityKey(affinity uint64) (string, int, *types.NewAPIError) {
	if !channel.ChannelInfo.IsMultiKey {
		return channel.GetNextEnabledKey()
	}
	keys := channel.GetKeys()
	if len(keys) == 0 {
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if status, ok := statusList[i]; !ok || status == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}
	idx := enabledIdx[int(affinity%uint64(len(enabledIdx)))]
	return keys[idx], idx, nil
}
