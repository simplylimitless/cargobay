package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewUnsupportedType tests that New rejects a non-redis cache type
// before attempting any network connection.
func TestNewUnsupportedType(t *testing.T) {
	_, err := New("memcached", "localhost:11211")
	assert.Error(t, err)
}

// TestNewInvalidURL tests that New rejects a malformed redis URL before
// attempting any network connection.
func TestNewInvalidURL(t *testing.T) {
	_, err := New("redis", "not-a-valid-url")
	assert.Error(t, err)
}

// TestNewConnects exercises the real connection path against a live Redis.
// Skipped when no Redis is reachable (e.g. running outside docker-compose).
func TestNewConnects(t *testing.T) {
	c, err := New("redis", "redis://localhost:6379/0")
	if err != nil {
		t.Skipf("no redis available at localhost:6379: %v", err)
	}
	defer c.Close()

	assert.NotNil(t, c)
	require.NoError(t, c.Ping())
}

// TestJoinParts tests the ":"-joining used to build cache keys.
func TestJoinParts(t *testing.T) {
	assert.Equal(t, "", joinParts())
	assert.Equal(t, "a", joinParts("a"))
	assert.Equal(t, "a:b:c", joinParts("a", "b", "c"))
}

// TestGetKey tests cache key generation via a white-box (unconnected) Cache.
func TestGetKey(t *testing.T) {
	c := &Cache{}

	artifactKey := c.getKey("artifact", "registry-123", "namespace", "name", "version")
	assert.Equal(t, "artifact:registry-123:namespace:name:version", artifactKey)

	registryKey := c.getKey("registry", "registry-123")
	assert.Equal(t, "registry:registry-123", registryKey)
}

// TestCacheStatsZero tests that a fresh Cache reports a zero hit rate.
func TestCacheStatsZero(t *testing.T) {
	c := &Cache{}
	assert.Equal(t, 0.0, c.HitRate())
}

// TestCacheStatsHitRate tests hit rate calculation via directly-populated
// stats (same-package white-box access, no live Redis required).
func TestCacheStatsHitRate(t *testing.T) {
	c := &Cache{stats: CacheStats{Hits: 10, Misses: 0}}
	assert.Equal(t, 1.0, c.HitRate())

	c = &Cache{stats: CacheStats{Hits: 10, Misses: 10}}
	assert.Equal(t, 0.5, c.HitRate())

	c = &Cache{stats: CacheStats{Hits: 1, Misses: 99}}
	assert.Equal(t, 0.01, c.HitRate())
}

// TestCacheStatsFields tests that CacheStats fields are independently
// addressable and start at zero.
func TestCacheStatsFields(t *testing.T) {
	stats := CacheStats{}
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, int64(0), stats.Errors)
	assert.Equal(t, int64(0), stats.BytesStored)
	assert.Equal(t, int64(0), stats.Entries)
}

// TestCacheTTLDefaults tests the default TTL tiers set by New.
func TestCacheTTLDefaults(t *testing.T) {
	c, err := New("redis", "redis://localhost:6379/0")
	if err != nil {
		t.Skipf("no redis available at localhost:6379: %v", err)
	}
	defer c.Close()

	assert.Equal(t, 24*time.Hour, c.artifactTTL)
	assert.Equal(t, 5*time.Minute, c.registryTTL)
	assert.Equal(t, 15*time.Minute, c.searchTTL)
	assert.Equal(t, time.Hour, c.ttl)
}
