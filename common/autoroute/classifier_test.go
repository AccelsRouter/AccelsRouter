package autoroute

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func mustNew(t *testing.T, cfg Config) *Classifier {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// Labeled samples in Chinese and English. These are the cases the default
// configuration is meant to get right; add real traffic here as it is
// collected, and tune keywords and boundaries until they pass.
var samples = []struct {
	name string
	text string
	want Tier
}{
	// simple
	{"zh greeting", "你好", TierSimple},
	{"zh what-is", "什么是TCP协议？", TierSimple},
	{"zh translate", "翻译成英文：今天天气很好", TierSimple},
	{"en greeting", "Hello!", TierSimple},
	{"en what-is", "What is the capital of France?", TierSimple},
	// medium: one clear code signal
	{"zh code request", "请帮我写一个 Python 函数，实现快速排序，并给出单元测试", TierMedium},
	{"zh code bug next to ascii", "这段代码出现bug了，帮我看看", TierMedium},
	{"en code request", "Write a Python function that parses JSON and returns the keys", TierMedium},
	// medium: no keyword at all, ordinary length -> default tier
	{"zh rewrite", "帮我把这段话润色一下，让它读起来更自然流畅，语气正式一些，但是不要改变原意，也不要增加新的内容", TierMedium},
	{"en rewrite", "Please rewrite the following paragraph so that it sounds more natural and professional.", TierMedium},
	// complex: code plus several technical terms
	{"zh complex design", "用Go实现一个分布式缓存，需要考虑一致性和高并发，写出核心代码和接口定义", TierComplex},
	{"en complex design", "Design a distributed cache in Python with an API, consistency guarantees and sharding, and write the code", TierComplex},
	// reasoning: two or more explicit reasoning markers
	{"zh reasoning", "请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊", TierReasoning},
	{"en reasoning", "Explain why this Go function deadlocks. Think step by step and compare the trade-offs of mutexes vs channels.", TierReasoning},
}

func TestSamples(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			got := c.Classify(Input{Text: s.text})
			if got.Tier != s.want {
				t.Errorf("tier = %s, want %s\n  score=%.3f cause=%s tokens=%d\n  signals=%v",
					got.Tier, s.want, got.Score, got.Cause, got.EstimatedTokens, got.Signals)
			}
		})
	}
}

func TestCauses(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	cases := []struct {
		text string
		want string
	}{
		{"请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊", CauseReasoningOverride},
		{"Please rewrite the following paragraph so that it sounds more natural and professional.", CauseNoSignal},
		{"请帮我写一个 Python 函数，实现快速排序，并给出单元测试", CauseScore},
	}
	for _, tc := range cases {
		if got := c.Classify(Input{Text: tc.text}); got.Cause != tc.want {
			t.Errorf("%q: cause = %s, want %s", tc.text, got.Cause, tc.want)
		}
	}
}

func TestNoSignalTierIsConfigurable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NoSignalTier = TierSimple
	c := mustNew(t, cfg)
	got := c.Classify(Input{Text: "Please rewrite the following paragraph so that it sounds more natural and professional."})
	if got.Tier != TierSimple {
		t.Fatalf("with NoSignalTier=simple want simple, got %s", got.Tier)
	}
}

func TestLongPlainPromptIsNotSimple(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	long := strings.Repeat("这是一段没有任何特别含义的普通描述文字，", 60) // ~1000 CJK chars
	got := c.Classify(Input{Text: long})
	if got.Tier == TierSimple {
		t.Fatalf("a ~1000 character plain prompt must not be simple: %+v", got)
	}
}

func TestMinTierForToolsAndImages(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	if got := c.Classify(Input{Text: "你好"}); got.Tier != TierSimple {
		t.Fatalf("baseline want simple, got %s", got.Tier)
	}
	got := c.Classify(Input{Text: "你好", HasTools: true})
	if got.Tier != TierMedium || got.Cause != CauseMinTier {
		t.Errorf("tools: want medium/min_tier, got %s/%s", got.Tier, got.Cause)
	}
	got = c.Classify(Input{Text: "你好", HasImages: true})
	if got.Tier != TierMedium || got.Cause != CauseMinTier {
		t.Errorf("images: want medium/min_tier, got %s/%s", got.Tier, got.Cause)
	}
	// a request already above the minimum is untouched
	got = c.Classify(Input{Text: "请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊", HasTools: true})
	if got.Tier != TierReasoning || got.Cause != CauseReasoningOverride {
		t.Errorf("tools above min: want reasoning untouched, got %s/%s", got.Tier, got.Cause)
	}
}

