package api

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
)

// claim 名各家 IdP 不同，且可能是嵌套路径（Keycloak 的角色在 realm_access.roles）。
func TestClaimPaths(t *testing.T) {
	m := map[string]any{
		"preferred_username": "zhangsan",
		"realm_access":       map[string]any{"roles": []any{"ops-admin", "sre"}},
		"single_group":       "only-one",
	}
	if got := claimStr(m, "preferred_username"); got != "zhangsan" {
		t.Errorf("平铺 claim 取错: %q", got)
	}
	if got := claimList(m, "realm_access.roles"); !reflect.DeepEqual(got, []string{"ops-admin", "sre"}) {
		t.Errorf("嵌套路径取错: %v", got)
	}
	// ⚠️ 有的 IdP 在只有一个群组时下发**字符串**而不是数组。
	// 只认数组的话，那个人的角色映射会静默不生效 —— 他会拿到默认角色，
	// 而配置页上映射规则写得好好的。
	if got := claimList(m, "single_group"); !reflect.DeepEqual(got, []string{"only-one"}) {
		t.Errorf("单值 claim 应当成一元数组，实际 %v", got)
	}
	if got := claimStr(m, "nope.deep"); got != "" {
		t.Errorf("不存在的路径应返回空串，实际 %q", got)
	}
}

// 🔴 默认角色绝不能是 admin：身份源里任何一个人登录一次就成了管理员。
func TestRoleMappingNeverEscalatesByDefault(t *testing.T) {
	cfg := &oidcConfig{
		DefaultRole: "viewer",
		RoleMapping: map[string]string{"ops-admin": "rule_admin"},
	}
	if got := mapRole(cfg, []string{"ops-admin"}); got != "rule_admin" {
		t.Errorf("命中映射应给映射角色，实际 %q", got)
	}
	if got := mapRole(cfg, []string{"随便一个组"}); got != "viewer" {
		t.Errorf("没命中应给默认角色，实际 %q", got)
	}
	if got := mapRole(cfg, nil); got != "viewer" {
		t.Errorf("没有群组时应给默认角色，实际 %q", got)
	}
	// 默认角色为空时兜底成 viewer，不能兜成空串（空串在权限判定里是"不受限"）
	if got := mapRole(&oidcConfig{}, nil); got != "viewer" {
		t.Errorf("默认角色为空时应兜底 viewer，实际 %q —— 空 role_code 在权限判定里意味着不受限", got)
	}
}

// id_token 的 payload 解析。
//
// ⚠️ 这里**不验签**是安全的：id_token 是我们自己刚从 token_endpoint
// 用 client_secret 通过 TLS 换回来的。改成接受前端传来的 id_token 时
// 必须加 JWKS 验签，否则任何人都能伪造身份。
func TestDecodeJWTPayload(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"sub": "123", "email": "a@b.c"})
	tok := "x." + base64.RawURLEncoding.EncodeToString(payload) + ".y"
	got, err := decodeJWTPayload(tok)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got["email"] != "a@b.c" {
		t.Errorf("payload 解错: %v", got)
	}
	if _, err := decodeJWTPayload("not-a-jwt"); err == nil {
		t.Error("非三段式 JWT 应报错，不能静默返回空 claim —— 那会让登录以「用户名为空」失败，排查方向完全错")
	}
}

// 接入失败时必须列出**实际拿到的 claim 名**。
//
// 只说"用户名为空"的话，用户不知道该把 username_claim 改成什么，
// 而这是接 SSO 时最常见的失败。
func TestClaimNamesForDiagnostics(t *testing.T) {
	got := claimNames(map[string]any{"sub": 1, "email": 2, "aud": 3})
	want := []string{"aud", "email", "sub"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claim 名应排序输出便于比对，实际 %v", got)
	}
}
