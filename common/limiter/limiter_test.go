package limiter

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useLimiterMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	previousInstance := instance

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Ping(context.Background()).Err())

	instance = nil
	once = sync.Once{}

	t.Cleanup(func() {
		_ = redisClient.Close()
		instance = previousInstance
		once = sync.Once{}
		if previousInstance != nil {
			once.Do(func() {})
		}
	})

	return redisServer, redisClient
}

func TestAllowFallsBackToEvalAfterScriptFlush(t *testing.T) {
	ctx := context.Background()
	_, redisClient := useLimiterMiniRedis(t)

	redisLimiter := New(ctx, redisClient)

	allowed, err := redisLimiter.Allow(
		ctx,
		"rateLimit:test-before-flush",
		WithCapacity(60),
		WithRate(60),
		WithRequested(60),
	)
	require.NoError(t, err)
	assert.True(t, allowed)

	exists, err := redisClient.ScriptExists(ctx, redisLimiter.limitScriptSHA).Result()
	require.NoError(t, err)
	require.Len(t, exists, 1)
	assert.True(t, exists[0])

	require.NoError(t, redisClient.ScriptFlush(ctx).Err())

	exists, err = redisClient.ScriptExists(ctx, redisLimiter.limitScriptSHA).Result()
	require.NoError(t, err)
	require.Len(t, exists, 1)
	assert.False(t, exists[0])

	allowed, err = redisLimiter.Allow(
		ctx,
		"rateLimit:test-after-flush",
		WithCapacity(60),
		WithRate(60),
		WithRequested(60),
	)
	require.NoError(t, err)
	assert.True(t, allowed)

	exists, err = redisClient.ScriptExists(ctx, redisLimiter.limitScriptSHA).Result()
	require.NoError(t, err)
	require.Len(t, exists, 1)
	assert.True(t, exists[0])
}

// The in-memory daily counter saturates: two absurd usages reported by an
// upstream must not wrap it below zero, which would lift the limit.
func TestMemTokenWindowSaturatesInsteadOfWrapping(t *testing.T) {
	windows := &memTokenWindows{}
	const huge = int64(5e18)
	assert.Equal(t, huge, windows.add("day:user:1", huge))
	assert.Equal(t, int64(math.MaxInt64), windows.add("day:user:1", huge))
	assert.Equal(t, int64(math.MaxInt64), windows.add("day:user:1", 1))
	assert.Equal(t, int64(math.MaxInt64), windows.peek("day:user:1"))
}
