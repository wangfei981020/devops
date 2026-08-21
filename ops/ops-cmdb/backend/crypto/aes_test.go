package crypto

import (
	"encoding/base64"
	"strings"
	"sync"
	"testing"
)

// 这个包管的是云账号 SA key、ACME 账户私钥、证书私钥的加密存储。
// 一个 bug 的后果是两个方向的灾难：
//   - 解不开 → 客户的凭据永久丢失，且备份也是密文，救不回来
//   - 不该解开的解开了 → 凭据泄露
// 所以这里测的重点不是"能跑"，是这两个方向的边界。

const testKey = "test-master-key-do-not-use-in-prod"

func newCipher(t *testing.T, key string) *Cipher {
	t.Helper()
	c, err := New(key)
	if err != nil {
		t.Fatalf("New(%q) 失败: %v", key, err)
	}
	return c
}

// ── 基本往返 ────────────────────────────────────────────────────

func TestRoundTrip(t *testing.T) {
	c := newCipher(t, testKey)
	cases := []struct {
		name  string
		plain string
	}{
		{"普通字符串", "hello-credential"},
		{"中文", "证书私钥内容"},
		{"JSON（云账号 SA key 的真实形态）", `{"type":"service_account","private_key":"-----BEGIN PRIVATE KEY-----\nMIIE"}`},
		{"含换行", "line1\nline2\r\nline3"},
		{"含 NUL 等二进制字节", "a\x00b\xff\xfec"},
		{"单字符", "x"},
		{"长文本（模拟 PEM 私钥）", strings.Repeat("MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ", 200)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := c.Encrypt(tc.plain)
			if err != nil {
				t.Fatalf("Encrypt 失败: %v", err)
			}
			if enc == tc.plain {
				t.Fatal("密文与明文相同 —— 根本没加密")
			}
			got, err := c.Decrypt(enc)
			if err != nil {
				t.Fatalf("Decrypt 失败: %v", err)
			}
			if got != tc.plain {
				t.Errorf("往返不一致\n得到 %q\n期望 %q", got, tc.plain)
			}
		})
	}
}

// 空明文加密后仍是有效密文，且能解回空串。
//
// ⚠️ 注意这与 Decrypt("") 的语义不同（见下一个测试）——
// 二者不对称，但业务上说得通：数据库里存空串表示"没配这项凭据"。
func TestEncryptEmptyString(t *testing.T) {
	c := newCipher(t, testKey)
	enc, err := c.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt(\"\") 失败: %v", err)
	}
	if enc == "" {
		t.Fatal("空明文的密文不该是空串 —— 那会与「未配置」混淆")
	}
	got, err := c.Decrypt(enc)
	if err != nil || got != "" {
		t.Errorf("Decrypt = (%q, %v), want (\"\", nil)", got, err)
	}
}

// ★ Decrypt("") 返回空串且不报错 —— 表示「这条记录没有加密内容」。
//
// 业务代码依赖这个行为：凭据字段为空时不该报解密失败。
// 改动它会让所有「未配置凭据」的记录开始报错，所以锁在这里。
func TestDecryptEmptyMeansNotConfigured(t *testing.T) {
	c := newCipher(t, testKey)
	got, err := c.Decrypt("")
	if err != nil {
		t.Errorf("Decrypt(\"\") 不该报错，它表示「未配置」，得到 %v", err)
	}
	if got != "" {
		t.Errorf("Decrypt(\"\") = %q, want \"\"", got)
	}
}

// ── 安全性质 ────────────────────────────────────────────────────

// ★ 同一明文两次加密必须产生不同密文（nonce 随机）。
//
// 如果 nonce 固定，相同凭据会产生相同密文 —— 攻击者拿到数据库就能看出
// 「这两个账号用了同一个密钥」，甚至能做频率分析。
func TestNonceIsRandom(t *testing.T) {
	c := newCipher(t, testKey)
	const plain = "same-secret-every-time"
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt 失败: %v", err)
		}
		if seen[enc] {
			t.Fatal("同一明文产生了重复密文 —— nonce 没有随机化")
		}
		seen[enc] = true
	}
}

// ★ 换一把密钥必须解不开。
//
// 这条保证了「拿到密文但没有主密钥 = 拿不到凭据」，
// 是整个加密存储的意义所在。
func TestWrongKeyCannotDecrypt(t *testing.T) {
	enc, err := newCipher(t, testKey).Encrypt("top-secret-credential")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	got, err := newCipher(t, "a-completely-different-key").Decrypt(enc)
	if err == nil {
		t.Fatalf("错误密钥竟然解开了，得到 %q", got)
	}
}

// ★ 篡改密文必须被发现（GCM 的认证特性）。
//
// 没有这一条，有数据库写权限的人可以翻转密文里的位来篡改凭据内容，
// 而系统会毫无察觉地拿着被改过的凭据去连云厂商。
func TestTamperedCiphertextIsRejected(t *testing.T) {
	c := newCipher(t, testKey)
	enc, err := c.Encrypt("credential-value")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("密文不是合法 base64: %v", err)
	}

	// 逐个位置翻转一位，每一处都必须被拒绝
	for i := range raw {
		mutated := make([]byte, len(raw))
		copy(mutated, raw)
		mutated[i] ^= 0x01
		if _, err := c.Decrypt(base64.StdEncoding.EncodeToString(mutated)); err == nil {
			t.Fatalf("第 %d 字节被篡改后仍解密成功 —— 认证失效", i)
		}
	}
}

