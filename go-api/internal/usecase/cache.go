package usecase

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

// Cache abstracts the Redis operations used by the use case to make testing easier.
type Cache interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
}

// RedisCache is a concrete implementation backed by go-redis.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache constructs a new Redis-backed cache adapter.
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// Set writes a value to Redis.
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return c.client.Set(ctx, key, value, expiration).Err()
}

// Get retrieves a cached value from Redis.
func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}
