package autoroute

import (
	"reflect"
	"testing"
)

func tiers(simple, medium, complex, reasoning []string) TierModels {
	var t TierModels
	t.Set(TierSimple, simple)
	t.Set(TierMedium, medium)
	t.Set(TierComplex, complex)
	t.Set(TierReasoning, reasoning)
	return t
}

func TestFailoverPlan(t *testing.T) {
	full := tiers([]string{"mini", "flash"}, []string{"gpt4o"}, []string{"sonnet", "gpt4o"}, []string{"o3"})
	cases := []struct {
		name string
		t    TierModels
		tier Tier
		want []string
	}{
		{"simple: own list, then every stronger tier, deduped", full, TierSimple, []string{"mini", "flash", "gpt4o", "sonnet", "o3"}},
		{"complex: never falls back to a weaker tier", full, TierComplex, []string{"sonnet", "gpt4o", "o3"}},
		{"reasoning: only its own list", full, TierReasoning, []string{"o3"}},
		{"empty tier uses the nearest stronger list", tiers(nil, nil, []string{"sonnet"}, []string{"o3"}), TierSimple, []string{"sonnet", "o3"}},
		{"nothing stronger: uses the nearest weaker list", tiers([]string{"mini"}, []string{"gpt4o"}, nil, nil), TierReasoning, []string{"gpt4o"}},
		{"all empty", TierModels{}, TierMedium, nil},
	}
	for _, tc := range cases {
		if got := tc.t.FailoverPlan(tc.tier); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPlanFromKeepsTheFirstModelFirst(t *testing.T) {
	full := tiers([]string{"mini"}, []string{"gpt4o"}, []string{"sonnet"}, []string{"o3"})
	got := full.PlanFrom("sonnet", TierComplex)
	if want := []string{"sonnet", "o3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// a model that is not in the plan at all still goes first
	if got := full.PlanFrom("legacy", TierSimple); got[0] != "legacy" || len(got) != 5 {
		t.Errorf("unexpected plan %v", got)
	}
}

func TestAllAndTierOf(t *testing.T) {
	m := tiers([]string{"a"}, []string{"b", "a"}, nil, []string{"c"})
	if got, want := m.All(), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("All = %v, want %v", got, want)
	}
	if tier, ok := m.TierOf("a"); !ok || tier != TierSimple {
		t.Errorf("TierOf(a) = %v %v, want the weakest tier that lists it", tier, ok)
	}
	if _, ok := m.TierOf("zzz"); ok {
		t.Error("TierOf must report a model that is in no list")
	}
}

func TestSetCopiesItsInput(t *testing.T) {
	src := []string{"a"}
	var m TierModels
	m.Set(TierSimple, src)
	src[0] = "changed"
	if m[TierSimple][0] != "a" {
		t.Error("Set must copy the slice")
	}
	m.Set(Tier(9), []string{"x"}) // out of range: ignored, no panic
}

func TestStickyRoundTrip(t *testing.T) {
	models := []string{"gpt-4o-mini", "gpt-4o", "claude-sonnet-4", "o3"}
	for tier := TierSimple; tier <= TierReasoning; tier++ {
		for _, m := range models {
			v := EncodeSticky(tier, m)
			if v <= 0 || v >= 1<<31 {
				t.Fatalf("value %d does not fit a positive 32-bit int", v)
			}
			gotTier, h, ok := DecodeSticky(v)
			if !ok || gotTier != tier {
				t.Fatalf("round trip of %s/%s: tier=%v ok=%v", tier, m, gotTier, ok)
			}
			if found, ok := FindByHash(models, h); !ok || found != m {
				t.Fatalf("FindByHash(%s) = %q %v", m, found, ok)
			}
		}
	}
	for _, bad := range []int{0, -1, 1, 1 << 28 >> 1, 6 << 28} {
		if _, _, ok := DecodeSticky(bad); ok {
			t.Errorf("DecodeSticky(%d) must not be a record", bad)
		}
	}
	if _, ok := FindByHash(models, ModelHash("something-else")); ok {
		t.Error("a hash that matches no candidate must be reported as absent")
	}
}

func TestParseConfigOverlay(t *testing.T) {
	cfg, err := ParseConfigOverlay([]byte(`{"noSignalTier":"simple","boundaries":{"simpleMedium":0.2,"mediumComplex":0.4,"complexReasoning":0.7}}`))
	if err != nil || cfg.NoSignalTier != TierSimple || cfg.Boundaries.MediumComplex != 0.4 || cfg.Weights.CodePresence != 0.30 {
		t.Fatalf("overlay not applied: %+v err=%v", cfg, err)
	}
	for _, empty := range []string{"", "  ", "null"} {
		if cfg, err := ParseConfigOverlay([]byte(empty)); err != nil || cfg.NoSignalTier != TierMedium {
			t.Errorf("%q must mean no overrides: %v", empty, err)
		}
	}
	for name, bad := range map[string]string{
		"unknown field":      `{"noSignalTeir":"simple"}`,
		"bad tier":           `{"noSignalTier":"bogus"}`,
		"trailing data":      `{"noSignalTier":"simple"} {}`,
		"invalid boundaries": `{"boundaries":{"simpleMedium":0.5,"mediumComplex":0.4,"complexReasoning":0.7}}`,
		"single char word":   `{"keywords":{"code":["类"],"reasoning":["推理"],"technical":["架构"],"simple":["你好"]}}`,
		"not an object":      `[1]`,
	} {
		if _, err := ParseConfigOverlay([]byte(bad)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// a list in the JSON replaces the default list as a whole
	cfg, err = ParseConfigOverlay([]byte(`{"keywords":{"code":["pytest"],"reasoning":["推理"],"technical":["架构"],"simple":["你好"]}}`))
	if err != nil || len(cfg.Keywords.Code) != 1 {
		t.Fatalf("keyword list should be replaced, not merged: %v %v", cfg.Keywords.Code, err)
	}
}
