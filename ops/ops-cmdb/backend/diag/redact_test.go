package diag

import "strings"

import "testing"

func TestRedact(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		mustGone string // 这一段必须不再出现
	}{
		{"JWT", `Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijk`, "eyJzdWIiOiIxMjM0NTY3ODkw"},
		{"连接串密码", `dial mysql://cmdb_user:S3cr3tP@ssw0rd@10.0.0.1:3306`, "S3cr3tP"},
		{"键值对", `error: password=hunter2butlonger failed`, "hunter2butlonger"},
		{"api key", `x-api-key: abc123def456ghi789`, "abc123def456ghi789"},
		{"私钥块", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nAAA\n-----END RSA PRIVATE KEY-----", "MIIEow"},
		{"长随机串", `token 9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a3928`, "9f8e7d6c5b4a39281706f5e4d3c2b1a0"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if strings.Contains(got, c.mustGone) {
			t.Errorf("%s: 敏感片段仍在\n  输入: %s\n  输出: %s", c.name, c.in, got)
		}
	}
}

// ⚠️ 正常日志不能被脱敏规则吃掉 —— 全被打码的日志喂给模型等于没喂，
// 而且人看审计时会以为出了什么问题。
func TestRedactKeepsNormalLogs(t *testing.T) {
	keep := []string{
		`2026-08-19T10:00:00Z ERROR failed to connect to 10.0.0.1:3306: connection refused`,
		`java.lang.OutOfMemoryError: Java heap space`,
		`no matches for kind "Cluster" in version "cluster.x-k8s.io/v1beta1"`,
		`Readiness probe failed: HTTP probe failed with statuscode: 503`,
	}
	for _, s := range keep {
		if got := Redact(s); got != s {
			t.Errorf("正常日志被改写了：\n  原: %s\n  后: %s", s, got)
		}
	}
}
