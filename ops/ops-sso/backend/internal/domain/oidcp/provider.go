// Package oidcp 是 OpenID Provider：让下游应用用标准 OIDC 接进关口。
//
// # 方向（很容易搞反）
//
//	idp 包    我们是 RP：去连飞书 / Entra ID 拿身份
//	oidcp 包  我们是 OP：CMDB、告警平台等来连我们
//
// 两个方向都有，SSO 才闭环：员工用飞书登进关口，关口再签发身份给下游。
//
// # 与普通 IdP 的关键差别
//
// authorize 时**先过访问判定**：没被授权用这个应用的人，走到这一步就被拒，
// 拿不到 code。普通 IdP 只回答"你是谁"，下游拿到 token 后还得自己判权限 ——
// 那正是"每个系统各写一套权限、各写错一遍"的由来。
package oidcp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var (
	ErrUnknownClient   = errors.New("oidcp: 未知的 client_id")
	ErrBadRedirectURI  = errors.New("oidcp: redirect_uri 不在白名单里")
	ErrBadSecret       = errors.New("oidcp: client_secret 不对")
	ErrCodeInvalid     = errors.New("oidcp: 授权码无效")
	ErrCodeUsed        = errors.New("oidcp: 授权码已被使用")
	ErrCodeExpired     = errors.New("oidcp: 授权码已过期")
	ErrPKCERequired    = errors.New("oidcp: 该客户端必须使用 PKCE")
	ErrPKCEMismatch    = errors.New("oidcp: code_verifier 校验失败")
	ErrAccessDenied    = errors.New("oidcp: 该用户未被授权访问此应用")
	ErrUnsupportedResp = errors.New("oidcp: 只支持 response_type=code")
)

// CodeTTL 授权码有效期。
//
// 60 秒：换 token 是服务端到服务端的一次调用，正常不到 1 秒。
// 给再长只是延长了"code 在浏览器历史 / 日志 / Referer 里可被截获"的窗口。
const CodeTTL = 60 * time.Second

// Client 一个下游客户端。
type Client struct {
	ID           int64
	TenantID     int64
	AppID        int64
	ClientID     string
	RedirectURIs []string
	PostLogout   []string
	Scopes       []string
	PublicClient bool
	RequirePKCE  bool
	IDTokenTTL   time.Duration
	Claims       []string
	Enabled      bool
}

// ValidateRedirectURI 精确匹配回调地址。
//
// **不做前缀匹配、不做通配** —— 这是 OIDC 最经典的一类漏洞：
// 允许 `https://app.example.com/*` 之后，攻击者可以构造出把 code
// 送到自己手上的地址，而整个流程看起来完全正常。
//
// 代价是客户端换一个回调路径就要改配置。这个代价值得付。
func (c Client) ValidateRedirectURI(uri string) error {
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrBadRedirectURI, uri)
}

// ValidatePostLogout 校验登出后的跳转地址，同样精确匹配。
func (c Client) ValidatePostLogout(uri string) bool {
	if uri == "" {
		return true
	}
	for _, allowed := range c.PostLogout {
		if allowed == uri {
			return true
		}
	}
	return false
}

// ── PKCE ────────────────────────────────────────────────────────

// VerifyPKCE 校验 code_verifier。
//
// 只支持 S256，不支持 plain：plain 模式下 challenge 就是 verifier 本身，
// 截获授权请求的人直接就拿到了 verifier —— 等于没做 PKCE。
func VerifyPKCE(challenge, method, verifier string) error {
	if challenge == "" {
		return nil // 该客户端不要求 PKCE，由调用方决定是否放行
	}
	if method != "S256" {
		return fmt.Errorf("%w: 只支持 S256（plain 等于没做 PKCE）", ErrPKCEMismatch)
	}
	if verifier == "" {
		return fmt.Errorf("%w: 缺少 code_verifier", ErrPKCEMismatch)
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		return ErrPKCEMismatch
	}
	return nil
}

// ── 授权码 ──────────────────────────────────────────────────────

// AuthCode 一张待兑换的授权码。
type AuthCode struct {
	Code        string // 明文只在签发那一刻存在
	Hash        string
	ClientID    string
	UserID      int64
	TenantID    int64
	RedirectURI string
	Nonce       string
	Scope       string
	Challenge   string
	Method      string
	AuthTime    time.Time
	SessionID   int64
	ExpiresAt   time.Time
}

