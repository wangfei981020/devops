package handlers

import (
	"testing"

	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// TestEffectivePrefix 验证密钥前缀取值：有覆盖用覆盖(转小写)，否则=项目名去后缀转小写。
func TestEffectivePrefix(t *testing.T) {
	cases := []struct {
		name, envType, override, want string
	}{
		{"G50-uat", "uat", "", "g50"},         // 正规项目：大写 G50 → 自动转小写 g50
		{"g50-uat", "uat", "", "g50"},         // 已小写：不变
		{"pa-re-uat", "uat", "", "pa-re"},     // 多段项目名：去 -uat → pa-re
		{"G33-uat", "uat", "g32", "g32"},      // 特例：覆盖成 g32
		{"G33-prod", "prod", "g32", "g32"},    // prod 也覆盖成 g32
		{"G66-uat", "uat", "G66", "g66"},      // 覆盖值也转小写
	}
	for _, c := range cases {
		p := &models.ProjectEnv{Name: c.name, EnvType: c.envType, SecretPrefix: c.override}
		if got := effectivePrefix(p); got != c.want {
			t.Errorf("effectivePrefix(%s, override=%q) = %q, want %q", c.name, c.override, got, c.want)
		}
	}
}

// TestRenameWithEffectivePrefix 验证换前缀端到端：g33 复用 g32 保持不换；正规项目换成自己前缀。
func TestRenameWithEffectivePrefix(t *testing.T) {
	// 模板样板的 extraEnvVars 用 g32- 打头，外加一个无项目前缀的 encyrpt-salt
	tpl := []byte(`extraEnvVars:
  - name: g32-nacos-secret
    valueFrom: {}
  - name: g32-redis-secret
    valueFrom: {}
  - name: encyrpt-salt
    valueFrom: {}
`)
	// 已登记项目前缀集合（模拟 allProjectPrefixes 转小写后的结果）
	known := map[string]bool{"g32": true, "g33": true, "g50": true, "pa-re": true}

	// 场景1：g50-uat（无覆盖）→ 应换成 g50-*，encyrpt-salt 不动
	g50 := &models.ProjectEnv{Name: "G50-uat", EnvType: "uat"}
	out := string(services.RenameSecretRefs(tpl, known, effectivePrefix(g50)))
	if !contains(out, "g50-nacos-secret") || !contains(out, "g50-redis-secret") {
		t.Errorf("g50 应换成 g50-*，实际:\n%s", out)
	}
	if !contains(out, "encyrpt-salt") || contains(out, "g50-salt") {
		t.Errorf("encyrpt-salt 不该被换，实际:\n%s", out)
	}

	// 场景2：g33-uat（覆盖=g32）→ 源==目标=g32 → 保持 g32-*，不出现 g33-
	g33 := &models.ProjectEnv{Name: "G33-uat", EnvType: "uat", SecretPrefix: "g32"}
	out2 := string(services.RenameSecretRefs(tpl, known, effectivePrefix(g33)))
	if !contains(out2, "g32-nacos-secret") || contains(out2, "g33-") {
		t.Errorf("g33 复用 g32 应保持 g32-*，实际:\n%s", out2)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
