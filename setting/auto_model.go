package setting

// Auto virtual models (fork-local). An "auto model" is an admin-curated set of
// real models exposed under a single name (e.g. "auto-cn"). At request time
// the gateway resolves the name to one concrete model (see
// service/auto_model.go) and, on channel exhaustion, fails over to the next
// candidate. Billing always uses the resolved concrete model, mirroring
// OpenRouter's `models` fallback semantics.
//
// Two shapes exist:
//
//   - fixed order: {"name": "auto-cn", "models": ["a", "b"]}. The first
//     candidate with an available channel wins; the request's content plays
//     no part.
//   - by content: {"name": "auto", "tiers": {"simple": [...], "medium": [...],
//     "complex": [...], "reasoning": [...]}, "classifier": {...}}. A local
//     classifier (common/autoroute) rates the request's difficulty, and the
//     matching tier's list is used. "classifier" is optional: JSON overrides
//     on top of the built-in defaults (see autoroute.ParseConfigOverlay).
//
// Security invariants enforced here:
//   - config is admin-only (flows through the option API);
//   - strict shape validation with hard caps, so user traffic can never
//     drive unbounded candidate lists or malformed names into routing/logs;
//   - candidates may not reference auto models (no recursion);
//   - the parsed config is swapped atomically under a lock.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/autoroute"
)

const (
	maxAutoModels          = 50
	maxAutoModelCandidates = 20 // per list: a fixed pool, or one tier
	maxModelNameLength     = 128
	maxClassifierJSONBytes = 64 << 10
)

