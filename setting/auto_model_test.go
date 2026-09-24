package setting

import (
	"strings"
	"testing"

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
