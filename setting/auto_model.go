package setting

// Auto virtual models (fork-local). An "auto model" is an admin-curated,
// ordered pool of real models exposed under a single name (e.g. "auto-cn").
// At request time the gateway resolves the name to the first candidate with
// an available channel and, on channel exhaustion, fails over to the next
// candidate (see service/auto_model.go). Billing always uses the resolved
// concrete model, mirroring OpenRouter's `models` fallback semantics.
//
// Security invariants enforced here:
//   - config is admin-only (flows through the option API);
//   - strict shape validation with hard caps, so user traffic can never
//     drive unbounded candidate lists or malformed names into routing/logs;
//   - candidates may not reference auto models (no recursion);
//   - the parsed config is swapped atomically under a lock.

import (
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const (
	maxAutoModels          = 50
	maxAutoModelCandidates = 20
	maxModelNameLength     = 128
)

// autoModelNamePattern deliberately restricts names to a conservative charset:
// they appear in URLs, logs, metrics labels, and ability lookups.
var autoModelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,63}$`)

// AutoModelConfig is one virtual model definition. Candidates are tried in
// order (V1 strategy: ordered).
type AutoModelConfig struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

var (
	autoModelMutex   sync.RWMutex
	autoModelConfigs []AutoModelConfig
	autoModelIndex   map[string][]string // name -> candidates (validated)
	autoModelJSON    = "[]"
)

// UpdateAutoModelConfigs validates and installs a new config. On any
// validation error the previous config is kept untouched.
func UpdateAutoModelConfigs(jsonStr string) error {
	if jsonStr == "" {
		jsonStr = "[]"
	}
	var configs []AutoModelConfig
	if err := common.UnmarshalJsonStr(jsonStr, &configs); err != nil {
		return fmt.Errorf("invalid auto model config JSON: %w", err)
	}
	if len(configs) > maxAutoModels {
		return fmt.Errorf("at most %d auto models are allowed", maxAutoModels)
	}

	names := make(map[string]bool, len(configs))
	for i := range configs {
		cfg := &configs[i]
		if !autoModelNamePattern.MatchString(cfg.Name) {
			return fmt.Errorf("invalid auto model name %q (allowed: letters, digits, . _ : -, max 64 chars)", cfg.Name)
		}
		if names[cfg.Name] {
			return fmt.Errorf("duplicate auto model name %q", cfg.Name)
		}
		names[cfg.Name] = true
		if len(cfg.Models) == 0 {
			return fmt.Errorf("auto model %q has no candidate models", cfg.Name)
		}
		if len(cfg.Models) > maxAutoModelCandidates {
			return fmt.Errorf("auto model %q exceeds %d candidates", cfg.Name, maxAutoModelCandidates)
		}
		seen := make(map[string]bool, len(cfg.Models))
		for _, candidate := range cfg.Models {
			if candidate == "" || len(candidate) > maxModelNameLength {
				return fmt.Errorf("auto model %q has an empty or over-long candidate", cfg.Name)
			}
			if seen[candidate] {
				return fmt.Errorf("auto model %q lists candidate %q twice", cfg.Name, candidate)
			}
			seen[candidate] = true
		}
	}
	// No recursion: a candidate must not be (or become) an auto model name.
	for _, cfg := range configs {
		for _, candidate := range cfg.Models {
			if names[candidate] {
				return errors.New("auto model candidates must be real models, not other auto models")
			}
		}
	}

	index := make(map[string][]string, len(configs))
	for _, cfg := range configs {
		candidates := make([]string, len(cfg.Models))
		copy(candidates, cfg.Models)
		index[cfg.Name] = candidates
	}

	autoModelMutex.Lock()
	defer autoModelMutex.Unlock()
	autoModelConfigs = configs
	autoModelIndex = index
	autoModelJSON = jsonStr
	return nil
}

// IsAutoModel reports whether name is a configured auto model.
func IsAutoModel(name string) bool {
	autoModelMutex.RLock()
	defer autoModelMutex.RUnlock()
	_, ok := autoModelIndex[name]
	return ok
}

// GetAutoModelCandidates returns a copy of the ordered candidate list.
func GetAutoModelCandidates(name string) ([]string, bool) {
	autoModelMutex.RLock()
	defer autoModelMutex.RUnlock()
	candidates, ok := autoModelIndex[name]
	if !ok {
		return nil, false
	}
	out := make([]string, len(candidates))
	copy(out, candidates)
	return out, true
}

// AutoModelNames returns the configured auto model names, in config order.
func AutoModelNames() []string {
	autoModelMutex.RLock()
	defer autoModelMutex.RUnlock()
	names := make([]string, 0, len(autoModelConfigs))
	for _, cfg := range autoModelConfigs {
		names = append(names, cfg.Name)
	}
	return names
}

// AutoModelConfigsJSON returns the raw JSON as stored, for the option map.
func AutoModelConfigsJSON() string {
	autoModelMutex.RLock()
	defer autoModelMutex.RUnlock()
	return autoModelJSON
}