// autoModelNamePattern deliberately restricts names to a conservative charset:
// they appear in URLs, logs, metrics labels, and ability lookups.
var autoModelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,63}$`)

// AutoModelTiers is the ordered candidate list of each difficulty tier. A tier
// may be left empty: its requests use the nearest stronger tier's list (or,
// when nothing is stronger, the nearest weaker one).
type AutoModelTiers struct {
	Simple    []string `json:"simple,omitempty"`
	Medium    []string `json:"medium,omitempty"`
	Complex   []string `json:"complex,omitempty"`
	Reasoning []string `json:"reasoning,omitempty"`
}

// AutoModelConfig is one virtual model definition: either Models (fixed
// order) or Tiers (by content), not both.
type AutoModelConfig struct {
	Name       string          `json:"name"`
	Models     []string        `json:"models,omitempty"`
	Tiers      *AutoModelTiers `json:"tiers,omitempty"`
	Classifier json.RawMessage `json:"classifier,omitempty"`
}

// AutoModel is the validated, immutable runtime form of one definition.
type AutoModel struct {
	name       string
	fixed      []string
	tiers      autoroute.TierModels
	classifier *autoroute.Classifier
}

// Name is the virtual model name users request.
func (m *AutoModel) Name() string { return m.name }

// IsTiered reports whether requests are routed by content.
func (m *AutoModel) IsTiered() bool { return m.classifier != nil }

// Classifier is the request classifier of a tiered model, nil for a fixed one.
func (m *AutoModel) Classifier() *autoroute.Classifier { return m.classifier }

// FixedCandidates returns a copy of a fixed-order model's list.
func (m *AutoModel) FixedCandidates() []string {
	return append([]string(nil), m.fixed...)
}

// AllCandidates returns every real model this auto model can resolve to
// (a fixed list, or the tiers' union, weakest tier first). The slice is a copy.
func (m *AutoModel) AllCandidates() []string {
	if !m.IsTiered() {
		return m.FixedCandidates()
	}
	return m.tiers.All()
}

// FailoverPlan is the order in which models are tried for a request rated at
// tier: the tier's list, then the stronger tiers'. It never goes down to a
// weaker tier. See autoroute.TierModels.FailoverPlan.
func (m *AutoModel) FailoverPlan(tier autoroute.Tier) []string { return m.tiers.FailoverPlan(tier) }

// PlanFrom is FailoverPlan with first moved to the front.
func (m *AutoModel) PlanFrom(first string, tier autoroute.Tier) []string {
	return m.tiers.PlanFrom(first, tier)
}

// TierOf returns the weakest tier whose list contains model.
func (m *AutoModel) TierOf(model string) (autoroute.Tier, bool) { return m.tiers.TierOf(model) }

var (
	autoModelMutex   sync.RWMutex
	autoModelConfigs []AutoModelConfig
	autoModelIndex   map[string]*AutoModel
	autoModelJSON    = "[]"
)

func hasClassifierJSON(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

// validateCandidates checks one ordered list. label names the list in errors
// ("" for a fixed pool, a tier name otherwise).
func validateCandidates(name, label string, list []string) error {
	where := fmt.Sprintf("auto model %q", name)
	if label != "" {
		where = fmt.Sprintf("auto model %q tier %q", name, label)
	}
	if len(list) > maxAutoModelCandidates {
		return fmt.Errorf("%s exceeds %d candidates", where, maxAutoModelCandidates)
	}
	seen := make(map[string]bool, len(list))
	for _, candidate := range list {
		if candidate == "" || len(candidate) > maxModelNameLength {
			return fmt.Errorf("%s has an empty or over-long candidate", where)
		}
		if seen[candidate] {
			return fmt.Errorf("%s lists candidate %q twice", where, candidate)
		}
		seen[candidate] = true
	}
	return nil
}

// buildAutoModel validates one definition and returns its runtime form.
func buildAutoModel(cfg AutoModelConfig) (*AutoModel, error) {
	if cfg.Tiers != nil && len(cfg.Models) > 0 {
		return nil, fmt.Errorf("auto model %q must use either models or tiers, not both", cfg.Name)
	}
	if cfg.Tiers == nil {
		if len(cfg.Models) == 0 {
			return nil, fmt.Errorf("auto model %q has no candidate models", cfg.Name)
		}
		if hasClassifierJSON(cfg.Classifier) {
			return nil, fmt.Errorf("auto model %q: classifier overrides apply only to tiers", cfg.Name)
		}
		if err := validateCandidates(cfg.Name, "", cfg.Models); err != nil {
			return nil, err
		}
		return &AutoModel{name: cfg.Name, fixed: append([]string(nil), cfg.Models...)}, nil
	}

	lists := []struct {
		tier   autoroute.Tier
		models []string
	}{
		{autoroute.TierSimple, cfg.Tiers.Simple},
		{autoroute.TierMedium, cfg.Tiers.Medium},
		{autoroute.TierComplex, cfg.Tiers.Complex},
		{autoroute.TierReasoning, cfg.Tiers.Reasoning},
	}
	am := &AutoModel{name: cfg.Name}
	total := 0
	for _, l := range lists {
		if err := validateCandidates(cfg.Name, l.tier.String(), l.models); err != nil {
			return nil, err
		}
		total += len(l.models)
		am.tiers.Set(l.tier, l.models)
	}
	if total == 0 {
		return nil, fmt.Errorf("auto model %q has no candidate models", cfg.Name)
	}

	raw := bytes.TrimSpace(cfg.Classifier)
	if len(raw) > maxClassifierJSONBytes {
		return nil, fmt.Errorf("auto model %q: classifier overrides exceed %d bytes", cfg.Name, maxClassifierJSONBytes)
	}
	classifierCfg, err := autoroute.ParseConfigOverlay(raw)
	if err != nil {
		return nil, fmt.Errorf("auto model %q: %w", cfg.Name, err)
	}
	am.classifier, err = autoroute.New(classifierCfg)
	if err != nil {
		return nil, fmt.Errorf("auto model %q: %w", cfg.Name, err)
	}
	return am, nil
}

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
	built := make([]*AutoModel, 0, len(configs))
	for i := range configs {
		cfg := configs[i]
		if !autoModelNamePattern.MatchString(cfg.Name) {
			return fmt.Errorf("invalid auto model name %q (allowed: letters, digits, . _ : -, max 64 chars)", cfg.Name)
		}
		if names[cfg.Name] {
			return fmt.Errorf("duplicate auto model name %q", cfg.Name)
		}
		names[cfg.Name] = true
		am, err := buildAutoModel(cfg)
		if err != nil {
			return err
		}
		built = append(built, am)
	}
	// No recursion: a candidate must not be (or become) an auto model name.
	for _, am := range built {
		for _, candidate := range am.AllCandidates() {
			if names[candidate] {
				return errors.New("auto model candidates must be real models, not other auto models")
			}
		}
	}

	index := make(map[string]*AutoModel, len(built))
	for _, am := range built {
		index[am.name] = am
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

// GetAutoModel returns the runtime form of a configured auto model. The value
// is immutable and safe to share across requests.
func GetAutoModel(name string) (*AutoModel, bool) {
	autoModelMutex.RLock()
	defer autoModelMutex.RUnlock()
	am, ok := autoModelIndex[name]
	return am, ok
}

// GetAutoModelCandidates returns a copy of every real model the auto model can
// resolve to: the ordered list of a fixed model, or the union of a tiered
// model's lists.
func GetAutoModelCandidates(name string) ([]string, bool) {
	am, ok := GetAutoModel(name)
	if !ok {
		return nil, false
	}
	return am.AllCandidates(), true
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
