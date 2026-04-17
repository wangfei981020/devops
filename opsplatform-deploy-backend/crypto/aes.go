package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

var globalKey []byte

// Init 初始化加密 Key (取前 32 字节作为 AES-256 Key)
func Init(key string) {
	b := []byte(key)
	if len(b) < 32 {
		// 不足 32 字节用 0 补齐
		padded := make([]byte, 32)
		copy(padded, b)
		globalKey = padded
		return
	}
	globalKey = b[:32]
}

// Encrypt AES-GCM 加密，返回 base64
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(globalKey)
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
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密 base64 → 明文
func Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(globalKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, payload := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Mask 脱敏显示 (token 等)
func Mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	if len(s) <= 8 {
		return s[:2] + "****"
	}
	return s[:4] + "****" + s[len(s)-2:]
}
