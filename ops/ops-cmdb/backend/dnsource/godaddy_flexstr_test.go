package dnsource

import (
	"encoding/json"
	"testing"
)

// 订单号是续费之后唯一能对上账单的凭据。原先它声明成 json.Number，
// 厂商一旦返回字符串型订单号，整个响应解析就失败——订单号、金额、币种
// 一起静默丢掉。这个测试盯的就是"换了类型也不能丢数据"。
func TestRenewRespFlexibleOrderID(t *testing.T) {
	type resp struct {
		OrderID  flexStr `json:"orderId"`
		Total    int64   `json:"total"`
		Currency string  `json:"currency"`
	}
	cases := []struct {
		name, body, wantID string
		wantTotal          int64
	}{
		{"数字型订单号（GoDaddy 当前行为）",
			`{"orderId":1234567,"total":25970000,"currency":"USD"}`, "1234567", 25970000},
		{"字符串型订单号（换了也不能崩）",
			`{"orderId":"ORD-9F3A","total":25970000,"currency":"USD"}`, "ORD-9F3A", 25970000},
		{"大数不能被 float64 精度吃掉",
			`{"orderId":900719925474099123,"total":1,"currency":"USD"}`, "900719925474099123", 1},
		{"没有订单号字段：其余字段照样要拿到",
			`{"total":25970000,"currency":"USD"}`, "", 25970000},
		{"显式 null",
			`{"orderId":null,"total":5,"currency":"USD"}`, "", 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var r resp
			if err := json.Unmarshal([]byte(c.body), &r); err != nil {
				t.Fatalf("解析失败（这正是原来的 bug）：%v", err)
			}
			if string(r.OrderID) != c.wantID {
				t.Errorf("订单号 = %q，期望 %q", r.OrderID, c.wantID)
			}
			if r.Total != c.wantTotal {
				t.Errorf("金额 = %d，期望 %d（解析一失败金额也会跟着丢）", r.Total, c.wantTotal)
			}
		})
	}
}
