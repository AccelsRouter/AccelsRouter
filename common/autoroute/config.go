// Package autoroute classifies a request by how demanding it is, so the
// gateway can send easy requests to a cheap model and hard ones to a strong
// one. It is a pure function of the request text: no network, no state, no
// dependency on the rest of the project, so it is safe to call on every
// request and trivial to test.
//
// The scoring design follows LiteLLM's complexity router (MIT licensed): a
// weighted sum over a few dimensions, mapped to four tiers by boundaries.
// It is reimplemented here for Chinese and English: keywords that contain
// CJK match as substrings (CJK has no spaces), ASCII keywords match on
// word boundaries even when they sit next to Chinese text, and token length
// counts each CJK character as a token instead of dividing by four.
package autoroute

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Tier is how demanding a request is.
type Tier int

const (
	TierSimple Tier = iota
	TierMedium
	TierComplex
	TierReasoning
)

var tierNames = [...]string{"simple", "medium", "complex", "reasoning"}

func (t Tier) String() string {
	if t < 0 || int(t) >= len(tierNames) {
		return "unknown"
	}
	return tierNames[t]
}

// ParseTier turns "simple", "medium", "complex" or "reasoning" back into a Tier.
func ParseTier(s string) (Tier, bool) {
	for i, name := range tierNames {
		if strings.EqualFold(strings.TrimSpace(s), name) {
			return Tier(i), true
		}
	}
	return TierSimple, false
}

// MarshalText and UnmarshalText let a Tier read and write as a string in JSON
// settings.
func (t Tier) MarshalText() ([]byte, error) {
	if t < 0 || int(t) >= len(tierNames) {
		return nil, fmt.Errorf("autoroute: invalid tier %d", int(t))
	}
	return []byte(tierNames[t]), nil
}

func (t *Tier) UnmarshalText(b []byte) error {
	parsed, ok := ParseTier(string(b))
	if !ok {
		return fmt.Errorf("autoroute: unknown tier %q", string(b))
	}
	*t = parsed
	return nil
}

// Weights are the share each dimension contributes to the score.
type Weights struct {
	TokenCount         float64 `json:"tokenCount"`
	CodePresence       float64 `json:"codePresence"`
	ReasoningMarkers   float64 `json:"reasoningMarkers"`
	TechnicalTerms     float64 `json:"technicalTerms"`
	SimpleIndicators   float64 `json:"simpleIndicators"`
	MultiStepPatterns  float64 `json:"multiStepPatterns"`
	QuestionComplexity float64 `json:"questionComplexity"`
}

// Boundaries map the score to a tier: below SimpleMedium is simple, below
// MediumComplex is medium, below ComplexReasoning is complex, else reasoning.
type Boundaries struct {
	SimpleMedium     float64 `json:"simpleMedium"`
	MediumComplex    float64 `json:"mediumComplex"`
	ComplexReasoning float64 `json:"complexReasoning"`
}

// TokenThresholds: a prompt shorter than Simple tokens is penalised, longer
// than Complex tokens is boosted.
type TokenThresholds struct {
	Simple  int `json:"simple"`
	Complex int `json:"complex"`
}

// Keywords are matched case-insensitively. Every keyword needs at least two
// characters: a single Chinese character such as 类 appears in 分类, 种类 and
// 类似, and would misfire on ordinary text.
type Keywords struct {
	Code      []string `json:"code"`
	Reasoning []string `json:"reasoning"`
	Technical []string `json:"technical"`
	Simple    []string `json:"simple"`
}

// Config is everything the classifier reads. DefaultConfig returns a working
// one; every field can be overridden from a JSON setting.
type Config struct {
	Weights           Weights         `json:"weights"`
	Boundaries        Boundaries      `json:"boundaries"`
	TokenThresholds   TokenThresholds `json:"tokenThresholds"`
	Keywords          Keywords        `json:"keywords"`
	MultiStepPatterns []string        `json:"multiStepPatterns"`

	// A request with at least ReasoningOverrideCount reasoning keywords is
	// promoted to the reasoning tier once its score reaches
	// ReasoningOverrideMinScore, so a short "think step by step" still counts
	// but a request the score calls simple is not rescued by a stray phrase.
	ReasoningOverrideCount    int     `json:"reasoningOverrideCount"`
	ReasoningOverrideMinScore float64 `json:"reasoningOverrideMinScore"`

	// NoSignalTier is the tier for a request of ordinary length in which no
	// keyword or structure was found at all. Plain rewriting, summarising and
	// chatting have no keywords, and scoring them as simple would send every
	// such request to the cheapest model. Set it to simple to get the plain
	// scorer's behaviour.
	NoSignalTier Tier `json:"noSignalTier"`

	// Requests that carry tool definitions, or images, never fall below
	// these tiers: tool calling is the first thing small models get wrong,
	// and the complexity of an image is invisible to a text scorer.
	ToolsMinTier  Tier `json:"toolsMinTier"`
	ImagesMinTier Tier `json:"imagesMinTier"`
}

