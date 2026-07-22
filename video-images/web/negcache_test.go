package main

import (
	"testing"
	"time"
)

func TestNegCacheTTLAndCap(t *testing.T) {
	negCache = map[string]time.Time{}
	negTTL = 50 * time.Millisecond

	// 记下不存在 → 立即命中
	negPut("missing.jpg")
	if !negHit("missing.jpg") {
		t.Fatal("刚 negPut 的 key 应命中负缓存")
	}
	// 从没记过的 key → 不命中
	if negHit("other.jpg") {
		t.Fatal("未记录的 key 不应命中")
	}
	// 过期后 → 不命中,且惰性清除
	time.Sleep(70 * time.Millisecond)
	if negHit("missing.jpg") {
		t.Fatal("超过 TTL 后不应命中")
	}
	if _, ok := negCache["missing.jpg"]; ok {
		// negHit 命中过期项时应 delete
		t.Fatal("过期项应被惰性清除")
	}

	// 上限保护:超过 negCacheMax 时整体清空,不无限增长
	for i := 0; i < negCacheMax+10; i++ {
		negPut(string(rune(i%128)) + string(rune(i/128)))
	}
	if len(negCache) > negCacheMax {
		t.Fatalf("负缓存条数 %d 超过上限 %d,未触发清空", len(negCache), negCacheMax)
	}

	// TTL=0 时负缓存整体关闭
	negTTL = 0
	negPut("x.jpg")
	if negHit("x.jpg") {
		t.Fatal("TTL=0 时负缓存应关闭")
	}
}
