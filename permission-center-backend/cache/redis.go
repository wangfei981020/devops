package cache

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"permission-center-backend/config"
)

var RDB *redis.Client
var ctx = context.Background()

const (
	sessionPrefix  = "pc:sess:"
	settingPrefix  = "pc:setting:"
	sessionTTL     = 8 * time.Hour
	settingTTL     = 5 * time.Minute
)

func InitRedis() {
	db, _ := strconv.Atoi(config.RedisDB)
	RDB = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", config.RedisHost, config.RedisPort),
		Password: config.RedisPassword,
		DB:       db,
	})

	if err := RDB.Ping(ctx).Err(); err != nil {
		log.Printf("[Redis] Warning: failed to connect: %v (will use MySQL only)", err)
		RDB = nil
		return
	}
	log.Println("[Redis] Connected")
}

// SetSession caches session in Redis
func SetSession(tokenHash, userID, username string, ttl time.Duration) {
	if RDB == nil {
		return
	}
	val := userID + "|" + username
	if err := RDB.Set(ctx, sessionPrefix+tokenHash, val, ttl).Err(); err != nil {
		log.Printf("[Redis] SetSession error: %v", err)
	}
}

// GetSession retrieves session from Redis cache
// Returns userID, username, found
func GetSession(tokenHash string) (string, string, bool) {
	if RDB == nil {
		return "", "", false
	}
	val, err := RDB.Get(ctx, sessionPrefix+tokenHash).Result()
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(val, "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// DeleteSession removes session from Redis
func DeleteSession(tokenHash string) {
	if RDB == nil {
		return
	}
	RDB.Del(ctx, sessionPrefix+tokenHash)
}

// DeleteUserSessions removes all sessions for a user using SCAN
func DeleteUserSessions(userID string) {
	if RDB == nil {
		return
	}
	var cursor uint64
	for {
		keys, nextCursor, err := RDB.Scan(ctx, cursor, sessionPrefix+"*", 100).Result()
		if err != nil {
			break
		}
		for _, key := range keys {
			val, err := RDB.Get(ctx, key).Result()
			if err == nil && strings.HasPrefix(val, userID+"|") {
				RDB.Del(ctx, key)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// CacheLocalLoginEnabled caches the setting in Redis
func CacheLocalLoginEnabled(enabled bool) {
	if RDB == nil {
		return
	}
	val := "false"
	if enabled {
		val = "true"
	}
	RDB.Set(ctx, settingPrefix+"local_login_enabled", val, settingTTL)
}

// GetLocalLoginEnabled gets cached setting
// Returns value, found
func GetLocalLoginEnabled() (bool, bool) {
	if RDB == nil {
		return false, false
	}
	val, err := RDB.Get(ctx, settingPrefix+"local_login_enabled").Result()
	if err != nil {
		return false, false
	}
	return val == "true", true
}

// InvalidateSettingCache clears cached setting
func InvalidateSettingCache(key string) {
	if RDB == nil {
		return
	}
	RDB.Del(ctx, settingPrefix+key)
}
