// Package mfa 是二次验证：TOTP 与「提权票据」。
//
// # 为什么单独一个包
//
// 网关判定出 challenge 之后，需要一个东西证明「这个人刚刚验过了」。
// 那个东西不是会话（会话是 8 小时的），而是一张**短时提权票据**：
// 验一次，30 分钟内同类请求直接放行，到点自动失效。
//
// 把它和会话混在一起是常见错误 —— 混了之后「二次验证」的有效期就等于登录有效期，
// 等于只在登录时验了一次，白做。
package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Period TOTP 步长。30 秒是 RFC 6238 的默认值，也是所有验证器 App 的默认值 ——
// 改成别的会让用户的 Google Authenticator 永远对不上，而且没有任何提示。
const Period = 30

// Skew 允许的时间漂移窗口（前后各 1 步 = ±30 秒）。
//
// 不放宽到 ±2 步：每放宽一步，可用验证码数量就多一个，暴力破解的成本就低一截。
// 手机时间偏差超过 30 秒的用户应该去校时，而不是让所有人陪着降低安全性。
const Skew = 1

// NewSecret 生成一个 TOTP 密钥（20 字节，base32 无填充）。
func NewSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// ProvisioningURI 生成验证器 App 扫的二维码内容。
//
// issuer 同时出现在路径与参数里是 Google 的约定，少一个某些 App 就不显示组织名。
func ProvisioningURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", fmt.Sprint(Period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Code 算出某个时间点的验证码。
func Code(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("mfa: 密钥不是合法 base32: %w", err)
	}
	counter := uint64(t.Unix() / Period)

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(buf[:])
	sum := m.Sum(nil)

	// RFC 4226 的动态截断
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off]&0x7f) << 24) | (uint32(sum[off+1]) << 16) |
		(uint32(sum[off+2]) << 8) | uint32(sum[off+3])
	return fmt.Sprintf("%06d", v%1000000), nil
}

// Verify 校验验证码，允许 ±Skew 步的漂移。
//
// 用 subtle.ConstantTimeCompare 而不是 == ：字符串比较会在第一个不同的字符处返回，
// 逐位试探能把 6 位码的搜索空间从 100 万降到 60 次。
func Verify(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for i := -Skew; i <= Skew; i++ {
		want, err := Code(secret, now.Add(time.Duration(i*Period)*time.Second))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// ── 提权票据 ────────────────────────────────────────────────────

// Ticket 一次二次验证换来的短时通行证。
//
// Scope 决定这张票能用在哪：空 = 该应用的所有需验证操作；
// 也可以细到某个路径前缀 —— 用「删项目」验过的票不该同时能用来「删仓库」。
type Ticket struct {
	ID        int64
	UserID    int64
	AppID     int64
	Scope     string
	IssuedAt  time.Time
	ExpiresAt time.Time
	// TicketRef 绑定的工单号。策略要求带工单时，票据里必须记下是哪一张 ——
	// 事后审计要能回答「这次删除是依据哪个工单批的」
	TicketRef string
}

// Valid 票据在给定时刻是否有效。
func (t Ticket) Valid(now time.Time) bool {
	return now.After(t.IssuedAt.Add(-time.Second)) && now.Before(t.ExpiresAt)
}

// Covers 这张票能否用于某个请求路径。
//
// 前缀匹配，且必须在路径分隔处对齐：`/api/v2/projects` 的票不能用于
// `/api/v2/projects-secret` —— 那是两个不同的资源，只是名字像。
func (t Ticket) Covers(path string) bool {
	if t.Scope == "" {
		return true
	}
	if path == t.Scope {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(t.Scope, "/")+"/")
}
