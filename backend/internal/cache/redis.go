package cache

import (
	"os"
	"strconv"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// NewRedisClient creates a shared Redis client from environment variables.
// Checks REDIS_ADDR first, then REDIS_URL, then constructs from REDIS_HOST+REDIS_PORT.
// Returns nil if no Redis configuration is found (graceful degradation).
func NewRedisClient() *redis.Client {
	buildClient := func(opts *redis.Options) *redis.Client {
		if opts == nil {
			return nil
		}
		if opts.DialTimeout == 0 {
			opts.DialTimeout = redisDurationFromEnv("REDIS_DIAL_TIMEOUT_MS", 800*time.Millisecond)
		}
		if opts.ReadTimeout == 0 {
			opts.ReadTimeout = redisDurationFromEnv("REDIS_READ_TIMEOUT_MS", 1500*time.Millisecond)
		}
		if opts.WriteTimeout == 0 {
			opts.WriteTimeout = redisDurationFromEnv("REDIS_WRITE_TIMEOUT_MS", 1500*time.Millisecond)
		}
		if opts.PoolTimeout == 0 {
			opts.PoolTimeout = redisDurationFromEnv("REDIS_POOL_TIMEOUT_MS", 1500*time.Millisecond)
		}
		if opts.MaxRetries == 0 {
			opts.MaxRetries = redisIntFromEnv("REDIS_MAX_RETRIES", 1)
		}
		return redis.NewClient(opts)
	}
	if addr := strings.TrimSpace(os.Getenv("REDIS_ADDR")); addr != "" {
		return buildClient(&redis.Options{Addr: addr})
	}
	if u := strings.TrimSpace(os.Getenv("REDIS_URL")); u != "" {
		if opts, err := redis.ParseURL(u); err == nil {
			return buildClient(opts)
		}
	}
	host := strings.TrimSpace(os.Getenv("REDIS_HOST"))
	port := strings.TrimSpace(os.Getenv("REDIS_PORT"))
	if host != "" {
		if port == "" {
			port = "6379"
		}
		return buildClient(&redis.Options{Addr: host + ":" + port})
	}
	return nil
}

func redisDurationFromEnv(key string, fallback time.Duration) time.Duration {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return time.Duration(value) * time.Millisecond
		}
	}
	return fallback
}

func redisIntFromEnv(key string, fallback int) int {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 0 {
			return value
		}
	}
	return fallback
}
