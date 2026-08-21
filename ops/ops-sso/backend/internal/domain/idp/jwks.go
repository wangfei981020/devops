package idp

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// # 为什么必须验签
//
// id_token 的载荷是明文 base64，任何人都能构造一个 `{"sub":"admin"}`。
// 不验签的 OIDC 接入等于「谁都能声称自己是任何人」——
// 这是这类集成里最经典、后果也最彻底的一个错误。
//
// # 支持的算法
//
// RS256 / RS384 / RS512 与 ES256 —— 覆盖飞书、Entra ID、Google、Okta、Keycloak。
// **不支持 HS256**：对称算法在 OIDC 里要用 client_secret 当密钥，
// 而 client_secret 常被配错成公开值；更要命的是「alg=none」与算法混淆攻击
// 都是从支持对称算法开始的。直接不支持，这类问题就不存在。

var (
	ErrNoKID          = errors.New("idp: id_token 头里没有 kid，无法定位验签公钥")
	ErrKeyNotFound    = errors.New("idp: JWKS 里没有匹配的公钥")
	ErrBadSignature   = errors.New("idp: id_token 签名校验失败")
	ErrUnsupportedAlg = errors.New("idp: 不支持的签名算法")
)

// JWK 一把公钥。
type JWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"` // RSA modulus
	E   string `json:"e"` // RSA exponent
	Crv string `json:"crv"`
	X   string `json:"x"` // EC
	Y   string `json:"y"`
}

// JWKS 一组公钥。
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// ParseJWKS 解析 JWKS 文档。
func ParseJWKS(b []byte) (JWKS, error) {
	var j JWKS
	if err := json.Unmarshal(b, &j); err != nil {
		return JWKS{}, fmt.Errorf("idp: JWKS 不是合法 JSON: %w", err)
	}
	if len(j.Keys) == 0 {
		return JWKS{}, errors.New("idp: JWKS 里没有任何公钥")
	}
	return j, nil
}

// PublicKey 把 JWK 转成 Go 的公钥。
func (k JWK) PublicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		n, err := b64uint(k.N)
		if err != nil {
			return nil, fmt.Errorf("idp: RSA modulus 不合法: %w", err)
		}
		e, err := b64uint(k.E)
		if err != nil {
			return nil, fmt.Errorf("idp: RSA exponent 不合法: %w", err)
		}
		if !e.IsInt64() || e.Int64() > 1<<31 {
			return nil, errors.New("idp: RSA exponent 超出范围")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	case "EC":
		if k.Crv != "P-256" {
			return nil, fmt.Errorf("%w: EC 曲线 %q", ErrUnsupportedAlg, k.Crv)
		}
		x, err := b64uint(k.X)
		if err != nil {
			return nil, err
		}
		y, err := b64uint(k.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
	}
	return nil, fmt.Errorf("%w: 密钥类型 %q", ErrUnsupportedAlg, k.Kty)
}

func b64uint(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b), nil
}

// jwtHeader id_token 的头部。
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// VerifySignature 用 JWKS 验证 id_token 的签名。
//
// **必须在 ParseClaims 之前调用**。顺序反了就等于先信任、后验证。
func VerifySignature(idToken string, jwks JWKS) error {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return errors.New("idp: id_token 格式不对")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("idp: id_token 头不是合法 base64url: %w", err)
	}
	var h jwtHeader
	if err := json.Unmarshal(hb, &h); err != nil {
		return fmt.Errorf("idp: id_token 头不是合法 JSON: %w", err)
	}

	// alg=none 是 JWT 规范里最著名的坑：声称"我没签名"，
	// 而幼稚的实现会照单全收。这里连带任何未知算法一起拒绝。
	switch h.Alg {
	case "RS256", "RS384", "RS512", "ES256":
	default:
		return fmt.Errorf("%w: %q（不支持对称算法与 none）", ErrUnsupportedAlg, h.Alg)
	}

	key, err := pickKey(jwks, h.Kid)
	if err != nil {
		return err
	}
	pub, err := key.PublicKey()
	if err != nil {
		return err
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("idp: 签名不是合法 base64url: %w", err)
	}
	signed := []byte(parts[0] + "." + parts[1])

	switch h.Alg {
	case "RS256", "RS384", "RS512":
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: 算法与密钥类型不符", ErrUnsupportedAlg)
		}
		hashed, cryptoHash := digest(h.Alg, signed)
		if err := rsa.VerifyPKCS1v15(rsaPub, cryptoHash, hashed, sig); err != nil {
			return ErrBadSignature
		}
	case "ES256":
		ecPub, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: 算法与密钥类型不符", ErrUnsupportedAlg)
		}
		if len(sig) != 64 {
			return ErrBadSignature
		}
		sum := sha256.Sum256(signed)
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(ecPub, sum[:], r, s) {
			return ErrBadSignature
		}
	}
	return nil
}

