package cache

import (
	"os"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

// NewRedisClient creates a shared Redis client from environment variables.
// Checks REDIS_ADDR first, then REDIS_URL, then constructs from REDIS_HOST+REDIS_PORT.
// Returns nil if no Redis configuration is found (graceful degradation).
func NewRedisClient() *redis.Client {
	if addr := strings.TrimSpace(os.Getenv("REDIS_ADDR")); addr != "" {
		return redis.NewClient(&redis.Options{Addr: addr})
	}
	if u := strings.TrimSpace(os.Getenv("REDIS_URL")); u != "" {
		if opts, err := redis.ParseURL(u); err == nil {
			return redis.NewClient(opts)
		}
	}
	host := strings.TrimSpace(os.Getenv("REDIS_HOST"))
	port := strings.TrimSpace(os.Getenv("REDIS_PORT"))
	if host != "" {
		if port == "" {
			port = "6379"
		}
		return redis.NewClient(&redis.Options{Addr: host + ":" + port})
	}
	return nil
}
