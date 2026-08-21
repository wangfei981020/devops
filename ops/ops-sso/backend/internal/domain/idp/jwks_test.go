package idp

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// 造一个真实签名的 id_token —— 用真密钥签，而不是塞一段假签名。
// 假签名只能证明"我们拒绝了明显错的东西"，证明不了"我们接受正确的东西"。
func signRS256(t *testing.T, priv *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	hdr, _ := json.Marshal(jwtHeader{Alg: "RS256", Kid: kid, Typ: "JWT"})
	pl, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." +
		base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func rsaJWKS(t *testing.T, priv *rsa.PrivateKey, kid string) JWKS {
	t.Helper()
	return JWKS{Keys: []JWK{{
		Kid: kid, Kty: "RSA", Alg: "RS256", Use: "sig",
		N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		E: encodeUint(uint64(priv.E)),
	}}}
}

func TestVerifyRS256(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := rsaJWKS(t, priv, "k1")
	tok := signRS256(t, priv, "k1", map[string]any{"sub": "u1"})

	if err := VerifySignature(tok, jwks); err != nil {
		t.Fatalf("正确签名应通过：%v", err)
	}
}

// ★ 载荷被改一个字节 → 验签必须失败。
// 这条是整个 OIDC 接入的地基：不验签的话，任何人构造 {"sub":"admin"} 就能登进来。
func TestTamperedPayloadRejected(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := rsaJWKS(t, priv, "k1")
	tok := signRS256(t, priv, "k1", map[string]any{"sub": "u1"})

	// 把载荷换成 admin，签名照抄
	forged, _ := json.Marshal(map[string]any{"sub": "admin"})
	parts := splitJWT(tok)
	bad := parts[0] + "." + base64.RawURLEncoding.EncodeToString(forged) + "." + parts[2]

	if err := VerifySignature(bad, jwks); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("伪造载荷必须被拒，得到 %v", err)
	}
}

// 别人的密钥签的 token
func TestWrongKeyRejected(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := signRS256(t, other, "k1", map[string]any{"sub": "u1"})

	if err := VerifySignature(tok, rsaJWKS(t, priv, "k1")); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("别人密钥签的 token 必须被拒，得到 %v", err)
	}
}

// ★ alg=none：JWT 规范里最著名的坑
func TestAlgNoneRejected(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	hdr, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	pl, _ := json.Marshal(map[string]any{"sub": "admin"})
	tok := base64.RawURLEncoding.EncodeToString(hdr) + "." +
		base64.RawURLEncoding.EncodeToString(pl) + "."

	if err := VerifySignature(tok, rsaJWKS(t, priv, "k1")); !errors.Is(err, ErrUnsupportedAlg) {
		t.Fatalf("alg=none 必须被拒，得到 %v", err)
	}
}

// ★ 算法混淆：把 RSA 公钥当 HMAC 密钥用。不支持对称算法，这条路就堵死了。
func TestHS256Rejected(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	hdr, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	pl, _ := json.Marshal(map[string]any{"sub": "admin"})
	tok := base64.RawURLEncoding.EncodeToString(hdr) + "." +
		base64.RawURLEncoding.EncodeToString(pl) + ".fakesig"

	if err := VerifySignature(tok, rsaJWKS(t, priv, "k1")); !errors.Is(err, ErrUnsupportedAlg) {
		t.Fatalf("HS256 必须被拒（算法混淆攻击的入口），得到 %v", err)
	}
}

func TestKidNotFound(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := signRS256(t, priv, "k2", map[string]any{"sub": "u1"})

	if err := VerifySignature(tok, rsaJWKS(t, priv, "k1")); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("kid 找不到应报 ErrKeyNotFound，得到 %v", err)
	}
}

