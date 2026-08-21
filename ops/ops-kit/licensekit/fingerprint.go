package licensekit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// fingerprintKey 让安装指纹不可被外部预测：知道 install_uuid 也算不出指纹，
// 客户因此无法自行构造一个匹配他人 license 的指纹。
// 它编译进二进制，不是秘密级凭据，只是防止指纹被平凡地重放。
const fingerprintKey = "ops-kit.install.fingerprint.v1"

// Fingerprint 由安装 UUID 与系统标识派生出这套部署的稳定指纹。
//
// ⚠️ 两个输入都必须来自**数据库**，绝不能取宿主机 MAC / hostname / machine-id。
//
// 原因：产品在 K8s 里多副本运行时，每个 Pod 的宿主机标识都不同，用宿主机派生
// 会让绑定安装的 license 在部分副本上校验失败，表现为「重启后偶发提示未授权」——
// 这是最难排查的一类故障，因为它只在部分请求上出现。
//
// 存数据库等价于「绑定这套部署」，对客户也更合理：Pod 本来就会漂移。
//
// ⚠️ 不带产品参数：同一套环境装多个产品时指纹必须一致（同一个数据库实例），
// 否则一份 license 覆盖多产品的前提就不成立了。
func Fingerprint(installUUID string, systemIdentifier uint64) string {
	h := hmac.New(sha256.New, []byte(fingerprintKey))
	h.Write([]byte(installUUID))
	h.Write([]byte("|"))
	fmt.Fprintf(h, "%d", systemIdentifier)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// ShortFingerprint 截断显示用。界面上给客户看完整 32 位没有意义，
// 但要能和签发方核对，所以留头尾。
func ShortFingerprint(fp string) string {
	if len(fp) <= 12 {
		return fp
	}
	return fp[:8] + "…" + fp[len(fp)-4:]
}
