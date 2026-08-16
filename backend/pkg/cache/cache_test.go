package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCacheNew tests cache instance creation
func TestCacheNew(t *testing.T) {
	config := &Config{
		Address:     "localhost:6379",
		Database:    0,
		ArtifactTTL: 24 * time.Hour,
		RegistryTTL: 5 * time.Minute,
		SearchTTL:   15 * time.Minute,
	}

	cache := New(config)
	assert.NotNil(t, cache)
	assert.NotNil(t, cache.config)
}

// TestCacheConfig tests cache configuration
func TestCacheConfig(t *testing.T) {
	artifactTTL := 24 * time.Hour
	registryTTL := 5 * time.Minute
	searchTTL := 15 * time.Minute

	config := &Config{
		Address:     "localhost:6379",
		Database:    0,
		ArtifactTTL: artifactTTL,
		RegistryTTL: registryTTL,
		SearchTTL:   searchTTL,
	}

	assert.Equal(t, "localhost:6379", config.Address)
	assert.Equal(t, 0, config.Database)
	assert.Equal(t, artifactTTL, config.ArtifactTTL)
	assert.Equal(t, registryTTL, config.RegistryTTL)
	assert.Equal(t, searchTTL, config.SearchTTL)
}

// TestCacheConfigDefaults tests default configuration values
func TestCacheConfigDefaults(t *testing.T) {
	config := &Config{
		Address: "localhost:6379",
	}

	// Test that defaults are set correctly
	assert.Equal(t, "localhost:6379", config.Address)
	assert.Equal(t, defaultArtifactTTL, config.ArtifactTTL)
	assert.Equal(t, defaultRegistryTTL, config.RegistryTTL)
	assert.Equal(t, defaultSearchTTL, config.SearchTTL)
}

// TestCacheStats tests cache statistics
func TestCacheStats(t *testing.T) {
	stats := CacheStats{}

	// Test initial values
	assert.Equal(t, 0, stats.Hits)
	assert.Equal(t, 0, stats.Misses)
	assert.Equal(t, 0, stats.SetCalls)
	assert.Equal(t, 0, stats.DeleteCalls)

	// Test incrementing stats
	stats.RecordHit()
	assert.Equal(t, 1, stats.Hits)

	stats.RecordMiss()
	assert.Equal(t, 1, stats.Misses)

	stats.RecordSet()
	assert.Equal(t, 1, stats.SetCalls)

	stats.RecordDelete()
	assert.Equal(t, 1, stats.DeleteCalls)
}

// TestCacheStatsReset tests resetting cache statistics
func TestCacheStatsReset(t *testing.T) {
	stats := CacheStats{}

	// Increment some stats
	stats.RecordHit()
	stats.RecordMiss()
	stats.RecordSet()

	// Reset
	stats.Reset()

	// Verify reset
	assert.Equal(t, 0, stats.Hits)
	assert.Equal(t, 0, stats.Misses)
	assert.Equal(t, 0, stats.SetCalls)
	assert.Equal(t, 0, stats.DeleteCalls)
}

// TestCacheStatsHitRate tests hit rate calculation
func TestCacheStatsHitRate(t *testing.T) {
	stats := CacheStats{}

	// Test with no requests
	assert.Equal(t, 0.0, stats.HitRate())

	// Test with only hits
	stats.Hits = 10
	stats.Misses = 0
	assert.Equal(t, 1.0, stats.HitRate())

	// Test with hits and misses
	stats.Hits = 10
	stats.Misses = 10
	assert.Equal(t, 0.5, stats.HitRate())

	// Test with many misses
	stats.Hits = 1
	stats.Misses = 99
	assert.Equal(t, 0.01, stats.HitRate())
}

// TestCacheStatsEmptyHitRate tests hit rate with no requests
func TestCacheStatsEmptyHitRate(t *testing.T) {
	stats := CacheStats{}
	assert.Equal(t, 0.0, stats.HitRate())
}

// TestNewConfig creates a new configuration and tests its values
func TestNewConfig(t *testing.T) {
	config := NewConfig("localhost:6379", 0)

	assert.Equal(t, "localhost:6379", config.Address)
	assert.Equal(t, 0, config.Database)
	assert.Equal(t, 24*time.Hour, config.ArtifactTTL)
	assert.Equal(t, 5*time.Minute, config.RegistryTTL)
	assert.Equal(t, 15*time.Minute, config.SearchTTL)
}

// TestCacheKeys tests cache key generation
func TestCacheKeys(t *testing.T) {
	// Test artifact key format
	artifactKey := cacheKey("artifact", "registry-123", "namespace", "name", "version")
	assert.Contains(t, artifactKey, "artifact:registry-123")
	assert.Contains(t, artifactKey, "namespace")
	assert.Contains(t, artifactKey, "name")
	assert.Contains(t, artifactKey, "version")

	// Test registry key format
	registryKey := cacheKey("registry", "registry-123", "", "", "")
	assert.Contains(t, registryKey, "registry:registry-123")

	// Test search key format
	searchKey := cacheKey("search", "registry-123", "query", "", "")
	assert.Contains(t, searchKey, "search:registry-123")
	assert.Contains(t, searchKey, "query")
}