// DefaultConfig is the built-in Chinese and English configuration.
func DefaultConfig() Config {
	return Config{
		Weights: Weights{
			TokenCount:         0.10,
			CodePresence:       0.30,
			ReasoningMarkers:   0.25,
			TechnicalTerms:     0.25,
			SimpleIndicators:   0.05,
			MultiStepPatterns:  0.03,
			QuestionComplexity: 0.02,
		},
		Boundaries:                Boundaries{SimpleMedium: 0.15, MediumComplex: 0.35, ComplexReasoning: 0.60},
		TokenThresholds:           TokenThresholds{Simple: 15, Complex: 400},
		Keywords:                  defaultKeywords(),
		MultiStepPatterns:         append([]string(nil), defaultMultiStepPatterns...),
		ReasoningOverrideCount:    2,
		ReasoningOverrideMinScore: 0.15,
		NoSignalTier:              TierMedium,
		ToolsMinTier:              TierMedium,
		ImagesMinTier:             TierMedium,
	}
}

// Validate reports the first problem that would make the classifier misbehave.
func (c Config) Validate() error {
	w := c.Weights
	for name, v := range map[string]float64{
		"tokenCount": w.TokenCount, "codePresence": w.CodePresence, "reasoningMarkers": w.ReasoningMarkers,
		"technicalTerms": w.TechnicalTerms, "simpleIndicators": w.SimpleIndicators,
		"multiStepPatterns": w.MultiStepPatterns, "questionComplexity": w.QuestionComplexity,
	} {
		if v < 0 {
			return fmt.Errorf("autoroute: weight %s must not be negative", name)
		}
	}
	b := c.Boundaries
	if !(0 < b.SimpleMedium && b.SimpleMedium < b.MediumComplex && b.MediumComplex < b.ComplexReasoning) {
		return errors.New("autoroute: boundaries must satisfy 0 < simpleMedium < mediumComplex < complexReasoning")
	}
	if c.TokenThresholds.Simple < 0 || c.TokenThresholds.Complex <= c.TokenThresholds.Simple {
		return errors.New("autoroute: tokenThresholds must satisfy 0 <= simple < complex")
	}
	if c.ReasoningOverrideCount < 1 {
		return errors.New("autoroute: reasoningOverrideCount must be at least 1")
	}
	for _, tier := range []Tier{c.NoSignalTier, c.ToolsMinTier, c.ImagesMinTier} {
		if tier < TierSimple || tier > TierReasoning {
			return fmt.Errorf("autoroute: invalid tier %d", int(tier))
		}
	}
	for group, list := range map[string][]string{
		"code": c.Keywords.Code, "reasoning": c.Keywords.Reasoning,
		"technical": c.Keywords.Technical, "simple": c.Keywords.Simple,
	} {
		for _, kw := range list {
			if utf8.RuneCountInString(strings.TrimSpace(kw)) < 2 {
				return fmt.Errorf("autoroute: keyword %q in %s must have at least two characters", kw, group)
			}
		}
	}
	return nil
}

// ParseConfigOverlay reads a JSON object of overrides on top of DefaultConfig.
// Fields left out keep their defaults, so {"noSignalTier":"simple"} changes
// one setting only. A list given in the JSON (keywords, multiStepPatterns)
// replaces the default list as a whole, it is not merged. Unknown fields are
// an error, so a misspelt name does not silently do nothing. Empty input or
// the JSON null mean no overrides. The result is validated.
func ParseConfigOverlay(raw []byte) (Config, error) {
	cfg := DefaultConfig()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return cfg, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("autoroute: invalid classifier overrides: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Config{}, errors.New("autoroute: classifier overrides must be a single JSON object")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
