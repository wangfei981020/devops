// Package idp 是上游身份源接入：OIDC 授权码流程（PKCE）与身份映射。
//
// # 定位：我们不做第二个 IdP
//
// 企业通常已经有飞书 / Entra ID / Google。关口的价值在**执行面与证据面**，
// 不在于再建一套用户目录。所以这个包只做三件事：
//
//  1. 把人送去上游登录，再把回调换成 id_token
//  2. 按配置把 claim 映射成本地身份（员工号、部门、组）
//  3. 首次登录时按策略决定「自动建人」还是「拒绝」
//
// # JIT 建人默认关闭
//
// 上游能登录 ≠ 该给他访问权。JIT 开着的时候，上游目录里的每个人
// （含外包、离职未清理、测试账号）第一次点进来就会在这边长出一个账号。
// 这在按席位计费的系统里会把 license 灌爆，在授权上则等于默默扩大了主体集合。
package idp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config 一个上游身份源。
type Config struct {
	ID       int64
	Name     string // 显示在登录页按钮上
	Issuer   string
	ClientID string
	// ClientSecret 加密存储，只在换 token 的那一刻解开
	AuthURL     string
	TokenURL    string
	JWKSURL     string
	Scopes      []string
	RedirectURI string

	// ── 身份映射 ──
	// 哪个 claim 是稳定标识。**不要用 email** —— 人改了邮箱就变成另一个人，
	// 历史审计全部对不上。飞书用 employee_id / union_id，Entra 用 oid。
	SubjectClaim string
	NameClaim    string
	EmailClaim   string
	GroupsClaim  string
	DeptClaim    string

	// JITCreate 首次登录自动建人。默认 false。
	JITCreate bool
	// JITDefaultGroups 自动建人时落到哪些用户组（空 = 不进任何组 = 默认拒绝）
	JITDefaultGroups []int64

	Enabled bool
}

var (
	ErrStateMismatch  = errors.New("idp: state 不匹配（可能是 CSRF，或用户开了两个登录页）")
	ErrStateExpired   = errors.New("idp: 登录流程已超时，请重新开始")
	ErrNoSubject      = errors.New("idp: 上游没有返回稳定标识，无法确定这是谁")
	ErrJITDisabled    = errors.New("idp: 该用户在本系统中不存在，且未开启自动建人")
	ErrIssuerMismatch = errors.New("idp: id_token 的签发方与配置不一致")
	ErrTokenExpired   = errors.New("idp: id_token 已过期")
	ErrNonceMismatch  = errors.New("idp: nonce 不匹配（可能是重放）")
)

// AuthState 一次登录尝试的中间状态。
//
// 存服务端（Redis / 库），不放 cookie —— 放 cookie 就得防篡改，
// 而防篡改要签名，签名要密钥轮换，绕一大圈还不如直接存。
type AuthState struct {
	State        string
	Nonce        string
	CodeVerifier string
	ConfigID     int64
	Next         string // 登录后跳回哪
	CreatedAt    time.Time
}

// StateTTL 登录流程的有效期。
//
// 10 分钟：够一个人扫码 + 输验证码，又不至于让一个被截获的 state 长期可用。
const StateTTL = 10 * time.Minute

// NewAuthState 生成 state / nonce / PKCE verifier。
//
// PKCE 即使在有 client_secret 的服务端流程里也要做：授权码被中间环节
// （日志、代理、浏览器历史）泄露时，没有 verifier 就换不到 token。
func NewAuthState(cfgID int64, next string) (AuthState, error) {
	s, err := randB64(32)
	if err != nil {
		return AuthState{}, err
	}
	n, err := randB64(32)
	if err != nil {
		return AuthState{}, err
	}
	v, err := randB64(48)
	if err != nil {
		return AuthState{}, err
	}
	return AuthState{State: s, Nonce: n, CodeVerifier: v, ConfigID: cfgID, Next: next, CreatedAt: time.Now()}, nil
}

// Expired state 是否已超时。
func (a AuthState) Expired(now time.Time) bool {
	return now.After(a.CreatedAt.Add(StateTTL))
}