// pickKey 按 kid 选公钥。
//
// JWKS 里只有一把且 token 没带 kid 时，用那一把 —— 有些自建 IdP 不发 kid。
// 但多把密钥时**必须**有 kid：猜错一把就是把伪造 token 放进来。
func pickKey(jwks JWKS, kid string) (JWK, error) {
	if kid == "" {
		if len(jwks.Keys) == 1 {
			return jwks.Keys[0], nil
		}
		return JWK{}, ErrNoKID
	}
	for _, k := range jwks.Keys {
		if k.Kid == kid {
			return k, nil
		}
	}
	return JWK{}, fmt.Errorf("%w: kid=%s", ErrKeyNotFound, kid)
}

func digest(alg string, b []byte) ([]byte, crypto.Hash) {
	switch alg {
	case "RS384":
		h := crypto.SHA384.New()
		h.Write(b)
		return h.Sum(nil), crypto.SHA384
	case "RS512":
		h := crypto.SHA512.New()
		h.Write(b)
		return h.Sum(nil), crypto.SHA512
	default:
		sum := sha256.Sum256(b)
		return sum[:], crypto.SHA256
	}
}

// ── JWKS 缓存 ────────────────────────────────────────────────────

// KeyCache 按 URL 缓存 JWKS。
//
// # 为什么要缓存
//
// 每次登录都拉一次 JWKS，会让上游的一次抖动变成"全公司登不上"。
//
// # 为什么 kid 未命中时要强制刷新
//
// IdP 轮换密钥时会先发新 kid、再撤旧 kid。缓存里没有新 kid 就直接拒绝的话，
// 密钥轮换当天所有人都登不上，而这通常发生在半夜且没有任何预告。
// 所以：未命中 → 立刻刷一次（带最小间隔，防止被伪造 kid 打成 DDoS）。
type KeyCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	TTL     time.Duration
	MinGap  time.Duration // 两次强制刷新之间的最小间隔
}

type cacheEntry struct {
	jwks       JWKS
	fetchedAt  time.Time
	lastForced time.Time
}

func NewKeyCache() *KeyCache {
	return &KeyCache{entries: map[string]cacheEntry{}, TTL: time.Hour, MinGap: time.Minute}
}

// Get 取缓存。第二个返回值表示是否需要（重新）拉取。
func (c *KeyCache) Get(url string, now time.Time) (JWKS, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[url]
	if !ok || now.Sub(e.fetchedAt) > c.TTL {
		return JWKS{}, true
	}
	return e.jwks, false
}

// Put 放入缓存。
func (c *KeyCache) Put(url string, jwks JWKS, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[url]
	e.jwks, e.fetchedAt = jwks, now
	c.entries[url] = e
}

// AllowForceRefresh kid 未命中时是否允许立刻刷新。
//
// 带最小间隔：否则攻击者用随机 kid 疯狂请求，就能把我们变成打上游 IdP 的放大器。
func (c *KeyCache) AllowForceRefresh(url string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[url]
	if now.Sub(e.lastForced) < c.MinGap {
		return false
	}
	e.lastForced = now
	c.entries[url] = e
	return true
}

// 供测试构造 RSA 的 e 字段
func encodeUint(v uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	i := 0
	for i < 7 && b[i] == 0 {
		i++
	}
	return base64.RawURLEncoding.EncodeToString(b[i:])
}
