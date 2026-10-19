package autoroute

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Input is what the classifier looks at. Text must be what the caller wrote
// in the latest user message. Do not pass the system prompt: it is constant
// within a session, carries no information about how requests differ, and
// tends to contain exactly the keywords (agent, code, analyze) that would
// push every request into the same tier.
type Input struct {
	Text      string
	HasTools  bool // the request carries tool or function definitions
	HasImages bool // the request carries image inputs
}

// Dimension is one scored part of the result, kept so a decision can be
// explained in logs.
type Dimension struct {
	Name   string
	Score  float64 // -1 to 1 before weighting
	Weight float64
	Signal string // what fired, empty when nothing did
}

// Why a tier was chosen.
const (
	CauseScore             = "score"
	CauseReasoningOverride = "reasoning_override"
	CauseNoSignal          = "no_signal_default"
	CauseNoText            = "no_text"
	CauseMinTier           = "min_tier"
)

// Result is the classifier's decision.
type Result struct {
	Tier            Tier
	Score           float64
	Cause           string
	Signals         []string
	Dimensions      []Dimension
	EstimatedTokens int
}

// Classifier is immutable after New and safe for concurrent use.
type Classifier struct {
	cfg       Config
	code      keywordSet
	reasoning keywordSet
	technical keywordSet
	simple    keywordSet
	multiStep []*regexp.Regexp
}

// New validates cfg and prepares the matchers.
func New(cfg Config) (*Classifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	c := &Classifier{
		cfg:       cfg,
		code:      newKeywordSet(cfg.Keywords.Code),
		reasoning: newKeywordSet(cfg.Keywords.Reasoning),
		technical: newKeywordSet(cfg.Keywords.Technical),
		simple:    newKeywordSet(cfg.Keywords.Simple),
	}
	for _, p := range cfg.MultiStepPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("autoroute: bad multiStepPattern %q: %w", p, err)
		}
		c.multiStep = append(c.multiStep, re)
	}
	return c, nil
}

// Config returns the configuration in use.
func (c *Classifier) Config() Config { return c.cfg }

// Classify scores in and returns its tier.
func (c *Classifier) Classify(in Input) Result {
	// No text at all (an image-only turn, or a request the extractor could not
	// read): scoring "" would call it a short prompt and therefore simple. Use
	// the default tier instead, and keep the tool and image floors.
	if strings.TrimSpace(in.Text) == "" {
		tier, cause := c.cfg.NoSignalTier, CauseNoText
		if in.HasTools && tier < c.cfg.ToolsMinTier {
			tier, cause = c.cfg.ToolsMinTier, CauseMinTier
		}
		if in.HasImages && tier < c.cfg.ImagesMinTier {
			tier, cause = c.cfg.ImagesMinTier, CauseMinTier
		}
		return Result{Tier: tier, Cause: cause}
	}
	text := strings.ToLower(in.Text)
	estimated := estimateTokens(in.Text)
	w := c.cfg.Weights

	tokenScore, tokenSignal := scoreTokens(estimated, c.cfg.TokenThresholds)
	codeScore, codeSignal, _ := scoreKeywords(&c.code, text, "code", 1, 2, 0.5, 1.0)
	reasoningScore, reasoningSignal, reasoningCount := scoreKeywords(&c.reasoning, text, "reasoning", 1, 2, 0.7, 1.0)
	technicalScore, technicalSignal, _ := scoreKeywords(&c.technical, text, "technical", 2, 4, 0.5, 1.0)
	simpleScore, simpleSignal, _ := scoreKeywords(&c.simple, text, "simple", 1, 2, -1.0, -1.0)

	multiScore, multiSignal := 0.0, ""
	for _, re := range c.multiStep {
		if re.MatchString(text) {
			multiScore, multiSignal = 0.5, "multi-step"
			break
		}
	}
	questionScore, questionSignal := 0.0, ""
	if n := strings.Count(text, "?") + strings.Count(text, "？"); n > 3 {
		questionScore, questionSignal = 0.5, fmt.Sprintf("%d questions", n)
	}

	dims := []Dimension{
		{"tokenCount", tokenScore, w.TokenCount, tokenSignal},
		{"codePresence", codeScore, w.CodePresence, codeSignal},
		{"reasoningMarkers", reasoningScore, w.ReasoningMarkers, reasoningSignal},
		{"technicalTerms", technicalScore, w.TechnicalTerms, technicalSignal},
		{"simpleIndicators", simpleScore, w.SimpleIndicators, simpleSignal},
		{"multiStepPatterns", multiScore, w.MultiStepPatterns, multiSignal},
		{"questionComplexity", questionScore, w.QuestionComplexity, questionSignal},
	}
	total := 0.0
	var signals []string
	for _, d := range dims {
		total += d.Score * d.Weight
		if d.Signal != "" {
			signals = append(signals, d.Signal)
		}
	}

	tier, cause := c.tierForScore(total), CauseScore
	if reasoningCount >= c.cfg.ReasoningOverrideCount && total >= c.cfg.ReasoningOverrideMinScore {
		tier, cause = TierReasoning, CauseReasoningOverride
	}

	// Nothing recognisable in a request of ordinary length: use the default
	// tier instead of letting a score of zero mean "simple".
	contentFired := codeScore != 0 || reasoningScore != 0 || technicalScore != 0 ||
		simpleScore != 0 || multiScore != 0 || questionScore != 0
	if cause == CauseScore && !contentFired && tokenScore >= 0 {
		tier, cause = c.cfg.NoSignalTier, CauseNoSignal
	}

	if in.HasTools && tier < c.cfg.ToolsMinTier {
		tier, cause = c.cfg.ToolsMinTier, CauseMinTier
	}
	if in.HasImages && tier < c.cfg.ImagesMinTier {
		tier, cause = c.cfg.ImagesMinTier, CauseMinTier
	}

	return Result{Tier: tier, Score: total, Cause: cause, Signals: signals, Dimensions: dims, EstimatedTokens: estimated}
}

