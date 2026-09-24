package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerAffinityHashIsStableAndKeyed(t *testing.T) {
	a := ResellerAffinityHash(1, 42, "claude-opus-4-8")
	assert.Equal(t, a, ResellerAffinityHash(1, 42, " Claude-Opus-4-8 "), "case/space-insensitive on model")
	assert.NotEqual(t, a, ResellerAffinityHash(1, 43, "claude-opus-4-8"), "different customer")
	assert.NotEqual(t, a, ResellerAffinityHash(2, 42, "claude-opus-4-8"), "different reseller")
	assert.NotEqual(t, a, ResellerAffinityHash(1, 42, "claude-sonnet-5"), "different model")
}

func tierOf(ids []int, weights []uint) []resellerCandidate {
	out := make([]resellerCandidate, len(ids))
	for i, id := range ids {
		out[i] = resellerCandidate{channel: &model.Channel{Id: id}, weight: weights[i]}
	}
	return out
}

// The whole point of affinity: the same customer+model must land on the same
// upstream every time, and changing the channel set must only move the
// customers that were mapped to the changed channel.
func TestPickByRendezvousIsStickyAndMinimallyDisruptive(t *testing.T) {
	tier := tierOf([]int{1, 2, 3}, []uint{0, 0, 0})
	key := ResellerAffinityHash(1, 42, "m")
	first := pickByRendezvous(tier, key)
	for i := 0; i < 1000; i++ {
		assert.Same(t, first, pickByRendezvous(tier, key), "same key must always pick the same channel")
	}

	withExtra := append(append([]resellerCandidate{}, tier...), resellerCandidate{channel: &model.Channel{Id: 4}})
	moved := 0
	for cust := 0; cust < 2000; cust++ {
		k := ResellerAffinityHash(1, cust, "m")
		before := pickByRendezvous(tier, k)
		after := pickByRendezvous(withExtra, k)
		if after.Id != before.Id {
			moved++
			assert.Equal(t, 4, after.Id, "a key may only move TO the newly added channel, never between old ones")
		}
	}
	// Four equal-weight channels: about a quarter of keys re-home to the new one.
	assert.InDelta(t, 500, moved, 120)
}

// Weight keeps its meaning as a traffic SHARE, now measured across customers
// instead of across requests: 30 vs 10 (effective 40 vs 20) => ~2/3 vs ~1/3.
func TestPickByRendezvousHonoursWeightsAcrossCustomers(t *testing.T) {
	tier := tierOf([]int{1, 2}, []uint{30, 10})
	counts := map[int]int{}
	n := 6000
	for cust := 0; cust < n; cust++ {
		counts[pickByRendezvous(tier, ResellerAffinityHash(9, cust, "m")).Id]++
	}
	assert.InDelta(t, float64(n)*2/3, float64(counts[1]), float64(n)*0.05)
	assert.InDelta(t, float64(n)/3, float64(counts[2]), float64(n)*0.05)
}

func TestGroupByPriorityOrdersTiersHighestFirst(t *testing.T) {
	c := []resellerCandidate{
		{channel: &model.Channel{Id: 5}, priority: 1},
		{channel: &model.Channel{Id: 2}, priority: 10},
		{channel: &model.Channel{Id: 9}, priority: 10},
		{channel: &model.Channel{Id: 7}, priority: -3},
	}
	tiers := groupByPriority(c)
	require.Len(t, tiers, 3)
	assert.Equal(t, []int{2, 9}, []int{tiers[0][0].channel.Id, tiers[0][1].channel.Id}, "top tier, id-sorted")
	assert.Equal(t, 5, tiers[1][0].channel.Id)
	assert.Equal(t, 7, tiers[2][0].channel.Id)
	assert.Nil(t, groupByPriority(nil))
}