// 多把密钥而 token 没带 kid → 必须拒绝，不能猜
func TestMultipleKeysWithoutKidRejected(t *testing.T) {
	a, _ := rsa.GenerateKey(rand.Reader, 2048)
	b, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := JWKS{Keys: append(rsaJWKS(t, a, "k1").Keys, rsaJWKS(t, b, "k2").Keys...)}

	hdr, _ := json.Marshal(map[string]string{"alg": "RS256"}) // 没有 kid
	pl, _ := json.Marshal(map[string]any{"sub": "u1"})
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(signing))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, a, crypto.SHA256, sum[:])
	tok := signing + "." + base64.RawURLEncoding.EncodeToString(sig)

	if err := VerifySignature(tok, jwks); !errors.Is(err, ErrNoKID) {
		t.Fatalf("多把密钥且无 kid 时必须拒绝而不是猜，得到 %v", err)
	}
}

// 只有一把密钥时允许无 kid（有些自建 IdP 不发 kid）
func TestSingleKeyWithoutKidAllowed(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256"})
	pl, _ := json.Marshal(map[string]any{"sub": "u1"})
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(signing))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])

	if err := VerifySignature(signing+"."+base64.RawURLEncoding.EncodeToString(sig),
		rsaJWKS(t, priv, "")); err != nil {
		t.Fatalf("单密钥无 kid 应通过：%v", err)
	}
}

func TestES256(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	hdr, _ := json.Marshal(jwtHeader{Alg: "ES256", Kid: "e1"})
	pl, _ := json.Marshal(map[string]any{"sub": "u1"})
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(signing))
	r, s, _ := ecdsa.Sign(rand.Reader, priv, sum[:])

	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	tok := signing + "." + base64.RawURLEncoding.EncodeToString(sig)

	jwks := JWKS{Keys: []JWK{{
		Kid: "e1", Kty: "EC", Crv: "P-256",
		X: base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y: base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}}}
	if err := VerifySignature(tok, jwks); err != nil {
		t.Fatalf("ES256 应通过：%v", err)
	}
}

func TestParseJWKS(t *testing.T) {
	if _, err := ParseJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Error("空 keys 应报错")
	}
	if _, err := ParseJWKS([]byte(`not json`)); err == nil {
		t.Error("非 JSON 应报错")
	}
	j, err := ParseJWKS([]byte(`{"keys":[{"kid":"a","kty":"RSA","n":"AQAB","e":"AQAB"}]}`))
	if err != nil || len(j.Keys) != 1 {
		t.Fatalf("正常 JWKS 应解析成功：%v", err)
	}
}

// ── 缓存 ──

func TestKeyCacheTTLAndForceRefresh(t *testing.T) {
	c := NewKeyCache()
	now := time.Unix(1_700_000_000, 0)
	url := "https://idp.example.com/jwks"

	if _, need := c.Get(url, now); !need {
		t.Fatal("空缓存应要求拉取")
	}
	c.Put(url, JWKS{Keys: []JWK{{Kid: "k1"}}}, now)

	if _, need := c.Get(url, now.Add(30*time.Minute)); need {
		t.Fatal("TTL 内不该重复拉取 —— 每次登录都拉一次会让上游抖动变成全公司登不上")
	}
	if _, need := c.Get(url, now.Add(2*time.Hour)); !need {
		t.Fatal("超过 TTL 应重新拉取")
	}

	// kid 未命中时允许强刷一次，但要有最小间隔 ——
	// 否则攻击者用随机 kid 疯狂请求，我们就成了打上游的放大器
	if !c.AllowForceRefresh(url, now) {
		t.Fatal("首次强刷应被允许")
	}
	if c.AllowForceRefresh(url, now.Add(10*time.Second)) {
		t.Fatal("最小间隔内不该再次强刷")
	}
	if !c.AllowForceRefresh(url, now.Add(2*time.Minute)) {
		t.Fatal("超过最小间隔应允许强刷（密钥轮换当天要能自愈）")
	}
}

func splitJWT(s string) [3]string {
	var out [3]string
	i, start := 0, 0
	for j := 0; j < len(s) && i < 2; j++ {
		if s[j] == '.' {
			out[i] = s[start:j]
			i++
			start = j + 1
		}
	}
	out[2] = s[start:]
	return out
}
