// Package secrets 负责入库前的对称加密（探针口令、表单填充凭据等）。
//
// # 密钥从哪来
//
// 环境变量 OAP_SECRET_KEY（32 字节的十六进制）。**不进库、不进镜像、不进仓库**。
// 客户用 KMS 的话，由部署侧把 KMS 取到的值注入这个环境变量即可。
//
// # 为什么不是明文存
//
// 探针口令与表单填充凭据是"能登进下游应用"的东西。库被拖走 = 下游全失守。
// 加密不能防住能读环境变量的人，但能防住只拿到一份数据库备份的人 ——
// 而后者恰恰是最常见的泄露形态。
//
// # 缺密钥时的行为
//
// 启动时若没配密钥：**拒绝启动**，而不是退化成明文存。
// 静默降级会让人以为自己加密了。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var (
	ErrNoKey      = errors.New("secrets: 未配置 OAP_SECRET_KEY")
	ErrBadKey     = errors.New("secrets: OAP_SECRET_KEY 必须是 64 位十六进制（32 字节）")
	ErrCiphertext = errors.New("secrets: 密文损坏或密钥不对")
)

type Box struct{ aead cipher.AEAD }

// New 用十六进制密钥建加解密盒。
func New(hexKey string) (*Box, error) {
	if hexKey == "" {
		return nil, ErrNoKey
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, ErrBadKey
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal 加密。nonce 随密文一起存 —— GCM 的 nonce 不是秘密，但绝不能重复使用。
func (b *Box) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open 解密。
func (b *Box) Open(ct []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(ct) < n {
		return nil, ErrCiphertext
	}
	out, err := b.aead.Open(nil, ct[:n], ct[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCiphertext, err)
	}
	return out, nil
}
