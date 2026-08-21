package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SendFeishu 必须三层都验：网络 → HTTP 状态码 → 业务 code。
//
// 原实现只看第一层，于是**所有投递失败都被记成「已送达」**。
//
// 实测（2026-08-17，本地）：填一个假 webhook
// `.../hook/FAKE-SECRET-abc123xyz789`，任务执行记录里
// notify_state 显示 **sent**、群名显示「全局兜底出口」。
//
// ⚠️ 这比"提醒发不出去"严重得多：
// 前者界面写着没发出去，人还会去查；后者界面写着已送达，于是没人会去查 ——
// 而域名到期、证书到期、磁盘告警全走这条路。
func TestSendFeishuDetectsFailures(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr bool
		errHas  string
	}{
		{
			name:    "飞书标准成功响应",
			status:  200,
			body:    `{"code":0,"msg":"success","data":{}}`,
			wantErr: false,
		},
		{
			// 🔴 这是最关键的一条：HTTP 200 但业务失败。
			// hook 被重置、机器人被移出群、地址里 token 错了，都长这样
			name:    "HTTP 200 + code≠0 → 必须报错",
			status:  200,
			body:    `{"code":19001,"msg":"param invalid"}`,
			wantErr: true,
			errHas:  "19001",
		},
		{
			name:    "HTTP 4xx",
			status:  400,
			body:    `bad request`,
			wantErr: true,
			errHas:  "HTTP 400",
		},
		{
			name:    "HTTP 500",
			status:  500,
			body:    `oops`,
			wantErr: true,
			errHas:  "HTTP 500",
		},
		{
			// webhook 填成了一个网页地址：拿到 200 但内容不是飞书响应。
			// 这时**不能断言成功** —— 消息肯定没进群
			name:    "200 但响应不是飞书的 JSON → 不能算成功",
			status:  200,
			body:    `<html><body>Hello</body></html>`,
			wantErr: true,
			errHas:  "不是预期的 JSON",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			err := SendFeishu(srv.URL, "测试消息")
			if c.wantErr && err == nil {
				t.Fatalf("期望报错，实际成功 —— 投递失败会被记成「已送达」")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("期望成功，实际报错：%v", err)
			}
			if c.errHas != "" && !strings.Contains(err.Error(), c.errHas) {
				t.Errorf("错误信息里没有 %q（排障时看的就是这句）：%v", c.errHas, err)
			}
		})
	}
}

// 空 webhook 不能返回 nil。
//
// 「没有配置投递出口」和「消息已送达」是完全相反的两件事，
// 返回 nil 会让调用方把 notify_state 记成 sent。
func TestSendFeishuEmptyWebhookIsNotSuccess(t *testing.T) {
	err := SendFeishu("", "测试消息")
	if err == nil {
		t.Fatal("空 webhook 返回了 nil —— 「根本没配」会被记成「已送达」")
	}
	if !strings.Contains(err.Error(), "未发送") {
		t.Errorf("错误信息应说清消息没发出去：%v", err)
	}
}
