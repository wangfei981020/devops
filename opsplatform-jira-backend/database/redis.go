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
		log.Println("REDIS_HOST 未设置，Redis 功能禁用")
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
		log.Printf("Redis 连接失败: %v，使用内存存储", err)
		RedisEnabled = false
		return nil
	}

	log.Printf("Redis 连接成功: %s:%s", host, port)
	RedisEnabled = true
	return nil
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

func RedisHSet(key string, values ...interface{}) error {
	if !RedisEnabled {
		return fmt.Errorf("redis not enabled")
	}
	return RDB.HSet(ctx, key, values...).Err()
}