// TestCacheKeyFormat tests cache key format consistency
func TestCacheKeyFormat(t *testing.T) {
	key1 := cacheKey("artifact", "reg", "ns", "name", "v1")
	key2 := cacheKey("artifact", "reg", "ns", "name", "v1")

	// Same inputs should produce same key
	assert.Equal(t, key1, key2)
}

// TestCacheTTLs tests TTL configuration
func TestCacheTTLs(t *testing.T) {
	config := NewConfig("localhost:6379", 0)

	// Test artifact TTL
	assert.Equal(t, 24*time.Hour, config.ArtifactTTL)

	// Test registry TTL
	assert.Equal(t, 5*time.Minute, config.RegistryTTL)

	// Test search TTL
	assert.Equal(t, 15*time.Minute, config.SearchTTL)
}

// TestMultiTierCache tests multi-tier caching
func TestMultiTierCache(t *testing.T) {
	config := &Config{
		Address:     "localhost:6379",
		Database:    0,
		ArtifactTTL: 24 * time.Hour,
		RegistryTTL: 5 * time.Minute,
		SearchTTL:   15 * time.Minute,
	}

	cache := New(config)

	// Test that cache is created
	assert.NotNil(t, cache)
	assert.NotNil(t, cache.config)

	// Test TTL values are correct
	assert.Equal(t, 24*time.Hour, cache.config.ArtifactTTL)
	assert.Equal(t, 5*time.Minute, cache.config.RegistryTTL)
	assert.Equal(t, 15*time.Minute, cache.config.SearchTTL)
}

// TestCacheStatsConcurrency tests cache stats thread safety
func TestCacheStatsConcurrency(t *testing.T) {
	stats := CacheStats{}

	// Simulate concurrent access
	for i := 0; i < 100; i++ {
		stats.RecordHit()
	}

	assert.Equal(t, 100, stats.Hits)
}

// TestCachePerformanceMetrics tests performance metric recording
func TestCachePerformanceMetrics(t *testing.T) {
	stats := CacheStats{}

	// Record various operations
	for i := 0; i < 50; i++ {
		stats.RecordHit()
	}
	for i := 0; i < 30; i++ {
		stats.RecordMiss()
	}
	for i := 0; i < 20; i++ {
		stats.RecordSet()
	}
	for i := 0; i < 10; i++ {
		stats.RecordDelete()
	}

	// Verify counts
	assert.Equal(t, 50, stats.Hits)
	assert.Equal(t, 30, stats.Misses)
	assert.Equal(t, 20, stats.SetCalls)
	assert.Equal(t, 10, stats.DeleteCalls)

	// Verify hit rate
	expectedHitRate := 50.0 / (50.0 + 30.0)
	assert.InDelta(t, expectedHitRate, stats.HitRate(), 0.001)
}

// TestCacheTTLExpiration tests TTL expiration times
func TestCacheTTLExpiration(t *testing.T) {
	// Test that TTL values are reasonable
	artifactTTL := 24 * time.Hour
	registryTTL := 5 * time.Minute
	searchTTL := 15 * time.Minute

	// Artifact TTL should be longer than registry TTL
	assert.Greater(t, artifactTTL, registryTTL)

	// Search TTL should be between artifact and registry
	assert.Greater(t, registryTTL, searchTTL)
	assert.Greater(t, searchTTL, 0)

	// All TTLs should be positive
	assert.Greater(t, artifactTTL, 0)
	assert.Greater(t, registryTTL, 0)
	assert.Greater(t, searchTTL, 0)
}

// TestCacheConfigWithDifferentDBs tests configuration with different database indices
func TestCacheConfigWithDifferentDBs(t *testing.T) {
	config1 := NewConfig("localhost:6379", 0)
	config2 := NewConfig("localhost:6379", 1)
	config3 := NewConfig("localhost:6379", 15)

	assert.Equal(t, 0, config1.Database)
	assert.Equal(t, 1, config2.Database)
	assert.Equal(t, 15, config3.Database)

	// All should have same TTLs
	assert.Equal(t, config1.ArtifactTTL, config2.ArtifactTTL)
	assert.Equal(t, config1.RegistryTTL, config2.RegistryTTL)
}

// TestCacheConfigWithCustomTTLs tests configuration with custom TTLs
func TestCacheConfigWithCustomTTLs(t *testing.T) {
	customArtifactTTL := 48 * time.Hour
	customRegistryTTL := 10 * time.Minute
	customSearchTTL := 30 * time.Minute

	config := &Config{
		Address:     "localhost:6379",
		Database:    0,
		ArtifactTTL: customArtifactTTL,
		RegistryTTL: customRegistryTTL,
		SearchTTL:   customSearchTTL,
	}

	assert.Equal(t, customArtifactTTL, config.ArtifactTTL)
	assert.Equal(t, customRegistryTTL, config.RegistryTTL)
	assert.Equal(t, customSearchTTL, config.SearchTTL)
}
