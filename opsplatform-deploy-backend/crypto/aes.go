package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"strings"
)

const prefix = "enc:"

var key []byte

// Init 接收 base64 编码的 key（解码后取前 32 字节；不足右侧 zero-pad，仅 dev fallback 用）
func Init(base64Key string) {
	if base64Key == "" {
		panic("crypto.Init: empty key")
	}
	k, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		panic("crypto.Init: key is not valid base64: " + err.Error())
	}
	if len(k) < 32 {
		padded := make([]byte, 32)
		copy(padded, k)
		k = padded
	}
	key = k[:32]
}

// Encrypt 返回 "enc:<base64(nonce|ciphertext)>"；空字符串原样返回
func Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if strings.HasPrefix(plain, prefix) {
		return plain, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt 解密；没有 enc: 前缀视为明文原样返回（兼容历史数据）
// ⚠️ 明文分支会打 warn log，提示管理员 re-save 那条数据让它被加密
func Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	if !strings.HasPrefix(enc, prefix) {
		log.Printf("[crypto] WARN: legacy plaintext token detected (len=%d prefix=%q). Re-save the record via UI to encrypt it.",
			len(enc), previewPlain(enc))
		return enc, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, prefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// previewPlain 只返回前 4 个字符（用于 warn log 里标识是哪条记录，不泄露完整 token）
func previewPlain(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}
