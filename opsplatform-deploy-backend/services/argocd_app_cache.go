package services

import (
	"context"
	"sync"
	"time"
)

// =========================================================================
//   ArgoCD App List Cache
//
// 用途：
//   - preview 阶段：UI 拉模块下拉时快速判断"该模块在 ArgoCD 是否已接入"
//   - submit 阶段：调 RefreshNow() 强制刷一次再校验，保证数据准确
//
// 范围：单 ArgoCD 实例 → 多 instance 时按 instance ID 分桶
// 后台 60s 自动 refresh，启动时 main.go 调一次预热
// =========================================================================

const (
	argoAppCacheTTL    = 60 * time.Second
	argoAppCacheTimeout = 8 * time.Second
)

type argoAppEntry struct {
	apps     map[string]bool // app 名 → 存在
	cachedAt time.Time
	err      error // 上次刷新失败的错误（让 caller 决定继续用旧缓存还是直接报）
}

// ArgocdAppCache 一个进程级单例，按 (argocd_url) 分桶
type ArgocdAppCache struct {
	mu      sync.RWMutex
	buckets map[string]*argoAppEntry // key = base URL
}

var globalArgocdAppCache = &ArgocdAppCache{buckets: make(map[string]*argoAppEntry)}

// GetArgocdAppCache 全局单例
func GetArgocdAppCache() *ArgocdAppCache { return globalArgocdAppCache }

// Has 查询某 ArgoCD 实例下某 app 是否存在
//
//	缓存未命中或过期 → 触发同步刷新一次，再查
//	返回 (exists, fresh, err)
//	  fresh=true 表示用的是新拉的数据；false 表示 stale > TTL 但本次刷新失败，给的旧数据
func (c *ArgocdAppCache) Has(ctx context.Context, client *ArgocdClient, appName string) (bool, bool, error) {
	if client == nil || client.BaseURL == "" {
		return false, false, nil
	}
	key := client.BaseURL
	c.mu.RLock()
	e, ok := c.buckets[key]
	c.mu.RUnlock()
	now := time.Now()
	if !ok || now.Sub(e.cachedAt) > argoAppCacheTTL {
		// 触发刷新（同步等结果）
		ne, err := c.refresh(ctx, client)
		if err != nil {
			// 旧缓存还能用就用
			if e != nil {
				return e.apps[appName], false, err
			}
			return false, false, err
		}
		e = ne
	}
	return e.apps[appName], true, nil
}

// RefreshNow 强制立即刷新（提交时 / 用户点手动刷新时调）
//
//	返回最新存在的 app 数量 + error
func (c *ArgocdAppCache) RefreshNow(ctx context.Context, client *ArgocdClient) (int, error) {
	if client == nil || client.BaseURL == "" {
		return 0, nil
	}
	e, err := c.refresh(ctx, client)
	if err != nil {
		return 0, err
	}
	return len(e.apps), nil
}

// LastRefreshedAt 给前端显示「上次同步: X 秒前」
func (c *ArgocdAppCache) LastRefreshedAt(client *ArgocdClient) (time.Time, bool) {
	if client == nil || client.BaseURL == "" {
		return time.Time{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.buckets[client.BaseURL]
	if !ok {
		return time.Time{}, false
	}
	return e.cachedAt, true
}

// refresh 实际拉取 app 列表（不加 selector，拿全量）
func (c *ArgocdAppCache) refresh(ctx context.Context, client *ArgocdClient) (*argoAppEntry, error) {
	rctx, cancel := context.WithTimeout(ctx, argoAppCacheTimeout)
	defer cancel()
	names, err := client.ListAppNames(rctx, "")
	if err != nil {
		return nil, err
	}
	apps := make(map[string]bool, len(names))
	for _, n := range names {
		apps[n] = true
	}
	e := &argoAppEntry{apps: apps, cachedAt: time.Now()}
	c.mu.Lock()
	c.buckets[client.BaseURL] = e
	c.mu.Unlock()
	return e, nil
}
