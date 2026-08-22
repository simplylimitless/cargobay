package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache provides Redis-based caching with multi-tier support
type Cache struct {
	client         *redis.Client
	ttl            time.Duration
	artifactTTL    time.Duration
	registryTTL    time.Duration
	searchTTL      time.Duration
	mu             sync.RWMutex
	stats          CacheStats
	statsLock      sync.Mutex
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	Hits          int64
	Misses        int64
	Errors        int64
	BytesStored   int64
	Entries       int64
}

// New creates a new Cache instance with configurable TTLs
func New(cacheType, url string) (*Cache, error) {
	if cacheType != "redis" {
		return nil, fmt.Errorf("unsupported cache type: %s", cacheType)
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}

	// Configure Redis for high performance
	opts.MaxRetries = 3
	opts.MinRetryBackoff = 8 * time.Millisecond
	opts.MaxRetryBackoff = 512 * time.Millisecond

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Cache{
		client:      client,
		ttl:         time.Hour,                    // Default TTL
		artifactTTL: 24 * time.Hour,              // Artifacts cached longer
		registryTTL: 5 * time.Minute,             // Registry metadata cached briefly
		searchTTL:   15 * time.Minute,            // Search results cached
	}, nil
}

// Close closes the Redis connection
func (c *Cache) Close() error {
	return c.client.Close()
}

// Ping checks Redis connectivity
func (c *Cache) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.client.Ping(ctx).Err()
}

// getKey generates a cache key
func (c *Cache) getKey(prefix string, parts ...string) string {
	return fmt.Sprintf("%s:%s", prefix, joinParts(parts...))
}

func joinParts(parts ...string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ":"
		}
		result += p
	}
	return result
}

// Get retrieves a value from cache
func (c *Cache) Get(key string, value interface{}) error {
	c.statsLock.Lock()
	c.stats.Misses++
	c.statsLock.Unlock()

	ctx := context.Background()
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		c.statsLock.Lock()
		c.stats.Errors++
		c.statsLock.Unlock()
		return err
	}

	c.statsLock.Lock()
	c.stats.Hits++
	c.stats.BytesStored += int64(len(data))
	c.statsLock.Unlock()

	return json.Unmarshal(data, value)
}

// GetWithTTL retrieves a value with custom TTL
func (c *Cache) GetWithTTL(key string, value interface{}, ttl time.Duration) error {
	if err := c.Get(key, value); err != nil {
		return err
	}
	// Key already exists in cache
	return nil
}

// Set stores a value in cache
func (c *Cache) Set(key string, value interface{}) error {
	return c.SetWithTTL(key, value, c.ttl)
}

// SetArtifact stores an artifact in cache with longer TTL
func (c *Cache) SetArtifact(key string, value interface{}) error {
	return c.SetWithTTL(key, value, c.artifactTTL)
}

// SetRegistry stores registry metadata with short TTL
func (c *Cache) SetRegistry(key string, value interface{}) error {
	return c.SetWithTTL(key, value, c.registryTTL)
}

// SetSearch stores search results with medium TTL
func (c *Cache) SetSearch(key string, value interface{}) error {
	return c.SetWithTTL(key, value, c.searchTTL)
}

// SetWithTTL stores a value with custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	ctx := context.Background()
	err = c.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		c.statsLock.Lock()
		c.stats.Errors++
		c.statsLock.Unlock()
		return err
	}

	c.statsLock.Lock()
	c.stats.BytesStored += int64(len(data))
	c.stats.Entries++
	c.statsLock.Unlock()
	return nil
}

// Delete removes a value from cache
func (c *Cache) Delete(key string) error {
	ctx := context.Background()
	return c.client.Del(ctx, key).Err()
}

// InvalidatePattern deletes all keys matching a pattern
func (c *Cache) InvalidatePattern(pattern string) error {
	ctx := context.Background()
	cursor := uint64(0)
	var keys []string

	for {
		var err error
		keys, cursor, err = c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			c.statsLock.Lock()
			c.stats.Errors++
			c.statsLock.Unlock()
			return err
		}

		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				c.statsLock.Lock()
				c.stats.Errors++
				c.statsLock.Unlock()
				return err
			}
		}

		if cursor == 0 {
			break
		}
	}

	return nil
}

// InvalidateArtifact invalidates cache for a specific artifact
func (c *Cache) InvalidateArtifact(registryID, namespace, artifactName, version string) error {
	// Invalidate specific artifact cache
	pattern := fmt.Sprintf("artifact:%s:%s:%s:%s", registryID, namespace, artifactName, version)
	if err := c.InvalidatePattern(pattern); err != nil {
		return err
	}

	// Invalidate artifact list cache
	pattern = fmt.Sprintf("artifact_list:%s:%s:%s", registryID, namespace, artifactName)
	return c.InvalidatePattern(pattern)
}

// InvalidateRegistry invalidates cache for a registry
func (c *Cache) InvalidateRegistry(registryID string) error {
	pattern := fmt.Sprintf("registry:%s:*", registryID)
	return c.InvalidatePattern(pattern)
}

// Exists checks if a key exists
func (c *Cache) Exists(key string) (bool, error) {
	ctx := context.Background()
	result, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

// Increment increments a counter
func (c *Cache) Increment(key string) (int64, error) {
	ctx := context.Background()
	return c.client.Incr(ctx, key).Result()
}

// IncrementWithTTL increments a counter with TTL
func (c *Cache) IncrementWithTTL(key string, ttl time.Duration) (int64, error) {
	ctx := context.Background()
	val, err := c.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	c.client.Expire(ctx, key, ttl)
	return val, nil
}

// Stats returns cache statistics
func (c *Cache) Stats() (CacheStats, error) {
	ctx := context.Background()
	if _, err := c.client.Info(ctx, "memory").Result(); err != nil {
		return CacheStats{}, err
	}

	c.statsLock.Lock()
	defer c.statsLock.Unlock()

	stats := c.stats
	return stats, nil
}

// HitRate returns the cache hit rate
func (c *Cache) HitRate() float64 {
	c.statsLock.Lock()
	defer c.statsLock.Unlock()

	total := c.stats.Hits + c.stats.Misses
	if total == 0 {
		return 0
	}
	return float64(c.stats.Hits) / float64(total)
}

// MultiGet retrieves multiple keys concurrently
func (c *Cache) MultiGet(keys []string) (map[string][]byte, error) {
	ctx := context.Background()
	results := make(map[string][]byte)

	for _, key := range keys {
		data, err := c.client.Get(ctx, key).Bytes()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		if err == redis.Nil {
			continue
		}
		results[key] = data
	}

	return results, nil
}

// MultiSet stores multiple keys concurrently
func (c *Cache) MultiSet(pairs map[string]interface{}, ttl time.Duration) error {
	ctx := context.Background()
	pipe := c.client.Pipeline()

	for key, value := range pairs {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		pipe.Set(ctx, key, data, ttl)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// InvalidateNamespace invalidates all cache entries for a namespace
func (c *Cache) InvalidateNamespace(registryID, namespace string) error {
	pattern := fmt.Sprintf("*:%s:%s:*", registryID, namespace)
	return c.InvalidatePattern(pattern)
}
