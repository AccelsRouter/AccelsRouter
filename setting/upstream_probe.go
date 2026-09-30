package setting

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// Upstream probe tuning per channel and per model (fork). The upstream
// monitor sweeps every enabled channel's models on a global interval; this
// lets an admin pause the sweep for chosen channels or chosen models, and
// give a channel its own interval. Observation-only settings: they never
// touch channel status or routing. Stored as one JSON option
// (UpstreamProbeConfig) so it syncs across nodes like every other option.

type UpstreamProbeChannelConfig struct {
	// Paused skips the channel in scheduled and "probe all" sweeps. A manual
	// per-model probe still works.
	Paused bool `json:"paused,omitempty"`
	// Minutes overrides the global sweep interval for this channel
	// (0 = follow the global interval; otherwise 5..1440).
	Minutes float64 `json:"minutes,omitempty"`
}

type UpstreamProbeConfig struct {
	// Channels is keyed by channel id (as a JSON object key, hence string).
	Channels map[string]UpstreamProbeChannelConfig `json:"channels"`
	// PausedModels are model names skipped on every channel (exact,
	// case-insensitive).
	PausedModels []string `json:"paused_models"`
}

const (
	upstreamProbeMaxChannels = 500
	upstreamProbeMaxModels   = 2000
	UpstreamProbeMinMinutes  = 5
	UpstreamProbeMaxMinutes  = 1440
)

var (
	upstreamProbeMu     sync.RWMutex
	upstreamProbeConfig = UpstreamProbeConfig{Channels: map[string]UpstreamProbeChannelConfig{}}
	upstreamProbeRaw    = ""
	upstreamProbeModels = map[string]bool{}
)

// UpdateUpstreamProbeConfig validates and installs a config from its JSON
// form. Empty input clears everything. On any error the previous config is
// kept untouched.
func UpdateUpstreamProbeConfig(jsonStr string) error {
	jsonStr = strings.TrimSpace(jsonStr)
	cfg := UpstreamProbeConfig{Channels: map[string]UpstreamProbeChannelConfig{}}
	if jsonStr != "" && jsonStr != "{}" {
		if err := common.UnmarshalJsonStr(jsonStr, &cfg); err != nil {
			return fmt.Errorf("invalid upstream probe config: %w", err)
		}
	}
	if cfg.Channels == nil {
		cfg.Channels = map[string]UpstreamProbeChannelConfig{}
	}
	if len(cfg.Channels) > upstreamProbeMaxChannels {
		return fmt.Errorf("at most %d channel entries are allowed", upstreamProbeMaxChannels)
	}
	if len(cfg.PausedModels) > upstreamProbeMaxModels {
		return fmt.Errorf("at most %d paused models are allowed", upstreamProbeMaxModels)
	}
	channels := map[string]UpstreamProbeChannelConfig{}
	for k, v := range cfg.Channels {
		id, err := strconv.Atoi(strings.TrimSpace(k))
		if err != nil || id <= 0 {
			return errors.New("channel ids must be positive integers")
		}
		if v.Minutes != 0 && (v.Minutes < UpstreamProbeMinMinutes || v.Minutes > UpstreamProbeMaxMinutes) {
			return fmt.Errorf("channel %d: interval must be %d..%d minutes (0 = global)", id, UpstreamProbeMinMinutes, UpstreamProbeMaxMinutes)
		}
		if !v.Paused && v.Minutes == 0 {
			continue // no-op entry, drop it
		}
		channels[strconv.Itoa(id)] = v
	}
	models := make([]string, 0, len(cfg.PausedModels))
	set := map[string]bool{}
	for _, m := range cfg.PausedModels {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || set[m] {
			continue
		}
		set[m] = true
		models = append(models, m)
	}
	cfg.Channels = channels
	cfg.PausedModels = models
	raw, err := common.Marshal(cfg)
	if err != nil {
		return err
	}
	upstreamProbeMu.Lock()
	defer upstreamProbeMu.Unlock()
	upstreamProbeConfig = cfg
	upstreamProbeModels = set
	upstreamProbeRaw = string(raw)
	return nil
}

// UpstreamProbeConfigJSON returns the normalised JSON, for the option map.
func UpstreamProbeConfigJSON() string {
	upstreamProbeMu.RLock()
	defer upstreamProbeMu.RUnlock()
	if upstreamProbeRaw == "" {
		return `{"channels":{},"paused_models":[]}`
	}
	return upstreamProbeRaw
}

// GetUpstreamProbeConfig returns a copy of the current config.
func GetUpstreamProbeConfig() UpstreamProbeConfig {
	upstreamProbeMu.RLock()
	defer upstreamProbeMu.RUnlock()
	out := UpstreamProbeConfig{
		Channels:     make(map[string]UpstreamProbeChannelConfig, len(upstreamProbeConfig.Channels)),
		PausedModels: append([]string{}, upstreamProbeConfig.PausedModels...),
	}
	for k, v := range upstreamProbeConfig.Channels {
		out.Channels[k] = v
	}
	return out
}

// UpstreamProbeChannel reports a channel's override: paused, and its own
// interval in minutes (0 = follow the global interval).
func UpstreamProbeChannel(channelId int) (paused bool, minutes float64) {
	upstreamProbeMu.RLock()
	defer upstreamProbeMu.RUnlock()
	c, ok := upstreamProbeConfig.Channels[strconv.Itoa(channelId)]
	if !ok {
		return false, 0
	}
	return c.Paused, c.Minutes
}

// UpstreamProbeModelPaused reports whether sweeps skip this model everywhere.
func UpstreamProbeModelPaused(modelName string) bool {
	upstreamProbeMu.RLock()
	defer upstreamProbeMu.RUnlock()
	return upstreamProbeModels[strings.ToLower(strings.TrimSpace(modelName))]
}
