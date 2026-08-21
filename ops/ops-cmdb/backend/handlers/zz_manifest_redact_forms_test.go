package handlers

import (
	"strings"
	"testing"
)

// ★ 按**值的形态**脱敏（OPSCMDB-050）。
//
// 🔴 键名判据挡不住连接串和 webhook —— 它们的键名里没有任何敏感词：
//
//	DATABASE_URL:     postgres://user:口令@host:5432/db
//	LARK_WEBHOOK_URL: https://open.larksuite.com/open-apis/bot/v2/hook/<token>
//
// 而 get_manifest 只要**读 Deployment 的权限**就能调，
// 等于把"能读 Secret"降到了"能读 Deployment"。
func TestRedact_值形态(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOut string // 期望的结果；空表示只断言"不含原口令"
		secret  string // 不该出现在结果里的那一段
	}{
		{
			name:   "连接串的口令段被打码，但 scheme/user/host 保留",
			in:     "postgres://g32_dev:s3cr3tPass@tmflow-g01-postgres-svc.devops:5432/g01",
			secret: "s3cr3tPass",
			// ⚠️ 保留 host 和 user 是有意的：排障时最常核对的就是"连的哪台库、哪个账号"
			wantOut: "postgres://g32_dev:" + redactedMark + "@tmflow-g01-postgres-svc.devops:5432/g01",
		},
		{
			name:    "飞书 webhook 的 token 段被打码",
			in:      "https://open.larksuite.com/open-apis/bot/v2/hook/8f2a1b3c-4d5e-6f70-8192-a3b4c5d6e7f8",
			secret:  "8f2a1b3c-4d5e-6f70-8192-a3b4c5d6e7f8",
			wantOut: "https://open.larksuite.com/open-apis/bot/v2/hook/" + redactedMark,
		},
		{
			name: "镜像 digest 不能被误伤",
			// 🔴 长随机串不等于凭据。把 digest 打掉，排障时就认不出跑的是哪个镜像了
			in:      "harbor.example.com/ops/cmdb@sha256:aadf416b2cdce311a8811ba3f0608a61b77dbf997500e2eafe781b51f6a0b019",
			wantOut: "harbor.example.com/ops/cmdb@sha256:aadf416b2cdce311a8811ba3f0608a61b77dbf997500e2eafe781b51f6a0b019",
		},
		{
			name:    "普通 URL 不动",
			in:      "https://cmdb.example.com/api/hosts",
			wantOut: "https://cmdb.example.com/api/hosts",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := redactValueForms(c.in)
			if c.wantOut != "" && got != c.wantOut {
				t.Errorf("\n期望: %s\n实际: %s", c.wantOut, got)
			}
			if c.secret != "" && strings.Contains(got, c.secret) {
				t.Errorf("口令仍在输出里: %s", got)
			}
		})
	}
}

// ★ env 项的 name 不敏感、value 是连接串时也要脱敏。
//
// ⚠️ 这一条单列，是因为 env 走的是**另一条分支**（按 name 判 value），
// 只测顶层键值会漏掉它 —— 而生产上的 DATABASE_URL 恰恰就是 env 项。
func TestRedact_env项按值形态(t *testing.T) {
	obj := map[string]any{
		"spec": map[string]any{
			"containers": []any{map[string]any{
				"env": []any{
					map[string]any{"name": "DATABASE_URL", "value": "mysql://root:p@ssw0rd@10.0.0.1:3306/db"},
					map[string]any{"name": "LARK_WEBHOOK_URL", "value": "https://open.larksuite.com/open-apis/bot/v2/hook/abcdef0123456789abcdef"},
					map[string]any{"name": "LOG_LEVEL", "value": "info"},
				},
			}},
		},
	}
	n, _ := scrubManifest(obj)
	if n < 2 {
		t.Fatalf("两个 env 值都该被脱敏，实际只脱了 %d 处", n)
	}
	dump := flatten(obj)
	for _, leak := range []string{"p@ssw0rd", "abcdef0123456789abcdef"} {
		if strings.Contains(dump, leak) {
			t.Errorf("凭据仍在输出里: %s", leak)
		}
	}
	if !strings.Contains(dump, "info") {
		t.Error("非敏感值 info 被误伤了")
	}
}

// ★ 计数要如实：判不准的必须单独报，不能只说"打了 N 处"。
//
// 🔴 这正是 OPSCMDB-050 里那句「给出'已经处理干净了'的错误暗示」——
// 实测是打了 4 处漏了 2 处，而提示只说打了 4 处。
func TestRedact_判不准的要单独计数(t *testing.T) {
	obj := map[string]any{
		"data": map[string]any{
			"note":    "hello",
			"blob":    "dGhpcyBpcyBhIHZlcnkgbG9uZyBiYXNlNjQgc3RyaW5nIHRoYXQgbG9va3MgbGlrZSBhIHNlY3JldA==",
			"pemLike": "-----BEGIN RSA PRIVATE KEY-----",
		},
	}
	_, suspicious := scrubManifest(obj)
	if suspicious < 2 {
		t.Errorf("Base64 与私钥块都该被计入'判不准'，实际 %d 处 —— "+
			"只报脱敏数会让人以为已经干净了", suspicious)
	}
}

// flatten 把嵌套结构拍平成一个字符串，方便断言"某个值还在不在里面"。
func flatten(v any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		case string:
			b.WriteString(t)
			b.WriteString("\n")
		}
	}
	walk(v)
	return b.String()
}
