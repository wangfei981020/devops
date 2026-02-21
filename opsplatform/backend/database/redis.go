package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	RDB          *redis.Client
	RedisEnabled bool
	ctx          = context.Background()
)

func InitRedis() error {
	host := getEnv("REDIS_HOST", "")
	if host == "" {
		log.Println("⚠️  REDIS_HOST 未设置，Redis 功能禁用，使用内存存储")
		RedisEnabled = false
		return nil
	}

	port := getEnv("REDIS_PORT", "6379")
	password := os.Getenv("REDIS_PASSWORD")
	dbStr := getEnv("REDIS_DB", "0")
	db, _ := strconv.Atoi(dbStr)

	RDB = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", host, port),
		Password:     password,
		DB:           db,
		PoolSize:     10,
		MinIdleConns: 3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	_, err := RDB.Ping(ctx).Result()
	if err != nil {
		log.Printf("⚠️  Redis 连接失败: %v，使用内存存储", err)
		RedisEnabled = false
		return nil
	}

	log.Printf("✅ Redis 连接成功: %s:%s", host, port)
	RedisEnabled = true
	return nil
}

func GetRedisContext() context.Context {
	return ctx
}

func RedisSet(key string, value interface{}, expiration time.Duration) error {
	if !RedisEnabled {
		return fmt.Errorf("redis not enabled")
	}
	return RDB.Set(ctx, key, value, expiration).Err()
}

func RedisGet(key string) (string, error) {
	if !RedisEnabled {
		return "", fmt.Errorf("redis not enabled")
	}
	return RDB.Get(ctx, key).Result()
}

func RedisDelete(key string) error {
	if !RedisEnabled {
		return fmt.Errorf("redis not enabled")
	}
	return RDB.Del(ctx, key).Err()
}

func RedisIncr(key string) (int64, error) {
	if !RedisEnabled {
		return 0, fmt.Errorf("redis not enabled")
	}
	return RDB.Incr(ctx, key).Result()
}

func RedisExpire(key string, expiration time.Duration) error {
	if !RedisEnabled {
		return fmt.Errorf("redis not enabled")
	}
	return RDB.Expire(ctx, key, expiration).Err()
}

func RedisExists(key string) (bool, error) {
	if !RedisEnabled {
		return false, fmt.Errorf("redis not enabled")
	}
	n, err := RDB.Exists(ctx, key).Result()
	return n > 0, err
}

func RedisTTL(key string) (time.Duration, error) {
	if !RedisEnabled {
		return 0, fmt.Errorf("redis not enabled")
	}
	return RDB.TTL(ctx, key).Result()
}

func RedisSetNX(key string, value interface{}, expiration time.Duration) (bool, error) {
	if !RedisEnabled {
		return false, fmt.Errorf("redis not enabled")
	}
	return RDB.SetNX(ctx, key, value, expiration).Result()
}

func RedisHSet(key string, values ...interface{}) error {
	if !RedisEnabled {
		return fmt.Errorf("redis not enabled")
	}
	return RDB.HSet(ctx, key, values...).Err()
}

func RedisHGetAll(key string) (map[string]string, error) {
	if !RedisEnabled {
		return nil, fmt.Errorf("redis not enabled")
	}
	return RDB.HGetAll(ctx, key).Result()
}

func RedisHGet(key, field string) (string, error) {
	if !RedisEnabled {
		return "", fmt.Errorf("redis not enabled")
	}
	return RDB.HGet(ctx, key, field).Result()
}

func RedisHIncrBy(key, field string, incr int64) (int64, error) {
	if !RedisEnabled {
		return 0, fmt.Errorf("redis not enabled")
	}
	return RDB.HIncrBy(ctx, key, field, incr).Result()
}
