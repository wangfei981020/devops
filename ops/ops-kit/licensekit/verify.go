package licensekit

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 令牌格式（紧凑，类 JWT 但没有 header —— 算法不给选，就是 Ed25519）：
//
//	base64url(payload_json) "." base64url(ed25519_signature)
//
// 不用 JWT 的原因：JWT 的 alg 字段是个历史包袱，alg=none 与算法混淆漏洞
// 都出在"让令牌自己声明怎么验"。这里算法写死在代码里，令牌无从置喙。

var (
	ErrEmptyToken   = errors.New("licensekit: 激活码为空")
	ErrMalformed    = errors.New("licensekit: 激活码格式不正确")
	ErrBadSignature = errors.New("licensekit: 激活码签名无效，可能被篡改或不是本厂商签发")
	ErrNoPublicKey  = errors.New("licensekit: 内嵌验签公钥不可用")
)

// Verify 用给定公钥校验令牌并返回 Payload。
//
// ⚠️ **这里只判签名，不判有效期。** 过期与否交给 Manager.evaluate ——
// 因为过期不等于失效：还有 30 天宽限期，宽限期内功能照常。
// 如果在这里因过期而拒绝，客户续签晚了一天系统就罢工，与「过期不停服」冲突。
//
// 同理也不判指纹：指纹不匹配有 14 天宽限，是状态而不是错误。
func Verify(token string, pub ed25519.PublicKey) (*Payload, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrEmptyToken
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, ErrNoPublicKey
	}

	body, sig, err := splitToken(token)
	if err != nil {
		return nil, err
	}
	// 先验签再解析：未经验证的 JSON 不值得信任，也不该喂给解析器。
	if !ed25519.Verify(pub, body, sig) {
		return nil, ErrBadSignature
	}

	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: 载荷不是合法 JSON", ErrMalformed)
	}
	return &p, nil
}

// VerifyEmbedded 用内嵌公钥校验，是产品侧的常规入口。
func VerifyEmbedded(token string) (*Payload, error) {
	pub, err := PublicKey()
	if err != nil {
		return nil, err
	}
	return Verify(token, pub)
}

func splitToken(token string) (body, sig []byte, err error) {
	// 去掉换行与空格：客户往往从邮件里复制，会带上换行。
	// 与其让他"格式不正确"来回问，不如这里容忍。
	token = strings.Join(strings.Fields(token), "")

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("%w: 应为两段，实际 %d 段", ErrMalformed, len(parts))
	}
	body, err = base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 载荷段无法解码", ErrMalformed)
	}
	sig, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 签名段无法解码", ErrMalformed)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, nil, fmt.Errorf("%w: 签名长度异常", ErrMalformed)
	}
	return body, sig, nil
}

// Encode 把 Payload 与签名拼成令牌。签发工具用，产品侧不会调到。
func Encode(body, sig []byte) string {
	return base64.RawURLEncoding.EncodeToString(body) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
}

// Inspect 只解码不验签，用于排查（比如客户发来一段激活码问为什么装不上）。
//
// ⚠️ 返回的内容**未经可信性确认**，绝不能用它做任何门控判断。
func Inspect(token string) (*Payload, error) {
	body, _, err := splitToken(strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}
	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: 载荷不是合法 JSON", ErrMalformed)
	}
	return &p, nil
}
