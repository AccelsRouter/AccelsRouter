package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reseller and BYOK traffic records metrics under private routing groups that
// are never in the group ratio map; the performance summaries must still be
// able to include them.
func TestGetPerfMetricPrivateGroups(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&PerfMetric{}))
	require.NoError(t, DB.Where("1 = 1").Delete(&PerfMetric{}).Error)
	for _, row := range []PerfMetric{
		{ModelName: "m", Group: "default", BucketTs: 1000, RequestCount: 1},
		{ModelName: "m", Group: "reseller-6", BucketTs: 1000, RequestCount: 1},
		{ModelName: "m", Group: "user-42", BucketTs: 1000, RequestCount: 1},
		{ModelName: "m", Group: "org-3", BucketTs: 1000, RequestCount: 1},
		{ModelName: "m", Group: "reseller-9", BucketTs: 10, RequestCount: 1}, // before the window
	} {
		require.NoError(t, DB.Create(&row).Error)
	}
	groups, err := GetPerfMetricPrivateGroups(500)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"reseller-6", "user-42", "org-3"}, groups)
}
