package controller

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseUsageWindow must bound the reporting window: default to the last 30 days,
// reject an inverted range, and reject any span wider than one year so a report
// or invoice never scans an unbounded slice of the log DB.
func TestParseUsageWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func(query string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/usage?"+query, nil)
		return c
	}
	s := func(v int64) string { return strconv.FormatInt(v, 10) }

	// No params → last 30 days, ending ~now.
	from, to, ok := parseUsageWindow(newCtx(""))
	require.True(t, ok)
	assert.InDelta(t, time.Now().Unix(), to, 5)
	assert.Equal(t, usageDefaultSpan, to-from)

	// Explicit valid range is preserved.
	from, to, ok = parseUsageWindow(newCtx("from=1000&to=5000"))
	require.True(t, ok)
	assert.Equal(t, int64(1000), from)
	assert.Equal(t, int64(5000), to)

	// Only 'from' (recent) → end defaults to now, within the max span.
	_, _, ok = parseUsageWindow(newCtx("from=" + s(time.Now().Unix()-3600)))
	assert.True(t, ok)

	// Inverted range is rejected.
	_, _, ok = parseUsageWindow(newCtx("from=5000&to=1000"))
	assert.False(t, ok, "from after to must be rejected")

	// A span wider than one year is rejected.
	_, _, ok = parseUsageWindow(newCtx("from=1&to=" + s(usageMaxSpan+100)))
	assert.False(t, ok, "an oversized span must be rejected")
}