func TestContainsWord(t *testing.T) {
	cases := []struct {
		text, kw string
		want     bool
	}{
		{"the capital of france", "api", false},
		{"debugging session", "bug", false},
		{"found a bug.", "bug", true},
		{"出现bug了", "bug", true},      // ascii keyword next to Chinese still matches
		{"请调用api接口", "api", true},    // ditto
		{"my_api_key", "api", false}, // underscore is a word byte
		{"trade-off analysis", "trade-off", true},
		{"api", "api", true},
		{"", "api", false},
	}
	for _, tc := range cases {
		if got := containsWord(tc.text, tc.kw); got != tc.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tc.text, tc.kw, got, tc.want)
		}
	}
}

func TestEstimateTokensCountsCJKPerCharacter(t *testing.T) {
	if got := estimateTokens(strings.Repeat("汉", 100)); got != 100 {
		t.Errorf("100 Chinese characters = %d tokens, want 100", got)
	}
	if got := estimateTokens("abcdefgh"); got != 2 {
		t.Errorf("8 ascii characters = %d tokens, want 2", got)
	}
	if got := estimateTokens("日本語のテキスト"); got != 8 {
		t.Errorf("Japanese = %d tokens, want 8", got)
	}
}

func TestQuestionAndMultiStepDimensions(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	r := c.Classify(Input{Text: "为什么会这样？怎么办？有没有别的办法？还能不能更快？"})
	if !contains(r.Signals, "4 questions") {
		t.Errorf("four full-width question marks should fire the question dimension, signals=%v", r.Signals)
	}
	r = c.Classify(Input{Text: "首先读取文件，然后解析内容，最后输出结果"})
	if !contains(r.Signals, "multi-step") {
		t.Errorf("首先...然后...最后 should fire multi-step, signals=%v", r.Signals)
	}
	r = c.Classify(Input{Text: "Do these:\n1. read the file\n2. parse it"})
	if !contains(r.Signals, "multi-step") {
		t.Errorf("numbered list should fire multi-step, signals=%v", r.Signals)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestDefaultConfigIsValidAndHasNoSingleCharacterKeywords(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	cfg.Keywords.Code = append(cfg.Keywords.Code, "类")
	if err := cfg.Validate(); err == nil {
		t.Fatal("a single-character keyword must be rejected")
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	mut := []struct {
		name string
		edit func(*Config)
	}{
		{"boundaries not ascending", func(c *Config) { c.Boundaries.MediumComplex = 0.10 }},
		{"negative weight", func(c *Config) { c.Weights.CodePresence = -1 }},
		{"token thresholds inverted", func(c *Config) { c.TokenThresholds.Complex = 5 }},
		{"override count zero", func(c *Config) { c.ReasoningOverrideCount = 0 }},
		{"invalid tier", func(c *Config) { c.NoSignalTier = Tier(9) }},
	}
	for _, m := range mut {
		cfg := DefaultConfig()
		m.edit(&cfg)
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: expected an error", m.name)
		}
	}
	cfg := DefaultConfig()
	cfg.MultiStepPatterns = []string{"("}
	if _, err := New(cfg); err == nil {
		t.Error("an invalid regular expression must be rejected")
	}
}

func TestJSONOverlayOnDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if err := json.Unmarshal([]byte(`{"noSignalTier":"simple","boundaries":{"simpleMedium":0.2,"mediumComplex":0.4,"complexReasoning":0.7}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.NoSignalTier != TierSimple || cfg.Boundaries.MediumComplex != 0.4 {
		t.Fatalf("overlay not applied: %+v", cfg)
	}
	if cfg.Weights.CodePresence != 0.30 || len(cfg.Keywords.Code) == 0 {
		t.Fatal("fields absent from the JSON must keep their defaults")
	}
	if err := json.Unmarshal([]byte(`{"noSignalTier":"bogus"}`), &cfg); err == nil {
		t.Fatal("an unknown tier name must be an error")
	}
	b, _ := json.Marshal(TierComplex)
	if string(b) != `"complex"` {
		t.Fatalf("tier marshals as %s", b)
	}
}

func TestDimensionsExplainTheDecision(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	r := c.Classify(Input{Text: "请帮我写一个 Python 函数，实现快速排序，并给出单元测试"})
	if len(r.Dimensions) != 7 {
		t.Fatalf("want 7 dimensions, got %d", len(r.Dimensions))
	}
	sum := 0.0
	for _, d := range r.Dimensions {
		sum += d.Score * d.Weight
	}
	if diff := sum - r.Score; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("dimensions do not add up to the score: %.6f vs %.6f", sum, r.Score)
	}
	if !contains(r.Signals, "code (python, 函数, 单元测试)") {
		t.Errorf("expected a code signal naming the matches, got %v", r.Signals)
	}
}

func TestConcurrentUse(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				for _, s := range samples {
					if got := c.Classify(Input{Text: s.text}); got.Tier != s.want {
						t.Errorf("%s: concurrent result %s, want %s", s.name, got.Tier, s.want)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkClassify(b *testing.B) {
	c, _ := New(DefaultConfig())
	text := "用Go实现一个分布式缓存，需要考虑一致性和高并发，写出核心代码和接口定义"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Classify(Input{Text: text})
	}
}

// Parity with LiteLLM's complexity router (MIT, Copyright (c) 2023 Berri AI).
// Where this package is unsure, it follows LiteLLM; these tests pin that down
// so a later change away from it is a deliberate one.
func TestLiteLLMParity(t *testing.T) {
	if len(litellmCodeKeywords) != 45 || len(litellmReasoningKeywords) != 19 ||
		len(litellmTechnicalKeywords) != 28 || len(litellmSimpleKeywords) != 26 {
		t.Fatalf("LiteLLM base lists changed size: code=%d reasoning=%d technical=%d simple=%d",
			len(litellmCodeKeywords), len(litellmReasoningKeywords), len(litellmTechnicalKeywords), len(litellmSimpleKeywords))
	}
	cfg := DefaultConfig()
	for name, pair := range map[string]struct{ base, got []string }{
		"code":      {litellmCodeKeywords, cfg.Keywords.Code},
		"reasoning": {litellmReasoningKeywords, cfg.Keywords.Reasoning},
		"technical": {litellmTechnicalKeywords, cfg.Keywords.Technical},
		"simple":    {litellmSimpleKeywords, cfg.Keywords.Simple},
	} {
		for _, kw := range pair.base {
			if !contains(pair.got, kw) {
				t.Errorf("%s: LiteLLM keyword %q is missing from the default list", name, kw)
			}
		}
	}
	// "define" stays a simple indicator, as in LiteLLM.
	if !contains(cfg.Keywords.Simple, "define") || !contains(cfg.Keywords.Simple, "定义") {
		t.Error(`"define" and its counterpart 定义 must stay simple indicators`)
	}
}

func TestLiteLLMMultiStepPatterns(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	for _, text := range []string{
		"first open the file, then save it",
		"see step 2 below",
		"1. read the file",
		"a) read the file",
	} {
		if r := c.Classify(Input{Text: text}); !contains(r.Signals, "multi-step") {
			t.Errorf("%q should fire multi-step, signals=%v", text, r.Signals)
		}
	}
}

// LiteLLM scores a short prompt as simple even when it is hard. This package
// keeps that behaviour (see the notes on short prompts); the test makes the
// choice visible.
func TestShortHardPromptStaysSimpleLikeLiteLLM(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	for _, text := range []string{"Prove that the square root of 2 is irrational.", "证明根号2是无理数"} {
		if got := c.Classify(Input{Text: text}); got.Tier != TierSimple {
			t.Errorf("%q: want simple (LiteLLM parity), got %s (score %.3f)", text, got.Tier, got.Score)
		}
	}
}
