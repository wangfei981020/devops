package cluster

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"ops-cmdb-backend/internal/store"
)

// 这组测试验证租约的 SQL 语义 —— 抢占、过期接管、fence 递增。
// 状态机逻辑在 leader_test.go 里用假实现测，不需要数据库；
// 但"这条 SQL 到底是不是原子的"只有真库能回答。
//
//	TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/db' go test ./internal/cluster/...
func testStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设 TEST_MYSQL_DSN，跳过租约的真库测试")
	}
	db, err := sql.Open("mysql", dsn+"?parseTime=true")
	if err != nil {
		t.Fatalf("连库: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS leases (
		name VARCHAR(128) NOT NULL, owner VARCHAR(255) NOT NULL,
		expires_at DATETIME(6) NOT NULL, fence BIGINT NOT NULL DEFAULT 1,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (name)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		t.Fatalf("建表: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM leases WHERE name LIKE 'test:%'`) })
	return store.New(db)
}

// ★ 两个副本同时抢，只能有一个拿到。
func TestOnlyOneAcquires(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	name := "test:one"

	a, _ := New(st, Options{Owner: "pod-a"})
	b, _ := New(st, Options{Owner: "pod-b"})

	_, okA, err := a.Acquire(ctx, name, 30*time.Second)
	if err != nil || !okA {
		t.Fatalf("pod-a 该拿到锁：ok=%v err=%v", okA, err)
	}
	_, okB, err := b.Acquire(ctx, name, 30*time.Second)
	if err != nil {
		t.Fatalf("pod-b Acquire 报错: %v", err)
	}
	if okB {
		t.Fatal("pod-b 也拿到了租约 —— 两个副本会同时跑定时任务")
	}
}

// ★ 自己续约不改 fence（还是同一段持有期）。
func TestRenewKeepsFence(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	name := "test:renew"
	a, _ := New(st, Options{Owner: "pod-a"})

	l1, _, _ := a.Acquire(ctx, name, 30*time.Second)
	l2, ok, _ := a.Acquire(ctx, name, 30*time.Second)
	if !ok {
		t.Fatal("续自己的约应当成功")
	}
	if l1.Fence != l2.Fence {
		t.Errorf("续约把 fence 从 %d 改成了 %d —— 那会让已发出的栅栏令牌失效",
			l1.Fence, l2.Fence)
	}
	if !l2.ExpiresAt.After(l1.ExpiresAt) {
		t.Error("续约没有延后到期时间")
	}
}

// ★ 过期后别人能接管，且 fence 递增。
func TestExpiredLeaseIsTakenOver(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	name := "test:expire"

	a, _ := New(st, Options{Owner: "pod-a"})
	b, _ := New(st, Options{Owner: "pod-b"})

	l1, _, _ := a.Acquire(ctx, name, 300*time.Millisecond)
	time.Sleep(500 * time.Millisecond)

	l2, ok, err := b.Acquire(ctx, name, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("租约已过期，pod-b 该接管：ok=%v err=%v", ok, err)
	}
	if l2.Fence <= l1.Fence {
		t.Errorf("易主后 fence 未递增（%d → %d）—— 旧持有者的写就挡不住了",
			l1.Fence, l2.Fence)
	}
}

// ★ 主动释放后立刻可被接管，不用等到自然过期。
func TestReleaseAllowsImmediateTakeover(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	name := "test:release"

	a, _ := New(st, Options{Owner: "pod-a"})
	b, _ := New(st, Options{Owner: "pod-b"})

	if _, ok, _ := a.Acquire(ctx, name, 5*time.Minute); !ok {
		t.Fatal("pod-a 没拿到锁")
	}
	if err := a.Release(ctx, name); err != nil {
		t.Fatalf("释放: %v", err)
	}
	if _, ok, _ := b.Acquire(ctx, name, 30*time.Second); !ok {
		t.Fatal("释放后 pod-b 仍拿不到 —— 滚动更新会有一个 TTL 的空窗")
	}
}

// ★★ Mutex 与 Leader 的关键差异：**同一个 owner 也不能重入**。
//
// 这条是最容易写错的：复用 Manager.Acquire 的话，同一个 Pod 内的两次双击
// 会被判成"续自己的约"而双双放行 —— 比进程内的 sync.Map 还差。
func TestMutexIsNotReentrant(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	mu := NewMutex(st)

	unlock, err := mu.Lock(ctx, "test:reentrant", 30*time.Second)
	if err != nil {
		t.Fatalf("第一次加锁: %v", err)
	}
	defer unlock()

	// 同一进程、同一个 Mutex 实例再加一次 —— 必须被拒
	if _, err := mu.Lock(ctx, "test:reentrant", 30*time.Second); err != ErrBusy {
		t.Fatalf("同一持有者重入应返回 ErrBusy，得到 %v —— 双击会重复扣费", err)
	}
}

// ★ 释放后能再次加锁。
func TestMutexUnlockAllowsRelock(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	mu := NewMutex(st)

	unlock, err := mu.Lock(ctx, "test:relock", 30*time.Second)
	if err != nil {
		t.Fatalf("加锁: %v", err)
	}
	unlock()

	u2, err := mu.Lock(ctx, "test:relock", 30*time.Second)
	if err != nil {
		t.Fatalf("释放后应能再加锁，得到 %v", err)
	}
	u2()
}
