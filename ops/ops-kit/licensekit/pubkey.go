package licensekit

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// 验签公钥。**只有公钥进代码**，私钥由签发方离线保管：
//
//	~/vscode/license-authority/keys/{dev,prod}.private.key   （0600，目录 0700）
//
// 那个目录不在任何 git 仓库内 —— 比任何 .gitignore 都可靠。
//
// dev 与 prod 是两对完全独立的密钥：
//   - 开发测试签的 license 装不进生产环境（默认只信 prod 公钥）
//   - 万一 dev 私钥泄露，也解锁不了任何一套客户环境
//
// 这一点是从别处的教训来的：dev key 一直没换、生产也在用同一把，
// 等于所有环境共享一个信任根。这里从第一天就分开。
const (
	pubKeyDev  = "VDj9KugLMWjykR/OlLyYUvMYTtvBPLiTKIGxaXnDvl0="
	pubKeyProd = "rRpseWa1gSGGP0Nf0o/6RY0run38Iu6OBgBP2V2ksjM="
)

// EnvVar 控制用哪把公钥验签。留空或任何非 "dev" 的值都按 prod 处理 ——
// 安全默认：必须显式声明才会接受开发密钥签的 license。
const EnvVar = "OPS_LICENSE_ENV"

// KeyEnv 返回当前生效的密钥环境（"dev" 或 "prod"）。
func KeyEnv() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(EnvVar)), "dev") {
		return "dev"
	}
	return "prod"
}

// PublicKey 返回当前环境的验签公钥。
func PublicKey() (ed25519.PublicKey, error) {
	return PublicKeyFor(KeyEnv())
}

// PublicKeyFor 取指定环境的内嵌公钥。
//
// 存在的理由是签发台：它要逐条校验台账里的 license，而每条各自签于 dev 或 prod，
// 不能都用当前进程的环境变量去验。
//
// **必须用产品内嵌的这两个常量**，不能改成读文件——
// 签发台验签的意义就是回答"客户的产品会不会认这张"，
// 用别处的公钥验过，只证明了另一件事。
//
// 公钥公开无妨（柯克霍夫原则），导出它不降低任何安全性。
func PublicKeyFor(env string) (ed25519.PublicKey, error) {
	enc := pubKeyProd
	if strings.EqualFold(strings.TrimSpace(env), "dev") {
		enc = pubKeyDev
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法解码", ErrNoPublicKey)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: 长度异常 %d", ErrNoPublicKey, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}
