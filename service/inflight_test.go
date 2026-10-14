package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The in-flight registry must count a request exactly once from begin to
// finish, keep the hour's peaks after requests end, tolerate a double finish,
// and drop an entry whose handler never returned.
func TestInflightRegistryCountsPeaksAndPurgesStaleEntries(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	registry := &inflightRegistry{
		entries: map[string]*InflightEntry{},
		byUser:  map[int]int{},
		minutes: map[int64]*inflightMinute{},
		clock:   func() time.Time { return now },
	}

	finishA := registry.begin(&InflightEntry{RequestId: "a", UserId: 7, Model: "gpt"})
	finishB := registry.begin(&InflightEntry{RequestId: "b", UserId: 7, Model: "gpt"})
	finishC := registry.begin(&InflightEntry{RequestId: "c", UserId: 9, Model: "claude"})

	snap := registry.snapshot()
	require.Len(t, snap.Entries, 3)
	assert.Equal(t, 3, snap.PeakTotal)
	assert.Equal(t, map[string]int{"7": 2, "9": 1}, snap.PeakUsers)

	finishA()
	finishA() // a second finish must not touch another request's count
	finishB()
	snap = registry.snapshot()
	require.Len(t, snap.Entries, 1)
	assert.Equal(t, "c", snap.Entries[0].RequestId)
	assert.Equal(t, 3, snap.PeakTotal, "peak survives after requests end")
	assert.Equal(t, 2, snap.PeakUsers["7"])

	// A request that never finishes is dropped once it is older than the
	// stale limit; the hour peak is forgotten once the window moves on.
	now = now.Add(inflightStaleAfter + time.Minute)
	snap = registry.snapshot()
	assert.Empty(t, snap.Entries)
	assert.Equal(t, 3, snap.PeakTotal, "peak still inside the hour window")
	now = now.Add(inflightPeakWindow)
	finishD := registry.begin(&InflightEntry{RequestId: "d", UserId: 9})
	snap = registry.snapshot()
	assert.Equal(t, 1, snap.PeakTotal, "old minutes pruned")
	assert.Equal(t, map[string]int{"9": 1}, snap.PeakUsers)
	finishD()
	finishC()
	assert.Empty(t, registry.snapshot().Entries)
}
