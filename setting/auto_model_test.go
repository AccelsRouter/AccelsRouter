package setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common/autoroute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The auto-model config gates which model names user traffic can be rewritten
// to before billing, so its validation is a security boundary: malformed or
// recursive configs must never install, and a rejected update must leave the
// previous config intact.
func TestUpdateAutoModelConfigsValidation(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, UpdateAutoModelConfigs("[]")) })

	valid := `[{"name":"auto-cn","models":["deepseek-chat","qwen-max"]},{"name":"auto.global:v1","models":["gpt-4o"]}]`
	require.NoError(t, UpdateAutoModelConfigs(valid))
	assert.True(t, IsAutoModel("auto-cn"))
	assert.Equal(t, []string{"auto-cn", "auto.global:v1"}, AutoModelNames())
	candidates, ok := GetAutoModelCandidates("auto-cn")
	require.True(t, ok)
	assert.Equal(t, []string{"deepseek-chat", "qwen-max"}, candidates)

	// Returned slice must be a copy — mutating it must not poison the config.
	candidates[0] = "hacked"
	fresh, _ := GetAutoModelCandidates("auto-cn")
	assert.Equal(t, "deepseek-chat", fresh[0])

	rejected := []struct {
		name string
		json string
	}{
		{"invalid JSON", `{not json`},
		{"bad name charset", `[{"name":"auto cn","models":["a"]}]`},
		{"name starting with symbol", `[{"name":"-auto","models":["a"]}]`},
		{"name too long", `[{"name":"` + strings.Repeat("a", 65) + `","models":["a"]}]`},
		{"duplicate names", `[{"name":"auto-x","models":["a"]},{"name":"auto-x","models":["b"]}]`},
		{"no candidates", `[{"name":"auto-x","models":[]}]`},
		{"empty candidate", `[{"name":"auto-x","models":[""]}]`},
		{"candidate too long", `[{"name":"auto-x","models":["` + strings.Repeat("m", 129) + `"]}]`},
		{"duplicate candidate", `[{"name":"auto-x","models":["a","a"]}]`},
		{"recursion: candidate is an auto model", `[{"name":"auto-a","models":["auto-b"]},{"name":"auto-b","models":["m"]}]`},
		{"self recursion", `[{"name":"auto-a","models":["auto-a"]}]`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, UpdateAutoModelConfigs(tc.json))
			// The previously installed valid config must survive a rejected update.
			assert.True(t, IsAutoModel("auto-cn"), "rejected update must not clobber the active config")
		})
	}

	// Candidate count cap.
	many := `[{"name":"auto-x","models":[` + strings.TrimSuffix(strings.Repeat(`"m`, 0), ",") + func() string {
		parts := make([]string, 21)
		for i := range parts {
			parts[i] = `"m` + strings.Repeat("x", i+1) + `"`
		}
		return strings.Join(parts, ",")
	}() + `]}]`
	require.Error(t, UpdateAutoModelConfigs(many))

	// Empty string resets to no auto models.
	require.NoError(t, UpdateAutoModelConfigs(""))
	assert.False(t, IsAutoModel("auto-cn"))
	assert.Empty(t, AutoModelNames())
}

const tieredConfig = `[{"name":"auto","tiers":{"simple":["mini","flash"],"medium":["gpt4o"],"complex":["sonnet","gpt4o"],"reasoning":["o3"]}}]`

func TestTieredAutoModel(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, UpdateAutoModelConfigs("[]")) })
	require.NoError(t, UpdateAutoModelConfigs(tieredConfig))

	am, ok := GetAutoModel("auto")
	require.True(t, ok)
	assert.True(t, am.IsTiered())
	assert.NotNil(t, am.Classifier())
	assert.Equal(t, "auto", am.Name())
	assert.Equal(t, []string{"mini", "flash", "gpt4o", "sonnet", "o3"}, am.AllCandidates())

	// The legacy accessor lists every model the name can resolve to.
	all, ok := GetAutoModelCandidates("auto")
	require.True(t, ok)
	assert.Equal(t, []string{"mini", "flash", "gpt4o", "sonnet", "o3"}, all)
	all[0] = "hacked"
	fresh, _ := GetAutoModelCandidates("auto")
	assert.Equal(t, "mini", fresh[0], "returned slices must be copies")

	assert.Equal(t, []string{"sonnet", "gpt4o", "o3"}, am.FailoverPlan(autoroute.TierComplex), "never down to a weaker tier")
	assert.Equal(t, []string{"gpt4o", "sonnet", "o3"}, am.PlanFrom("gpt4o", autoroute.TierMedium), "kept model first, then the usual order")
	tier, found := am.TierOf("gpt4o")
	assert.True(t, found)
	assert.Equal(t, autoroute.TierMedium, tier)

	// A fixed-order model next to it keeps the old behaviour.
	require.NoError(t, UpdateAutoModelConfigs(`[{"name":"auto-cn","models":["a","b"]},`+tieredConfig[1:]))
	fixed, ok := GetAutoModel("auto-cn")
	require.True(t, ok)
	assert.False(t, fixed.IsTiered())
	assert.Nil(t, fixed.Classifier())
	assert.Equal(t, []string{"a", "b"}, fixed.FixedCandidates())
	assert.Equal(t, []string{"auto-cn", "auto"}, AutoModelNames())
}

