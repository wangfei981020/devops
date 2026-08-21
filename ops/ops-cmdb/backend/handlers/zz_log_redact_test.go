package handlers

import (
	"strings"
	"testing"

	"ops-cmdb-backend/diag"
)

// ★ 日志转发前的凭据脱敏（OPSCMDB-065）。
//
// 用例取自生产实测的真实形态（已改掉具体值）：
// niuniu-game-server-backend 以 INFO 级打印了解析后的完整配置。
func TestRedactLogTail(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		secret string // 不该出现在结果里
		keep   string // 必须保留（排障要用）
	}{
		{
			name:   "var=X, realVal=口令 形态",
			in:     "parseConfLine …, var=REDIS_PASSWORD, …, realVal=1qaz2wsx3edc6",
			secret: "1qaz2wsx3edc6",
			keep:   "REDIS_PASSWORD", // 变量名要留：它说明是哪个配置项
		},
		{
			name:   "DSN 连接串",
			in:     "dsn=g32_dev:s3cr3tPw@tcp(10.170.48.249:4000)/g33_niuniu_dev",
			secret: "s3cr3tPw",
			keep:   "10.170.48.249", // 地址要留：排障最常核对"连的哪台"
		},
		{
			name:   "Go 结构体 %+v 打印",
			in:     "Database:{UidDb:{Host:10.0.0.1 UserName:g32_dev Password:p@ss123} }",
			secret: "p@ss123",
			keep:   "UserName:g32_dev",
		},
		{
			name:   "URL 形态的连接串",
			in:     "connecting to postgres://cmdb:mypassword@db.internal:5432/ops",
			secret: "mypassword",
			keep:   "db.internal",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := diag.RedactLogTail(c.in)
			if strings.Contains(got, c.secret) {
				t.Errorf("口令仍在输出里：\n输入: %s\n输出: %s", c.in, got)
			}
			if n == 0 {
				t.Errorf("命中数为 0，但输入里确实有凭据：%s", c.in)
			}
			if c.keep != "" && !strings.Contains(got, c.keep) {
				t.Errorf("排障必需的信息被误伤了，期望保留 %q：\n%s", c.keep, got)
			}
		})
	}
}

// ★ 不能误伤：日志的可读性是 log_tails 的全部价值。
//
// 🔴 过度脱敏比不脱敏更糟 —— 本轮正是靠 log_tails 才找到「库名含换行符」
// 那个 bug（OPSCMDB-064）。把日志打成一片 ***REDACTED*** 等于把这个能力废掉。
func TestRedactLogTail_不误伤(t *testing.T) {
	for _, s := range []string{
		"level=info msg=\"listening on :8080\"",
		"Error 1049 (42000): Unknown database 'g33_niuniu_dev\\n'",
		"pulled image harbor.example.com/ops/cmdb@sha256:aadf416b2cdce311a8811ba3f0608a61",
		"GET /api/hosts?page=1&size=50 200 12ms",
	} {
		got, n := diag.RedactLogTail(s)
		if got != s || n != 0 {
			t.Errorf("正常日志被改动了：\n原文: %s\n结果: %s（命中 %d）", s, got, n)
		}
	}
}
