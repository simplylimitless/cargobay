package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache provides Redis-based caching
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

// New creates a new Cache instance
func New(cacheType, url string) (*Cache, error) {
	if cacheType != "redis" {
		return nil, fmt.Errorf("unsupported cache type: %s", cacheType)
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}
	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Cache{client: client, ttl: time.Hour}, nil
}

// Close closes the Redis connection
func (c *Cache) Close() error {
	return c.client.Close()
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
	ctx := context.Background()
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		return err
	}
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

// SetWithTTL stores a value with custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	ctx := context.Background()
	return c.client.Set(ctx, key, data, ttl).Err()
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
			return err
		}

		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		if cursor == 0 {
			break
		}
	}

	return nil
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
func (c *Cache) Stats() (map[string]interface{}, error) {
	ctx := context.Background()
	info, err := c.client.Info(ctx, "memory").Result()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"info": info}, nil
}
