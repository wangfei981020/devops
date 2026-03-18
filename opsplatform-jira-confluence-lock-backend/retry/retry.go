package retry

import (
	"fmt"
	"log"
	"math"
	"time"
)

// Config 重试配置
type Config struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultConfig 默认: 10次指数退避, 2s起步, 最大5分钟
func DefaultConfig() Config {
	return Config{
		MaxRetries: 10,
		BaseDelay:  2 * time.Second,
		MaxDelay:   5 * time.Minute,
	}
}

// Do 执行带重试的操作，返回重试次数和错误
func Do(cfg Config, operation string, fn func() error) (int, error) {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(2, float64(attempt-1)))
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			log.Printf("[重试] %s 第 %d/%d 次, 等待 %v", operation, attempt, cfg.MaxRetries, delay)
			time.Sleep(delay)
		}

		if err := fn(); err != nil {
			lastErr = err
			log.Printf("[重试] %s 失败 (第 %d 次): %v", operation, attempt+1, err)
			continue
		}
		if attempt > 0 {
			log.Printf("[重试] %s 成功 (第 %d 次)", operation, attempt+1)
		}
		return attempt, nil
	}
	return cfg.MaxRetries, fmt.Errorf("%s 重试 %d 次后仍然失败: %w", operation, cfg.MaxRetries, lastErr)
}