func (c *Classifier) tierForScore(score float64) Tier {
	b := c.cfg.Boundaries
	switch {
	case score < b.SimpleMedium:
		return TierSimple
	case score < b.MediumComplex:
		return TierMedium
	case score < b.ComplexReasoning:
		return TierComplex
	default:
		return TierReasoning
	}
}

func scoreTokens(estimated int, t TokenThresholds) (float64, string) {
	switch {
	case estimated < t.Simple:
		return -1.0, fmt.Sprintf("short (%d tokens)", estimated)
	case estimated > t.Complex:
		return 1.0, fmt.Sprintf("long (%d tokens)", estimated)
	}
	return 0, ""
}

// scoreKeywords returns the dimension score, a description of what matched,
// and the number of distinct keywords that matched.
func scoreKeywords(set *keywordSet, text, label string, low, high int, scoreLow, scoreHigh float64) (float64, string, int) {
	matched := set.matches(text)
	n := len(matched)
	if n < low {
		return 0, "", n
	}
	shown := matched
	if len(shown) > 3 {
		shown = shown[:3]
	}
	score := scoreLow
	if n >= high {
		score = scoreHigh
	}
	return score, fmt.Sprintf("%s (%s)", label, strings.Join(shown, ", ")), n
}

// estimateTokens counts a CJK character as one token and everything else at
// four characters per token. The plain four-characters rule would count a
// 100-character Chinese prompt as 25 tokens and call it short.
func estimateTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + other/4
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

type keyword struct {
	text      string
	substring bool // contains CJK or a space: match as a plain substring
}

type keywordSet struct{ items []keyword }

func newKeywordSet(list []string) keywordSet {
	seen := make(map[string]bool, len(list))
	var set keywordSet
	for _, raw := range list {
		kw := strings.ToLower(strings.TrimSpace(raw))
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		sub := strings.Contains(kw, " ")
		for _, r := range kw {
			if isCJK(r) {
				sub = true
				break
			}
		}
		set.items = append(set.items, keyword{text: kw, substring: sub})
	}
	return set
}

// matches returns the distinct keywords found in text (already lower-cased),
// in list order.
func (s *keywordSet) matches(text string) []string {
	var found []string
	for _, k := range s.items {
		if k.substring {
			if strings.Contains(text, k.text) {
				found = append(found, k.text)
			}
		} else if containsWord(text, k.text) {
			found = append(found, k.text)
		}
	}
	return found
}

// containsWord reports whether kw occurs in text bounded by non-word bytes.
// A word byte is an ASCII letter, digit or underscore, so "api" does not
// match "capital", while "bug" still matches in "出现bug了": bytes of Chinese
// characters are never word bytes.
func containsWord(text, kw string) bool {
	for start := 0; start <= len(text); {
		i := strings.Index(text[start:], kw)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(kw)
		if (i == 0 || !isWordByte(text[i-1])) && (end == len(text) || !isWordByte(text[end])) {
			return true
		}
		start = i + 1
	}
	return false
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}
