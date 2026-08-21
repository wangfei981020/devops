package dnsource

import (
	"context"
	"sync"
	"testing"
)

// fakeQuota 模拟一个所有副本共享的计数器。
type fakeQuota struct {
	mu   sync.Mutex
	used map[string]int
	fail error
}

func (f *fakeQuota) Take(_ context.Context, scope string, limit int) (int, bool, error) {
	if f.fail != nil {
		return 0, true, f.fail
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.used[scope]++
	return f.used[scope], f.used[scope] <= limit, nil
}

// 这是这组改动的核心：**两个副本各有一份进程内计数器，但共享同一份配额**。
//
// 修复前每个副本各算 50/分钟，2 副本合计 100，越过 GoDaddy 的 60。
// 撞上之后的现象不是一句清楚的报错，而是批量续费里"莫名其妙有几个没续上"。
func TestSharedQuotaCapsAcrossReplicas(t *testing.T) {
	shared := &fakeQuota{used: map[string]int{}}
	SetQuota(shared)
	defer SetQuota(nil)

	const limit = 10
	// 两个 Limiter = 两个副本。各自的进程内计数是独立的，
	// 但 scope 相同，所以共享同一份跨副本配额
	repA := &Limiter{limit: limit, scope: "dnsource:1"}
	repB := &Limiter{limit: limit, scope: "dnsource:1"}

	allowed := 0
	for i := 0; i < limit; i++ {
		if repA.Allow() == nil {
			allowed++
		}
		if repB.Allow() == nil {
			allowed++
		}
	}
	if allowed != limit {
		t.Errorf("两个副本合计放行 %d 次，应为共享上限 %d 次——跨副本配额没生效", allowed, limit)
	}
}

// 不同数据源（不同厂商账号）的配额互不干扰。
// 共用一个 scope 会让多账号互相挤占，表现是"给 A 账号续费把 B 账号的配额吃光了"。
func TestQuotaScopedPerSource(t *testing.T) {
	shared := &fakeQuota{used: map[string]int{}}
	SetQuota(shared)
	defer SetQuota(nil)

	a := &Limiter{limit: 2, scope: "dnsource:1"}
	b := &Limiter{limit: 2, scope: "dnsource:2"}
	for i := 0; i < 2; i++ {
		if err := a.Allow(); err != nil {
			t.Fatalf("源 1 第 %d 次不该被限：%v", i+1, err)
		}
	}
	if err := a.Allow(); err == nil {
		t.Error("源 1 超限了却放行")
	}
	if err := b.Allow(); err != nil {
		t.Errorf("源 2 不该受源 1 影响：%v", err)
	}
}

// 配额后端故障时放行，而不是把正常续费全挡掉。
//
// 这与 Mutex 的选择相反，是刻意的：Mutex 守的是重复扣费，挂了必须拒绝；
// Quota 守的是厂商配额，挡掉的代价是"续不了费"，放行的代价只是可能撞一次
// 厂商限流（对方会拒绝，我们看得到）。但必须打 WARN，否则保护失效无人知晓。
func TestQuotaBackendFailureFailsOpen(t *testing.T) {
	SetQuota(&fakeQuota{used: map[string]int{}, fail: context.DeadlineExceeded})
	defer SetQuota(nil)

	l := &Limiter{limit: 5, scope: "dnsource:1"}
	if err := l.Allow(); err != nil {
		t.Errorf("配额后端故障时应放行，实际被拒：%v", err)
	}
}

// 没注入后端时（单副本部署、或注入前）退化成纯进程内计数，行为与改动前一致。
func TestNoQuotaBackendFallsBackToLocal(t *testing.T) {
	SetQuota(nil)
	l := &Limiter{limit: 3, scope: "dnsource:1"}
	for i := 0; i < 3; i++ {
		if err := l.Allow(); err != nil {
			t.Fatalf("第 %d 次不该被限：%v", i+1, err)
		}
	}
	if err := l.Allow(); err == nil {
		t.Error("超过进程内上限却放行了")
	}
}
