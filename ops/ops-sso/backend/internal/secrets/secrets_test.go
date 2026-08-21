package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

func newBox(t *testing.T) *Box {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	b, err := New(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpenRoundTrip(t *testing.T) {
	b := newBox(t)
	plain := []byte("probe-account-password-中文也要能过")

	ct, err := b.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, plain) {
		t.Fatal("密文里出现了明文 —— 这就不叫加密了")
	}

	got, err := b.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("解出来对不上：%q", got)
	}
}

// 同一份明文两次加密必须得到不同密文。
//
// 相同 → 说明 nonce 复用了。GCM 下 nonce 复用是灾难性的：
// 攻击者能异或两段密文还原出明文差异，甚至恢复认证密钥。
func TestNonceIsNotReused(t *testing.T) {
	b := newBox(t)
	plain := []byte("same-secret")

	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		ct, err := b.Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		k := hex.EncodeToString(ct)
		if seen[k] {
			t.Fatal("同一明文加出了相同密文 —— nonce 被复用了")
		}
		seen[k] = true
	}
}

// 密文被改一个 bit 就必须解不开。
// AES-GCM 自带认证，这条是它与裸 AES-CTR 的关键区别 ——
// 没有认证的话，攻击者可以翻转密文的位来定向修改明文。
func TestTamperedCiphertextRejected(t *testing.T) {
	b := newBox(t)
	ct, _ := b.Seal([]byte("secret"))

	for _, i := range []int{0, len(ct) / 2, len(ct) - 1} {
		bad := append([]byte(nil), ct...)
		bad[i] ^= 0x01
		if _, err := b.Open(bad); err == nil {
			t.Fatalf("第 %d 字节被改后仍能解开 —— 认证失效", i)
		}
	}
}

// 换一把密钥就解不开：数据库备份泄露时，没有密钥的人拿不到凭据
func TestWrongKeyCannotOpen(t *testing.T) {
	a, b := newBox(t), newBox(t)
	ct, _ := a.Seal([]byte("secret"))

	if _, err := b.Open(ct); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("换密钥应解不开且报密文错误，得到 %v", err)
	}
}

func TestTruncatedCiphertext(t *testing.T) {
	b := newBox(t)
	if _, err := b.Open([]byte{1, 2, 3}); !errors.Is(err, ErrCiphertext) {
		t.Fatal("过短的密文应报错而不是 panic")
	}
	if _, err := b.Open(nil); !errors.Is(err, ErrCiphertext) {
		t.Fatal("空密文应报错")
	}
}

// ★ 密钥缺失/格式不对时必须**建不出来**，
// 而不是退化成明文存储 —— 静默降级会让人以为自己加密了
func TestBadKeyRefuses(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrNoKey) {
		t.Fatalf("空密钥应报 ErrNoKey，得到 %v", err)
	}
	for _, bad := range []string{
		"tooshort",
		hex.EncodeToString(make([]byte, 16)),        // 16 字节，不是 32
		hex.EncodeToString(make([]byte, 64)),        // 64 字节
		"zz" + hex.EncodeToString(make([]byte, 31)), // 非十六进制
	} {
		if _, err := New(bad); !errors.Is(err, ErrBadKey) {
			t.Errorf("密钥 %q 应被拒，得到 %v", bad[:min(12, len(bad))], err)
		}
	}
}

func TestEmptyPlaintext(t *testing.T) {
	b := newBox(t)
	ct, err := b.Seal(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("空明文应解出空，得到 %q", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
