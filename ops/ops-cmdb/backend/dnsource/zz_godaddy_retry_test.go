package dnsource

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// ★ 只对**瞬时**错误重试（OPSCMDB-057）。
//
// 🔴 这条判据写反的后果是双向的，两边都很糟：
//
//	把终态当瞬时  → 凭据错了还重试 3 次，把一轮同步拖长、多打对方 3 次
//	把瞬时当终态  → 429 一次就放弃（**现状**：62 个域名里 19 个每轮都拉不到，
//	                而且哪些域名新鲜不可预期）
func TestIsTransient(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   bool
		why    string
	}{
		{"429 厂商限流", http.StatusTooManyRequests, nil, true,
			"限流是时间窗问题，等一会儿就好 —— 正是本条要修的那个"},
		{"500 对方故障", http.StatusInternalServerError, nil, true, "对方的问题，可能只是抖了一下"},
		{"502/503", http.StatusBadGateway, nil, true, "同上"},
		{"401 凭据错", http.StatusUnauthorized, nil, false,
			"重试一万次也一样。而且每次重试都在消耗限流配额，把别的域名挤掉"},
		{"403 无权限", http.StatusForbidden, nil, false, "同上"},
		{"404 域名不存在", http.StatusNotFound, nil, false, "对方没有这个域名，是终态"},
		{"400 参数错", http.StatusBadRequest, nil, false, "我们发错了，重试还是错"},
		{"网络层错误", 0, errors.New("dial tcp: i/o timeout"), true,
			"连响应都没拿到，对方可能只是网络抖动"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTransient(c.status, c.err); got != c.want {
				t.Errorf("isTransient(%d, %v) = %v，期望 %v —— %s", c.status, c.err, got, c.want, c.why)
			}
		})
	}
}

// ★ 退避必须**够长**。
//
// GoDaddy 的限流窗口是分钟级 —— 退 200ms 再打只是再撞一次，
// 还多消耗一次配额。这条防的是"加了重试但等于没加"。
func TestBackoffLongEnough(t *testing.T) {
	if len(transientRetries) < 2 {
		t.Fatal("至少要重试 2 次，否则撞上限流窗口的概率仍然很高")
	}
	var total time.Duration
	for i, d := range transientRetries {
		if d < time.Second {
			t.Errorf("第 %d 次退避只有 %v —— 分钟级的限流窗口下，这等于立刻再撞一次", i+1, d)
		}
		if i > 0 && d <= transientRetries[i-1] {
			t.Errorf("第 %d 次退避 %v 没有比上一次 %v 更长 —— 那就不是指数退避了",
				i+1, d, transientRetries[i-1])
		}
		total += d
	}
	// ⚠️ 也不能太长：一轮同步 62 个域名，每个都退满会把任务拖到超时
	if total > 2*time.Minute {
		t.Errorf("退避总时长 %v 过长，62 个域名各退一轮会把任务拖垮", total)
	}
}
