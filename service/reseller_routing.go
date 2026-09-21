package service

// Fork-only reseller upstream routing selector. Invoked from
// CacheGetRandomSatisfiedChannel when a request's TokenGroup is a reseller
// routing group (reseller-<id>) — which the distributor sets only for reseller
// customers whose reseller has bound channels, and only while the global switch
// setting.ResellerRoutingEnabled is on. Every other request, and every request
// when the switch is off, never reaches this file.
//
// Selection mirrors the platform selector's shape (priority tiers; retry index
// = tier index; weight shares traffic inside a tier) with two differences:
//
//   - priority/weight per (model, channel) come from the reseller's matrix
//     (exact model beats prefix, unmatched keeps the channel's own values);
//   - inside a tier, selection is STICKY by default: a weighted rendezvous
//     (highest-random-weight) hash of (reseller, customer org, model) ranks the
//     tier's channels deterministically, so the same customer+model always
//     lands on the same upstream while weights still hold as shares across
//     customers. Stateless — identical on every instance and across restarts,
//     and adding/removing a channel only moves the customers mapped to it.
//     The same hash also pins the key inside a multi-key channel (see
//     model.Channel.GetAffinityKey), because provider prompt caches live per
//     upstream account.

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// ResellerAffinityHash is the sticky-routing key for one customer org's use of
// one model under one reseller. Stable across processes (FNV-1a, no seed).
func ResellerAffinityHash(resellerOrgId, customerOrgId int, modelName string) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(resellerOrgId))
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(customerOrgId))
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(modelName))))
	return h.Sum64()
}

// rendezvousScore is the weighted highest-random-weight score of one channel
// for one affinity key. Higher wins. Effective weight is weight+10, the same
// smoothing the platform selector applies so a 0-weight channel still gets a
// share instead of vanishing.
//
// The per-(key, channel) draw goes through a SplitMix64 finaliser rather than a
// byte-stream hash: consecutive channel ids differ in a single byte, and a weak
// mixer leaves their draws correlated, which breaks the property that adding a
// channel re-homes only the keys that channel wins.
func rendezvousScore(affinity uint64, channelId int, weight uint) float64 {
	x := affinity ^ (uint64(channelId)+1)*0x9E3779B97F4A7C15
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	x ^= x >> 31
	// Map the top 53 bits into (0,1) exclusive so ln(u) is finite and non-zero.
	u := (float64(x>>11) + 0.5) / float64(uint64(1)<<53)
	return -float64(weight+10) / math.Log(u)
}

type resellerCandidate struct {
	channel  *model.Channel
	priority int64
	weight   uint
}

// SelectResellerChannel picks a channel for a reseller-routed request.
// Return conventions match model.GetRandomSatisfiedChannel: (nil, group, nil)
// means "no channel available" and the caller emits its generic message.
func SelectResellerChannel(param *RetryParam, resellerOrgId int) (*model.Channel, string, error) {
	group := param.TokenGroup
	originGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyResellerOriginGroup)

	cfg := model.GetResellerRoutingCached(resellerOrgId)
	if cfg == nil || len(cfg.ChannelIdList) == 0 {
		// Config vanished between distributor and selection (admin cleared it
		// inside the cache TTL). Fail closed to the platform group the customer
		// was on, never to some other reseller's group.
		if originGroup == "" {
			return nil, group, nil
		}
		ch, err := model.GetRandomSatisfiedChannel(originGroup, param.ModelName, param.GetRetry(), param.RequestPath)
		return ch, originGroup, err
	}

	// Candidates: bound channels that are enabled, serve this model in the
	// reseller group, match the request path, and are under their daily budget.
	ids := model.FilterChannelIdsByRequestPathAndModel(cfg.ChannelIdList, param.RequestPath, param.ModelName)
	candidates := make([]resellerCandidate, 0, len(ids))
	for _, id := range ids {
		if !model.IsChannelEnabledForGroupModel(group, param.ModelName, id) {
			continue
		}
		ch, err := model.CacheGetChannel(id)
		if err != nil || ch == nil || ch.Status != common.ChannelStatusEnabled {
			continue
		}
		if model.IsChannelOverDailyTokenBudget(ch) {
			continue
		}
		priority, weight, matched := model.ResolveResellerRule(cfg.RuleList, param.ModelName, id)
		if !matched {
			priority, weight = ch.GetPriority(), uint(ch.GetWeight())
		}
		candidates = append(candidates, resellerCandidate{channel: ch, priority: priority, weight: weight})
	}

	// Tiers by priority, highest first; retry index walks down the tiers.
	tiers := groupByPriority(candidates)
	retry := param.GetRetry()
	if retry < len(tiers) {
		tier := tiers[retry]
		if !cfg.AffinityOff {
			if affinity, ok := common.GetContextKeyType[uint64](param.Ctx, constant.ContextKeyResellerAffinityHash); ok {
				return pickByRendezvous(tier, affinity), group, nil
			}
		}
		return pickWeightedRandom(tier), group, nil
	}

	// Bound channels exhausted (or none serve this model).
	if !cfg.Fallback || originGroup == "" {
		return nil, group, nil
	}
	ch, err := model.GetRandomSatisfiedChannel(originGroup, param.ModelName, retry-len(tiers), param.RequestPath)
	return ch, originGroup, err
}

func groupByPriority(candidates []resellerCandidate) [][]resellerCandidate {
	if len(candidates) == 0 {
		return nil
	}
	byPriority := map[int64][]resellerCandidate{}
	for _, c := range candidates {
		byPriority[c.priority] = append(byPriority[c.priority], c)
	}
	priorities := make([]int64, 0, len(byPriority))
	for p := range byPriority {
		priorities = append(priorities, p)
	}
	sort.Slice(priorities, func(i, j int) bool { return priorities[i] > priorities[j] })
	tiers := make([][]resellerCandidate, 0, len(priorities))
	for _, p := range priorities {
		tier := byPriority[p]
		// Deterministic order inside a tier so ties in the rendezvous score
		// (astronomically unlikely) still resolve identically everywhere.
		sort.Slice(tier, func(i, j int) bool { return tier[i].channel.Id < tier[j].channel.Id })
		tiers = append(tiers, tier)
	}
	return tiers
}

func pickByRendezvous(tier []resellerCandidate, affinity uint64) *model.Channel {
	var best *model.Channel
	bestScore := math.Inf(-1)
	for _, c := range tier {
		if s := rendezvousScore(affinity, c.channel.Id, c.weight); s > bestScore {
			bestScore = s
			best = c.channel
		}
	}
	return best
}

func pickWeightedRandom(tier []resellerCandidate) *model.Channel {
	total := 0
	for _, c := range tier {
		total += int(c.weight) + 10
	}
	r := rand.Intn(total)
	for _, c := range tier {
		r -= int(c.weight) + 10
		if r < 0 {
			return c.channel
		}
	}
	return tier[len(tier)-1].channel
}