// CodeChallenge PKCE 的 S256 挑战值。
func (a AuthState) CodeChallenge() string {
	sum := sha256.Sum256([]byte(a.CodeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthorizeURL 拼出把人送去上游的地址。
func (c Config) AuthorizeURL(st AuthState) string {
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	q := []string{
		"response_type=code",
		"client_id=" + urlEncode(c.ClientID),
		"redirect_uri=" + urlEncode(c.RedirectURI),
		"scope=" + urlEncode(strings.Join(scopes, " ")),
		"state=" + urlEncode(st.State),
		"nonce=" + urlEncode(st.Nonce),
		"code_challenge=" + urlEncode(st.CodeChallenge()),
		"code_challenge_method=S256",
	}
	sep := "?"
	if strings.Contains(c.AuthURL, "?") {
		sep = "&"
	}
	return c.AuthURL + sep + strings.Join(q, "&")
}

// Claims id_token 里我们关心的部分。
type Claims struct {
	Issuer   string
	Subject  string
	Audience []string
	Expiry   time.Time
	Nonce    string
	Raw      map[string]any
}

// Identity 映射之后的本地身份。
type Identity struct {
	ExternalID  string
	Username    string
	DisplayName string
	Email       string
	Groups      []string
	DeptPath    string
}

// ParseClaims 解开 id_token 的载荷部分。
//
// ⚠️ 这个函数**不验签**。验签需要 JWKS 与网络，由调用方在拿到密钥后完成；
// 分开是为了让映射逻辑能被穷举测试，而不是为了省事。
// 调用方必须先验签再调它 —— 顺序反了就等于信任任何人伪造的 token。
func ParseClaims(idToken string) (Claims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("idp: id_token 格式不对")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("idp: id_token 载荷不是合法 base64url: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Claims{}, fmt.Errorf("idp: id_token 载荷不是合法 JSON: %w", err)
	}
	c := Claims{Raw: raw}
	c.Issuer, _ = raw["iss"].(string)
	c.Subject, _ = raw["sub"].(string)
	c.Nonce, _ = raw["nonce"].(string)
	switch aud := raw["aud"].(type) {
	case string:
		c.Audience = []string{aud}
	case []any:
		for _, a := range aud {
			if s, ok := a.(string); ok {
				c.Audience = append(c.Audience, s)
			}
		}
	}
	if exp, ok := raw["exp"].(float64); ok {
		c.Expiry = time.Unix(int64(exp), 0)
	}
	return c, nil
}

// Validate 校验 claim 与配置、与本次流程是否对得上。
//
// 三条都不能省：
//   - issuer 不符 → 别人的 token 拿来登我们的系统
//   - audience 不含我们的 client_id → 给别的应用签的 token 被拿来复用
//   - nonce 不符 → 重放
func (c Config) Validate(cl Claims, st AuthState, now time.Time) error {
	if c.Issuer != "" && cl.Issuer != c.Issuer {
		return fmt.Errorf("%w: 期望 %q，得到 %q", ErrIssuerMismatch, c.Issuer, cl.Issuer)
	}
	var audOK bool
	for _, a := range cl.Audience {
		if a == c.ClientID {
			audOK = true
		}
	}
	if !audOK {
		return fmt.Errorf("%w: audience 里没有本客户端", ErrIssuerMismatch)
	}
	if !cl.Expiry.IsZero() && now.After(cl.Expiry) {
		return ErrTokenExpired
	}
	if st.Nonce != "" && cl.Nonce != st.Nonce {
		return ErrNonceMismatch
	}
	return nil
}

// MapIdentity 按配置把 claim 映射成本地身份。
func (c Config) MapIdentity(cl Claims) (Identity, error) {
	sub := c.SubjectClaim
	if sub == "" {
		sub = "sub"
	}
	ext := claimString(cl.Raw, sub)
	if ext == "" {
		// 拿不到稳定标识就**停下来**，不要退而求其次用 email。
		// 用 email 顶替的后果是：人改了邮箱 = 变成另一个人，
		// 旧账号的权限还在、审计对不上，而且没有任何报错。
		return Identity{}, fmt.Errorf("%w: claim %q 为空", ErrNoSubject, sub)
	}
	id := Identity{
		ExternalID:  ext,
		Username:    ext,
		DisplayName: claimString(cl.Raw, orDefault(c.NameClaim, "name")),
		Email:       claimString(cl.Raw, orDefault(c.EmailClaim, "email")),
		DeptPath:    claimString(cl.Raw, c.DeptClaim),
	}
	if c.GroupsClaim != "" {
		id.Groups = claimStrings(cl.Raw, c.GroupsClaim)
	}
	if id.DisplayName == "" {
		id.DisplayName = ext
	}
	return id, nil
}

func claimString(raw map[string]any, key string) string {
	if key == "" {
		return ""
	}
	// 支持一层嵌套：user.employee_id
	if i := strings.Index(key, "."); i > 0 {
		if sub, ok := raw[key[:i]].(map[string]any); ok {
			return claimString(sub, key[i+1:])
		}
		return ""
	}
	switch v := raw[key].(type) {
	case string:
		return v
	case float64:
		// 有的 IdP 把员工号当数字发。转成字符串时不能带小数点，
		// 否则 "10482" 会变成 "10482.000000"，之后每次匹配都对不上。
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

func claimStrings(raw map[string]any, key string) []string {
	switch v := raw[key].(type) {
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	}
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func randB64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func urlEncode(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~':
			b.WriteByte(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}
