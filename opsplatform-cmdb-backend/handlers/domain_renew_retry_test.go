package handlers

import (
	"errors"
	"fmt"
	"testing"

	"opsplatform-cmdb-backend/dnsource"
)

// 这个函数把"要不要再花一次钱"的判断压在一个地方，所以值得单测。
// 判错的代价不对称：漏判只是少续一个域名（人工补），误判是重复扣费。
func TestIsTransientRenewErr(t *testing.T) {
	yes := []error{
		&dnsource.RateLimitError{Info: dnsource.RateLimitInfo{Limit: 50, RetryAfterSec: 30}},
		errors.New("Post \"https://api.godaddy.com/v1/domains/x/renew\": context deadline exceeded"),
		errors.New("godaddy renew failed: 429 Too Many Requests"),
		errors.New("godaddy renew failed: 502 Bad Gateway"),
		errors.New("read tcp 10.0.0.1:443: connection reset by peer"),
		errors.New("unexpected EOF"),
		fmt.Errorf("wrapped: %w", &dnsource.RateLimitError{}), // 包一层也要认得出来
	}
	for _, e := range yes {
		if !isTransientRenewErr(e) {
			t.Errorf("应判为瞬时可重试，实际不是：%v", e)
		}
	}

	// 这些重试多少次都一样，重试只是白白多打一次 API、多占一次限流配额
	no := []error{
		errors.New("godaddy renew failed: 401 Unauthorized: invalid API key"),
		errors.New("godaddy renew failed: 403 Forbidden"),
		errors.New("domain not found in account"),
		errors.New("insufficient funds"),
		errors.New("godaddy renew failed: 422 domain is not eligible for renewal"),
	}
	for _, e := range no {
		if isTransientRenewErr(e) {
			t.Errorf("确定性失败不该重试，却判成了可重试：%v", e)
		}
	}
}

// SafeRetry 的两个前提缺一不可。这里把四种组合列出来，
// 防止以后有人"顺手"把条件放宽成 err != "" 就重试。
func TestSafeRetryGate(t *testing.T) {
	cases := []struct {
		name             string
		verified         bool // 回查读到了到期日，且还是旧值 → 确认没扣费
		transient        bool // 错误是限流/超时/5xx
		wantAllowedRetry bool
	}{
		{"确认没扣费 + 瞬时错误 → 唯一可以重试的组合", true, true, true},
		{"确认没扣费 + 确定性错误 → 重试没意义", true, false, false},
		{"没查出来 + 瞬时错误 → 可能已扣费，绝不能重试", false, true, false},
		{"没查出来 + 确定性错误 → 更不能重试", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 复刻 renewOne 失败分支的赋值逻辑
			var res renewOneResult
			if c.verified {
				var err error = errors.New("403 Forbidden")
				if c.transient {
					err = errors.New("504 Gateway Timeout")
				}
				res = renewOneResult{Err: "续费失败：" + err.Error(), SafeRetry: isTransientRenewErr(err)}
			} else {
				res = renewOneResult{Err: "续费失败：无法确认是否扣费"} // SafeRetry 保持 false
			}
			if res.SafeRetry != c.wantAllowedRetry {
				t.Errorf("SafeRetry=%v，期望 %v", res.SafeRetry, c.wantAllowedRetry)
			}
		})
	}
}
