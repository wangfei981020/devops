package licensekit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSource 可控的授权来源，模拟数据库。
type fakeSource struct {
	mu       sync.Mutex
	rev      string
	payload  *Payload
	revErr   error
	fetchErr error
	fetches  int // Fetch 被调了几次，用来验证"没变就不拉全量"
}

func (f *fakeSource) Revision(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rev, f.revErr
}

func (f *fakeSource) Fetch(context.Context) (*Payload, string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches++
	if f.fetchErr != nil {
		return nil, "", time.Time{}, f.fetchErr
	}
	return f.payload, "", time.Time{}, nil
}

func (f *fakeSource) set(rev string, p *Payload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rev, f.payload = rev, p
}

func (f *fakeSource) fail(rev, fetch error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revErr, f.fetchErr = rev, fetch
}

func (f *fakeSource) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}

func watchManager() *Manager {
	return NewManager(Options{
		Product:     "p",
		Implemented: map[string]bool{"sso": true},
		Plans:       map[string][]string{"standard": {"sso"}},
		Now:         func() time.Time { return time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) },
	})
}

func activePayload() *Payload {
	return &Payload{
		ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:standard"}}},
	}
}

// 等到条件成立或超时。轮询测试不能靠 sleep 固定时长——机器慢的时候会假红。
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时仍未满足：%s", what)
}

// 这就是多副本要解决的场景：另一个副本激活了，本副本靠轮询自己收敛过来。
func TestWatchConvergesFromOtherReplica(t *testing.T) {
	m := watchManager()
	src := &fakeSource{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Watch(ctx, src, 10*time.Millisecond)

	if m.Status() != StatusNotActivated {
		t.Fatal("起始应当是未激活")
	}

	// 管理员在 A 副本粘了激活码 → 写进了库
	src.set("rev-1", activePayload())

	eventually(t, "本副本收敛到已激活", func() bool { return m.Status() == StatusActive })
	if !m.Has("sso") {
		t.Error("收敛后功能应当可用")
	}
}

// 没变就不该反复拉全量 —— Revision 是廉价查询，Fetch 不是。
func TestWatchDoesNotRefetchWhenUnchanged(t *testing.T) {
	m := watchManager()
	src := &fakeSource{rev: "rev-1", payload: activePayload()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Watch(ctx, src, 5*time.Millisecond)

	eventually(t, "首次装载完成", func() bool { return m.Status() == StatusActive })
	time.Sleep(80 * time.Millisecond) // 期间至少十几个轮询周期

	if n := src.fetchCount(); n != 1 {
		t.Errorf("revision 没变却拉了 %d 次全量，应当只有首次那 1 次", n)
	}
}

// ★ 最重要的一条：数据库抖一下，绝不能把已生效的授权吊销掉。
//
// "晚 30 秒生效"和"全公司突然变只读"不是一个量级的事故。
func TestWatchKeepsStateOnError(t *testing.T) {
	m := watchManager()
	src := &fakeSource{rev: "rev-1", payload: activePayload()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var errs int
	m.onWatchError = func(error) { mu.Lock(); errs++; mu.Unlock() }

	go m.Watch(ctx, src, 5*time.Millisecond)
	eventually(t, "先装载成功", func() bool { return m.Status() == StatusActive })

	src.fail(errors.New("数据库连不上"), nil)
	eventually(t, "错误已上报", func() bool { mu.Lock(); defer mu.Unlock(); return errs > 0 })

	if got := m.Status(); got != StatusActive {
		t.Fatalf("数据库出错期间状态应当保持 %s，得到 %s", StatusActive, got)
	}
	if !m.Has("sso") {
		t.Error("数据库出错不该影响已生效的功能")
	}
}

// Fetch 出错时不能记住 revision，否则这一版永远拉不下来了。
func TestWatchRetriesAfterFetchError(t *testing.T) {
	m := watchManager()
	src := &fakeSource{rev: "rev-1", fetchErr: errors.New("超时")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Watch(ctx, src, 5*time.Millisecond)

	eventually(t, "至少重试过几次", func() bool { return src.fetchCount() >= 3 })
	if m.Status() != StatusNotActivated {
		t.Error("一直失败时应当保持未激活")
	}

	// 数据库恢复
	src.fail(nil, nil)
	src.set("rev-1", activePayload()) // revision 没变，但之前没成功过
	eventually(t, "恢复后补上", func() bool { return m.Status() == StatusActive })
}

// 授权被删（解绑）→ 退回未激活。
// 注意这与"查不出来"必须区分：前者是确认没有，后者是不知道。
func TestWatchClearsWhenLicenseRemoved(t *testing.T) {
	m := watchManager()
	src := &fakeSource{rev: "rev-1", payload: activePayload()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Watch(ctx, src, 5*time.Millisecond)

	eventually(t, "先激活", func() bool { return m.Status() == StatusActive })

	src.set("rev-2", nil) // 库里的授权被清了
	eventually(t, "退回未激活", func() bool { return m.Status() == StatusNotActivated })
}

func TestWatchStopsOnContextCancel(t *testing.T) {
	m := watchManager()
	src := &fakeSource{rev: "rev-1", payload: activePayload()}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() { m.Watch(ctx, src, 5*time.Millisecond); close(done) }()

	eventually(t, "先跑起来", func() bool { return m.Status() == StatusActive })
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ctx 取消后 Watch 没有退出，goroutine 泄漏")
	}
}

// 参数不合法时安静返回，不要卡住调用方的 goroutine。
func TestWatchIgnoresBadArgs(t *testing.T) {
	m := watchManager()
	done := make(chan struct{})
	go func() {
		m.Watch(context.Background(), nil, time.Second)
		m.Watch(context.Background(), &fakeSource{}, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("非法参数下 Watch 应当立即返回")
	}
}
