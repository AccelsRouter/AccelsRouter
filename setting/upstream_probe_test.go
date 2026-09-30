package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The probe config is validated and normalised on install: channel ids must
// be positive, a per-channel interval must be 5..1440 (0 = global), no-op
// channel entries are dropped, paused models are lower-cased and de-duped,
// and a bad payload leaves the previous config in place.
func TestUpdateUpstreamProbeConfig(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, UpdateUpstreamProbeConfig("")) })

	require.NoError(t, UpdateUpstreamProbeConfig(`{
		"channels": {"5": {"paused": true}, "7": {"minutes": 30}, "9": {}},
		"paused_models": [" GLM-5 ", "glm-5", "deepseek-v4-pro"]
	}`))
	paused, minutes := UpstreamProbeChannel(5)
	assert.True(t, paused)
	assert.Zero(t, minutes)
	paused, minutes = UpstreamProbeChannel(7)
	assert.False(t, paused)
	assert.Equal(t, 30.0, minutes)
	_, ok := GetUpstreamProbeConfig().Channels["9"]
	assert.False(t, ok, "a no-op entry is dropped")
	assert.Equal(t, []string{"glm-5", "deepseek-v4-pro"}, GetUpstreamProbeConfig().PausedModels)
	assert.True(t, UpstreamProbeModelPaused("GLM-5"))
	assert.False(t, UpstreamProbeModelPaused("glm-5-2"), "exact match only")

	for _, bad := range []string{
		`{"channels": {"0": {"paused": true}}}`,
		`{"channels": {"x": {"paused": true}}}`,
		`{"channels": {"5": {"minutes": 1}}}`,
		`{"channels": {"5": {"minutes": 100000}}}`,
		`not json`,
	} {
		require.Error(t, UpdateUpstreamProbeConfig(bad), bad)
		paused, _ = UpstreamProbeChannel(5)
		assert.True(t, paused, "previous config kept after a rejected update")
	}

	require.NoError(t, UpdateUpstreamProbeConfig(""))
	paused, _ = UpstreamProbeChannel(5)
	assert.False(t, paused)
	assert.Equal(t, `{"channels":{},"paused_models":[]}`, UpstreamProbeConfigJSON())
}
