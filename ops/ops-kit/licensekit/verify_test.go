package licensekit

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sign(t *testing.T, priv ed25519.PrivateKey, p Payload) string {
	t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return Encode(body, ed25519.Sign(priv, body))
}

func samplePayload() Payload {
	return Payload{
		LicenseID: "LIC-2026-0001",
		Licensee: Licensee{
			Org: "Acme 科技有限公司", TaxID: "91310000MA1FL0XXXX",
			Contact: "张三", Email: "ops@acme.example", ScopeName: "生产",
		},
		Products: map[string]ProductGrant{
			"ops-data-plane":  {Features: []string{"tenant.multi", "sso.oidc"}, Capacity: map[string]int64{"nodes": 1000}},
			"ops-alert-plane": {Features: []string{"tenant.multi", "incident.oncall"}, Capacity: map[string]int64{"rules": 200}},
		},
		IssuedAt:  time.Now().Add(-24 * time.Hour),
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok := sign(t, priv, samplePayload())

	p, err := Verify(tok, pub)
	if err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
	if p.Licensee.Org != "Acme 科技有限公司" {
		t.Errorf("授权主体解析错误: %q", p.Licensee.Org)
	}
	if len(p.Products) != 2 {
		t.Errorf("应含 2 个产品授权, got %d", len(p.Products))
	}
	g, ok := p.Grant("ops-alert-plane")
	if !ok || !g.Has("incident.oncall") {
		t.Error("应能取到告警平面的授权")
	}
	// 只买了一个产品的客户，另一个产品查不到自己的 key —— 这是正确行为
	if _, ok := p.Grant("ops-nonexist-plane"); ok {
		t.Error("未授权的产品不该有 grant")
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok := sign(t, priv, samplePayload())

	// 典型攻击：改载荷加功能，保留原签名
	p := samplePayload()
	p.Products["ops-alert-plane"] = ProductGrant{Features: []string{"everything"}}
	body, _ := json.Marshal(p)
	forged := base64.RawURLEncoding.EncodeToString(body) + "." + strings.Split(tok, ".")[1]

	if _, err := Verify(forged, pub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("篡改后必须报签名无效, got %v", err)
	}
}

func TestWrongKeyRejected(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	tok := sign(t, priv, samplePayload())

	if _, err := Verify(tok, otherPub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("换一把公钥必须验不过, got %v", err)
	}
}

// 过期的 license 必须能验签通过 —— 过期是状态不是错误，由 Manager 判宽限期。
// 若在验签阶段就拒绝，客户续签晚一天系统就罢工，与「过期不停服」直接冲突。
func TestExpiredStillVerifies(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := samplePayload()
	p.ExpiresAt = time.Now().Add(-100 * 24 * time.Hour)

	got, err := Verify(sign(t, priv, p), pub)
	if err != nil {
		t.Fatalf("过期 license 也应验签通过（过期交给状态机判）: %v", err)
	}
	if !got.ExpiresAt.Before(time.Now()) {
		t.Error("到期时间应保持原样")
	}
}

// 指纹不匹配同样不在验签阶段拒绝 —— 有 14 天宽限。
func TestFingerprintMismatchStillVerifies(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := samplePayload()
	p.InstallID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	if _, err := Verify(sign(t, priv, p), pub); err != nil {
		t.Fatalf("指纹不匹配不该在验签阶段失败: %v", err)
	}
}

func TestMalformedTokens(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	cases := map[string]string{
		"空":         "",
		"只有一段":      "abcdef",
		"三段":        "a.b.c",
		"载荷非base64": "!!!.aGVsbG8",
		"签名非base64": "aGVsbG8.###",
		"签名长度不对":    "aGVsbG8.aGVsbG8",
	}
	for name, tok := range cases {
		if _, err := Verify(tok, pub); err == nil {
			t.Errorf("%s 应被拒绝", name)
		}
	}
}

// 客户从邮件复制激活码常带换行，不该因此报"格式不正确"来回沟通。
func TestWhitespaceTolerated(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok := sign(t, priv, samplePayload())
	messy := tok[:40] + "\n  " + tok[40:80] + "\r\n" + tok[80:]

	if _, err := Verify(messy, pub); err != nil {
		t.Fatalf("应容忍换行与空格: %v", err)
	}
}

func TestInspectDoesNotValidate(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok := sign(t, priv, samplePayload())
	// 换成完全无效的签名，Inspect 仍应能读出内容（它明确不验签）
	broken := strings.Split(tok, ".")[0] + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	p, err := Inspect(broken)
	if err != nil {
		t.Fatalf("Inspect 应能解码: %v", err)
	}
	if p.LicenseID != "LIC-2026-0001" {
		t.Error("Inspect 应读出内容")
	}
}

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint("550e8400-e29b-41d4-a716-446655440000", 1234567890)
	b := Fingerprint("550e8400-e29b-41d4-a716-446655440000", 1234567890)
	if a != b {
		t.Fatal("同样输入必须得到同样指纹，否则多副本会各算各的")
	}
	if len(a) != 32 {
		t.Fatalf("指纹长度应为 32, got %d", len(a))
	}
	if Fingerprint("550e8400-e29b-41d4-a716-446655440001", 1234567890) == a {
		t.Error("换 UUID 指纹应变")
	}
	if Fingerprint("550e8400-e29b-41d4-a716-446655440000", 1234567891) == a {
		t.Error("换系统标识指纹应变")
	}
	if ShortFingerprint(a) == a || !strings.Contains(ShortFingerprint(a), "…") {
		t.Error("短指纹应截断显示")
	}
}

func TestEmbeddedKeysDistinctAndProdDefault(t *testing.T) {
	if pubKeyDev == pubKeyProd {
		t.Fatal("dev 与 prod 必须是不同的密钥对")
	}
	t.Setenv(EnvVar, "")
	if KeyEnv() != "prod" {
		t.Error("未设置环境变量时必须默认 prod")
	}
	t.Setenv(EnvVar, "production")
	if KeyEnv() != "prod" {
		t.Error("非 dev 的任意值都按 prod 处理")
	}
	t.Setenv(EnvVar, "dev")
	if KeyEnv() != "dev" {
		t.Error("显式 dev 才用开发密钥")
	}
	for _, env := range []string{"dev", "prod"} {
		t.Setenv(EnvVar, env)
		if _, err := PublicKey(); err != nil {
			t.Fatalf("%s 公钥不可用: %v", env, err)
		}
	}
}

// 内嵌公钥必须与签发方私钥配对，且 dev 签的进不了 prod。
// 私钥不在仓库里，别的机器上自动跳过；签发方本机必须通过 ——
// 否则会出现"签出来的 license 客户装不上"这种最晚才发现的事故。
func TestDevSignedRejectedInProd(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法定位用户目录")
	}
	raw, err := os.ReadFile(filepath.Join(home, "vscode", "license-authority", "keys", "dev.private.key"))
	if err != nil {
		t.Skip("签发私钥不在本机，跳过")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		t.Fatal("私钥格式异常")
	}
	tok := sign(t, ed25519.PrivateKey(key), samplePayload())

	t.Setenv(EnvVar, "dev")
	if _, err := VerifyEmbedded(tok); err != nil {
		t.Fatalf("dev 私钥签的应在 dev 环境通过（内嵌 dev 公钥可能与私钥不配对）: %v", err)
	}
	t.Setenv(EnvVar, "prod")
	if _, err := VerifyEmbedded(tok); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("dev 签的绝不能在 prod 环境生效, got %v", err)
	}
}
