package limiter

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// This file implements a daily (UTC calendar-day) counter that accumulates
// an arbitrary "amount" per key instead of a simple +1 per request. It is
// the primitive behind the daily token-quota limits: callers peek today's
// accumulated total before doing work, and add the actual amount consumed
// afterwards (see AddDailyTokens / PeekDailyTokens).
//
// The bucket for a key always resets at 00:00 UTC: the key embeds the
// current UTC date (YYYY-MM-DD), so once the date rolls over, a fresh
// (zero-valued) bucket is used automatically without any explicit reset
// logic needed.

// dailyIncrScript atomically increments the bucket and (re)sets its TTL so
// abandoned buckets are eventually cleaned up. The TTL is refreshed on every
// write, which is harmless because each UTC day has its own bucket key.
const dailyIncrScript = `
local n = redis.call('INCRBY', KEYS[1], ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
return n
`

// dailyBucketTTLSeconds is deliberately more than 24h so a bucket created
// just before midnight still has time to be read/cleaned up without a race.
const dailyBucketTTLSeconds = 26 * 60 * 60

func dailyBucketKey(key string) string {
	return key + ":" + time.Now().UTC().Format("2006-01-02")
}

func toInt64(v interface{}) (int64, error) {
	switch typed := v.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, redis.Nil
	}
}

// AddDailyTokens atomically adds amount to today's (UTC) counter for key and
// returns the counter's new total for the day. Falls back to an in-process
// counter when Redis is not enabled/available.
func AddDailyTokens(ctx context.Context, key string, amount int64) (int64, error) {
	if amount < 0 {
		amount = 0
	}
	bk := dailyBucketKey(key)
	if common.RedisEnabled && common.RDB != nil {
		v, err := common.RDB.Eval(ctx, dailyIncrScript, []string{bk}, amount, dailyBucketTTLSeconds).Result()
		if err != nil {
			return 0, err
		}
		return toInt64(v)
	}
	return memWindows.add(bk, amount), nil
}

// PeekDailyTokens returns today's (UTC) accumulated total for key without
// modifying it. A key with no recorded usage yet today reports 0.
func PeekDailyTokens(ctx context.Context, key string) (int64, error) {
	bk := dailyBucketKey(key)
	if common.RedisEnabled && common.RDB != nil {
		v, err := common.RDB.Get(ctx, bk).Result()
		if err != nil {
			if err == redis.Nil {
				return 0, nil
			}
			return 0, err
		}
		return strconv.ParseInt(v, 10, 64)
	}
	return memWindows.peek(bk), nil
}

// --- in-memory fallback (single-process deployments without Redis) ---

type memWindowEntry struct {
	value     int64
	expiresAt time.Time
}

type memTokenWindows struct {
	mutex sync.Mutex
	store map[string]*memWindowEntry
	once  sync.Once
}

var memWindows = &memTokenWindows{}

func (m *memTokenWindows) ensureInit() {
	m.once.Do(func() {
		m.store = make(map[string]*memWindowEntry)
		go m.sweep()
	})
}

func (m *memTokenWindows) sweep() {
	for {
		time.Sleep(time.Minute)
		now := time.Now()
		m.mutex.Lock()
		for k, e := range m.store {
			if now.After(e.expiresAt) {
				delete(m.store, k)
			}
		}
		m.mutex.Unlock()
	}
}

func (m *memTokenWindows) add(key string, amount int64) int64 {
	m.ensureInit()
	m.mutex.Lock()
	defer m.mutex.Unlock()
	e, ok := m.store[key]
	if !ok {
		e = &memWindowEntry{}
		m.store[key] = e
	}
	e.value += amount
	e.expiresAt = time.Now().Add(dailyBucketTTLSeconds * time.Second)
	return e.value
}

func (m *memTokenWindows) peek(key string) int64 {
	m.ensureInit()
	m.mutex.Lock()
	defer m.mutex.Unlock()
	e, ok := m.store[key]
	if !ok || time.Now().After(e.expiresAt) {
		return 0
	}
	return e.value
}