func TestTieredAutoModelClassifierOverrides(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, UpdateAutoModelConfigs("[]")) })

	require.NoError(t, UpdateAutoModelConfigs(`[{"name":"auto","tiers":{"medium":["m"]},"classifier":{"noSignalTier":"simple"}}]`))
	am, _ := GetAutoModel("auto")
	assert.Equal(t, autoroute.TierSimple, am.Classifier().Config().NoSignalTier)
	assert.Equal(t, 0.30, am.Classifier().Config().Weights.CodePresence, "fields left out keep their defaults")

	// "classifier": null is the same as leaving it out.
	require.NoError(t, UpdateAutoModelConfigs(`[{"name":"auto","tiers":{"medium":["m"]},"classifier":null}]`))
	am, _ = GetAutoModel("auto")
	assert.Equal(t, autoroute.TierMedium, am.Classifier().Config().NoSignalTier)

	// With only one tier configured every request lands on it, whatever its rating.
	for tier := autoroute.TierSimple; tier <= autoroute.TierReasoning; tier++ {
		assert.Equal(t, []string{"m"}, am.FailoverPlan(tier)[:1])
	}
}

func TestTieredAutoModelValidation(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, UpdateAutoModelConfigs("[]")) })
	require.NoError(t, UpdateAutoModelConfigs(tieredConfig))

	twentyOne := func() string {
		parts := make([]string, 21)
		for i := range parts {
			parts[i] = `"m` + strings.Repeat("x", i+1) + `"`
		}
		return strings.Join(parts, ",")
	}()

	rejected := []struct {
		name string
		json string
	}{
		{"models and tiers together", `[{"name":"auto","models":["a"],"tiers":{"simple":["b"]}}]`},
		{"tiers with no list at all", `[{"name":"auto","tiers":{}}]`},
		{"every tier name misspelt", `[{"name":"auto","tiers":{"simpel":["a"]}}]`},
		{"duplicate in a tier", `[{"name":"auto","tiers":{"simple":["a","a"]}}]`},
		{"empty candidate in a tier", `[{"name":"auto","tiers":{"simple":[""]}}]`},
		{"too many in one tier", `[{"name":"auto","tiers":{"simple":[` + twentyOne + `]}}]`},
		{"candidate is an auto model", `[{"name":"auto","tiers":{"simple":["auto-b"]}},{"name":"auto-b","models":["m"]}]`},
		{"self recursion through a tier", `[{"name":"auto","tiers":{"reasoning":["auto"]}}]`},
		{"classifier on a fixed model", `[{"name":"auto-cn","models":["a"],"classifier":{"noSignalTier":"simple"}}]`},
		{"classifier with an unknown field", `[{"name":"auto","tiers":{"simple":["a"]},"classifier":{"noSignalTeir":"simple"}}]`},
		{"classifier with a bad tier name", `[{"name":"auto","tiers":{"simple":["a"]},"classifier":{"noSignalTier":"huge"}}]`},
		{"classifier with a one character keyword", `[{"name":"auto","tiers":{"simple":["a"]},"classifier":{"keywords":{"code":["类"],"reasoning":["推理"],"technical":["架构"],"simple":["你好"]}}}]`},
		{"classifier too large", `[{"name":"auto","tiers":{"simple":["a"]},"classifier":{"multiStepPatterns":["` + strings.Repeat("a", 70<<10) + `"]}}]`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, UpdateAutoModelConfigs(tc.json))
			am, ok := GetAutoModel("auto")
			require.True(t, ok, "a rejected update must not clobber the active config")
			assert.True(t, am.IsTiered())
			assert.Equal(t, []string{"mini", "flash", "gpt4o", "sonnet", "o3"}, am.AllCandidates())
		})
	}

	// The same model may appear in several tiers; only repeats within one list are an error.
	require.NoError(t, UpdateAutoModelConfigs(`[{"name":"auto","tiers":{"simple":["a"],"medium":["a"]}}]`))
}