// 截断的密文必须被拒绝，不能 panic。
func TestTruncatedCiphertext(t *testing.T) {
	c := newCipher(t, testKey)
	enc, _ := c.Encrypt("something")
	raw, _ := base64.StdEncoding.DecodeString(enc)

	for _, n := range []int{0, 1, 5, 11, 12, len(raw) - 1} {
		if n < 0 || n > len(raw) {
			continue
		}
		in := base64.StdEncoding.EncodeToString(raw[:n])
		if in == "" {
			continue // 空串是「未配置」，另有语义
		}
		if _, err := c.Decrypt(in); err == nil {
			t.Errorf("截断到 %d 字节仍解密成功", n)
		}
	}
}

// 非 base64 的输入必须报错，不能 panic。
func TestMalformedInput(t *testing.T) {
	c := newCipher(t, testKey)
	for _, in := range []string{
		"not base64 at all!!!",
		"###",
		"短",
		strings.Repeat("A", 3), // 合法 base64 字符但长度不足以含 nonce
	} {
		if _, err := c.Decrypt(in); err == nil {
			t.Errorf("Decrypt(%q) 应报错", in)
		}
	}
}

// ── 已知限制（用测试记录下来，防止误以为有保护）─────────────────

// ⚠️ 当前实现没有 AAD（附加认证数据），因此密文可以「移花接木」：
// 把 A 记录的凭据密文原样复制到 B 记录，解密照样成功。
//
// 影响有限 —— 能改数据库的人通常也能直接读密文。但如果将来要防内部篡改，
// 应当把记录标识（表名+主键）作为 AAD 绑定进去。
//
// 这条测试**记录现状**而不是主张它正确。真要修时，它会失败并提醒你：
// 改 AAD 会让所有存量密文解不开，必须配双读迁移。
func TestKnownLimitation_NoAADBinding(t *testing.T) {
	c := newCipher(t, testKey)
	enc, _ := c.Encrypt("account-A-credential")

	// 同一把主密钥下，密文脱离原记录上下文仍可解开
	got, err := c.Decrypt(enc)
	if err != nil || got != "account-A-credential" {
		t.Fatalf("前提不成立: got=%q err=%v", got, err)
	}
	t.Log("已知限制：无 AAD 绑定，密文可跨记录复制。要修需配双读迁移。")
}

// ⚠️ New 用 SHA-256 直接派生密钥，不是 KDF。
//
// 主密钥若是高熵随机串没问题；若是人选的弱口令，没有 PBKDF2/scrypt/argon2
// 的计算拉伸，离线爆破成本很低。
//
// 同样，改派生方式会让所有存量密文解不开 —— 这条测试锁住现状，
// 提醒任何想改的人：必须配版本前缀 + 双读迁移。
func TestKnownLimitation_KeyDerivationIsPlainSHA256(t *testing.T) {
	// 任意长度的 key 都能构造成功（因为先 SHA-256 到 32 字节）
	for _, k := range []string{"", "x", strings.Repeat("k", 1000)} {
		if _, err := New(k); err != nil {
			t.Errorf("New(len=%d) 失败: %v", len(k), err)
		}
	}
	t.Log("已知限制：SHA-256 直接派生，非 KDF。弱口令下爆破成本低。")
}

// ⚠️ 空主密钥也能构造成功 —— 这不该在生产发生。
// 配置层必须拒绝空主密钥；这里只记录加密层不做这个检查。
func TestEmptyMasterKeyIsAcceptedByCryptoLayer(t *testing.T) {
	c, err := New("")
	if err != nil {
		t.Fatalf("New(\"\") 失败: %v", err)
	}
	enc, err := c.Encrypt("data")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if got, _ := c.Decrypt(enc); got != "data" {
		t.Error("空主密钥下往返失败")
	}
	t.Log("加密层不校验主密钥非空 —— 该检查必须在配置加载处做")
}

// ── 并发 ────────────────────────────────────────────────────────

// Cipher 会被多个请求并发使用（每个 handler 都可能解凭据），
// cipher.AEAD 本身是并发安全的，这里锁住这个前提。
func TestConcurrentUse(t *testing.T) {
	c := newCipher(t, testKey)
	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plain := strings.Repeat("x", i%64+1)
			enc, err := c.Encrypt(plain)
			if err != nil {
				errs <- err
				return
			}
			got, err := c.Decrypt(enc)
			if err != nil {
				errs <- err
				return
			}
			if got != plain {
				errs <- errMismatch
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发使用出错: %v", err)
	}
}

var errMismatch = errConst("并发往返结果不一致")

type errConst string

func (e errConst) Error() string { return string(e) }
