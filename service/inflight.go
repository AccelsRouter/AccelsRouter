package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Live view of relay requests in progress (fork). Each node counts its own
// requests in memory and, with Redis, publishes a snapshot every
// inflightPublishInterval so the admin page can add the nodes up. Nothing is
// written per request; the registry is a mutex-guarded map.

const (
	inflightPublishInterval = 2 * time.Second
	inflightSnapshotTTL     = 10 * time.Second
	inflightRedisPrefix     = "inflight:node:"
	// A relay request that has been "in flight" this long is a leaked entry
	// (the handler never returned); it is dropped from every view.
	inflightStaleAfter = 30 * time.Minute
	inflightPeakWindow = time.Hour
)

// InflightEntry is one relay request currently being served.
type InflightEntry struct {
	RequestId string `json:"request_id"`
	Node      string `json:"node"`
	UserId    int    `json:"user_id"`
	Username  string `json:"username"`
	TokenId   int    `json:"token_id"`
	TokenName string `json:"token_name"`
	OrgId     int    `json:"org_id,omitempty"`
	Group     string `json:"group"`
	Model     string `json:"model"`
	ChannelId int    `json:"channel_id"`
	Path      string `json:"path"`
	StartedAt int64  `json:"started_at"` // unix milliseconds
}

// InflightSnapshot is one node's view: its live entries plus the highest
// concurrency it saw per user and overall during the last hour.
type InflightSnapshot struct {
	Node      string          `json:"node"`
	TakenAt   int64           `json:"taken_at"`
	Entries   []InflightEntry `json:"entries"`
	PeakTotal int             `json:"peak_total"`
	PeakUsers map[string]int  `json:"peak_users"` // user id → peak (string keys for JSON)
}

// inflightMinute holds the peaks observed during one wall-clock minute.
type inflightMinute struct {
	total int
	users map[int]int
}

type inflightRegistry struct {
	mu      sync.Mutex
	entries map[string]*InflightEntry
	byUser  map[int]int
	minutes map[int64]*inflightMinute
	clock   func() time.Time
}

var inflight = &inflightRegistry{
	entries: map[string]*InflightEntry{},
	byUser:  map[int]int{},
	minutes: map[int64]*inflightMinute{},
	clock:   time.Now,
}

// InflightBegin registers the request the distributor is about to hand to the
// relay handler and returns the function that removes it again. Called once
// per relay request on the hot path: one lock, no I/O.
func InflightBegin(c *gin.Context) func() {
	entry := &InflightEntry{
		RequestId: c.GetString(common.RequestIdKey),
		Node:      common.NodeName,
		UserId:    c.GetInt("id"),
		Username:  c.GetString("username"),
		TokenId:   c.GetInt("token_id"),
		TokenName: c.GetString("token_name"),
		Group:     common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		Model:     c.GetString("original_model"),
		ChannelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		Path:      c.Request.URL.Path,
	}
	if payer, err := model.GetOrgPayerInfo(entry.UserId); err == nil && payer != nil {
		entry.OrgId = payer.OrgId
	}
	return inflight.begin(entry)
}

func (r *inflightRegistry) begin(entry *InflightEntry) func() {
	now := r.clock()
	entry.StartedAt = now.UnixMilli()
	key := entry.RequestId
	if key == "" {
		key = "anon-" + now.Format(time.RFC3339Nano)
	}
	r.mu.Lock()
	r.entries[key] = entry
	r.byUser[entry.UserId]++
	minute := r.minuteLocked(now)
	minute.total = max(minute.total, len(r.entries))
	minute.users[entry.UserId] = max(minute.users[entry.UserId], r.byUser[entry.UserId])
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if current, ok := r.entries[key]; ok && current == entry {
				delete(r.entries, key)
				if r.byUser[entry.UserId] <= 1 {
					delete(r.byUser, entry.UserId)
				} else {
					r.byUser[entry.UserId]--
				}
			}
			r.mu.Unlock()
		})
	}
}

// minuteLocked returns the bucket for now, pruning buckets outside the peak
// window. Caller holds r.mu.
func (r *inflightRegistry) minuteLocked(now time.Time) *inflightMinute {
	key := now.Unix() / 60
	for stamp := range r.minutes {
		if stamp < key-int64(inflightPeakWindow/time.Minute) {
			delete(r.minutes, stamp)
		}
	}
	bucket, ok := r.minutes[key]
	if !ok {
		bucket = &inflightMinute{users: map[int]int{}}
		r.minutes[key] = bucket
	}
	return bucket
}

// snapshot copies the live state, dropping leaked entries.
func (r *inflightRegistry) snapshot() InflightSnapshot {
	now := r.clock()
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := InflightSnapshot{
		Node:      common.NodeName,
		TakenAt:   now.UnixMilli(),
		Entries:   make([]InflightEntry, 0, len(r.entries)),
		PeakUsers: map[string]int{},
	}
	staleBefore := now.Add(-inflightStaleAfter).UnixMilli()
	for key, entry := range r.entries {
		if entry.StartedAt < staleBefore {
			common.SysError(common.LogText("inflight: dropping request %s started %d ms ago that never finished", entry.RequestId, now.UnixMilli()-entry.StartedAt))
			delete(r.entries, key)
			if r.byUser[entry.UserId] <= 1 {
				delete(r.byUser, entry.UserId)
			} else {
				r.byUser[entry.UserId]--
			}
			continue
		}
		snap.Entries = append(snap.Entries, *entry)
	}
	sort.Slice(snap.Entries, func(i, j int) bool { return snap.Entries[i].StartedAt < snap.Entries[j].StartedAt })
	oldest := now.Unix()/60 - int64(inflightPeakWindow/time.Minute)
	peakUsers := map[int]int{}
	for stamp, bucket := range r.minutes {
		if stamp < oldest {
			continue
		}
		snap.PeakTotal = max(snap.PeakTotal, bucket.total)
		for userId, peak := range bucket.users {
			peakUsers[userId] = max(peakUsers[userId], peak)
		}
	}
	for userId, peak := range peakUsers {
		snap.PeakUsers[common.Interface2String(userId)] = peak
	}
	return snap
}

// InflightSnapshots returns this node's snapshot followed by every other
// node's last published one (Redis only; a single node sees just itself).
func InflightSnapshots(ctx context.Context) []InflightSnapshot {
	snapshots := []InflightSnapshot{inflight.snapshot()}
	if !common.RedisEnabled || common.RDB == nil {
		return snapshots
	}
	iter := common.RDB.Scan(ctx, 0, inflightRedisPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		raw, err := common.RDB.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var snap InflightSnapshot
		if err := common.UnmarshalJsonStr(raw, &snap); err != nil || snap.Node == common.NodeName {
			continue
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots
}

// StartInflightPublisher keeps this node's snapshot in Redis so the admin
// view can add up every node; without Redis each node only knows itself.
func StartInflightPublisher() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(inflightPublishInterval)
		defer ticker.Stop()
		for range ticker.C {
			encoded, err := common.Marshal(inflight.snapshot())
			if err != nil {
				continue
			}
			if err := common.RedisSet(inflightRedisPrefix+common.NodeName, string(encoded), inflightSnapshotTTL); err != nil {
				common.SysError("inflight: publish snapshot: " + err.Error())
			}
		}
	}()
}