// NewAuthCode 生成一张授权码。
func NewAuthCode(clientID string, userID, tenantID, sessionID int64,
	redirectURI, nonce, scope, challenge, method string, authTime, now time.Time) (AuthCode, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return AuthCode{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	return AuthCode{
		Code: code, Hash: HashCode(code),
		ClientID: clientID, UserID: userID, TenantID: tenantID, SessionID: sessionID,
		RedirectURI: redirectURI, Nonce: nonce, Scope: scope,
		Challenge: challenge, Method: method,
		AuthTime: authTime, ExpiresAt: now.Add(CodeTTL),
	}, nil
}

// HashCode 授权码只存哈希 —— 库泄露时未用的 code 也不能被拿去换 token。
func HashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// ── id_token ────────────────────────────────────────────────────

// Subject 签发 token 时需要的用户信息。
type Subject struct {
	UserID      int64
	Username    string
	DisplayName string
	Email       string
	EmployeeID  string
	Groups      []string
	// AuthSource 他是怎么认证进来的：local / oidc / break_glass。
	// **必须下发**：下游要能知道"这个会话是走应急通道进来的"，
	// 从而对高危操作再加一道自己的确认。
	AuthSource string
}

// IDTokenClaims 组装 id_token 的载荷。
//
// claims 白名单由客户端配置决定 —— 默认不给手机号、成本中心等，
// 应用要不到就泄不了。
func IDTokenClaims(c Client, s Subject, issuer, nonce, sid string, authTime, now time.Time) map[string]any {
	cl := map[string]any{
		"iss":       issuer,
		"sub":       fmt.Sprintf("%d", s.UserID),
		"aud":       c.ClientID,
		"iat":       now.Unix(),
		"exp":       now.Add(c.IDTokenTTL).Unix(),
		"auth_time": authTime.Unix(),
		// sid 让全局单点登出成为可能：登出时按 sid 通知每个下游
		"sid": sid,
		// 走的哪条认证路径。应急通道进来的会话，下游应当更谨慎
		"amr": []string{s.AuthSource},
	}
	if nonce != "" {
		cl["nonce"] = nonce
	}
	allowed := map[string]bool{}
	for _, k := range c.Claims {
		allowed[strings.TrimSpace(k)] = true
	}
	if allowed["name"] && s.DisplayName != "" {
		cl["name"] = s.DisplayName
	}
	if allowed["email"] && s.Email != "" {
		cl["email"] = s.Email
	}
	if allowed["employee_id"] && s.EmployeeID != "" {
		cl["employee_id"] = s.EmployeeID
	}
	if allowed["groups"] && len(s.Groups) > 0 {
		cl["groups"] = s.Groups
	}
	if allowed["preferred_username"] || allowed["sub"] {
		cl["preferred_username"] = s.Username
	}
	return cl
}

// SigningKey 一把签名密钥。
type SigningKey struct {
	Kid     string
	Alg     string
	Private *rsa.PrivateKey
	Active  bool
}

// NewSigningKey 生成一把 2048 位 RSA 密钥。
//
// 用 RSA 而不是 EdDSA：RS256 是 OIDC 生态里唯一被所有客户端库支持的算法。
// 我们的目标是让客户拿任何一个现成库都能接上，不是展示我们会用新算法。
func NewSigningKey() (SigningKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return SigningKey{}, err
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return SigningKey{}, err
	}
	return SigningKey{Kid: "k-" + hex.EncodeToString(b), Alg: "RS256", Private: priv, Active: true}, nil
}

// PublicJWK 转成对外发布的 JWK。
func (k SigningKey) PublicJWK() map[string]any {
	pub := k.Private.Public().(*rsa.PublicKey)
	return map[string]any{
		"kty": "RSA",
		"use": "sig",
		"alg": k.Alg,
		"kid": k.Kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// SignIDToken 用 RS256 签出一个 JWT。
func SignIDToken(k SigningKey, claims map[string]any) (string, error) {
	hdr, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": k.Kid})
	if err != nil {
		return "", err
	}
	pl, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." +
		base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.Private, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// NewSID 生成会话标识。
func NewSID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ── Discovery ───────────────────────────────────────────────────

// Discovery 生成 `/.well-known/openid-configuration` 的内容。
//
// 只声明**真正实现了**的能力。声明了没做的，客户端库会按声明去调，
// 然后拿到 404 —— 而对方的第一反应是"我配错了"，排查方向从一开始就是错的。
func Discovery(issuer string) map[string]any {
	return map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oidc/authorize",
		"token_endpoint":                        issuer + "/oidc/token",
		"userinfo_endpoint":                     issuer + "/oidc/userinfo",
		"jwks_uri":                              issuer + "/oidc/jwks",
		"end_session_endpoint":                  issuer + "/oidc/logout",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "auth_time", "nonce", "sid", "amr",
			"name", "email", "groups", "employee_id", "preferred_username",
		},
		// 明确声明不支持隐式流与混合流：那两种会把 token 直接放进 URL 片段，
		// 在日志、Referer、浏览器历史里到处留痕
		"response_modes_supported": []string{"query"},
	}
}
