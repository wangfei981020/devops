package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"opsplatform-alert-backend/config"
)

var RDB *redis.Client

func InitRedis(cfg *config.Config) error {
	RDB = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RDB.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}

	log.Println("Redis connected successfully")
	return nil
}

// CheckDedup checks if a dedup key exists, returns true if already seen
func CheckDedup(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	set, err := RDB.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, err
	}
	// SetNX returns true if key was set (i.e., NOT a duplicate)
	return !set, nil
}
