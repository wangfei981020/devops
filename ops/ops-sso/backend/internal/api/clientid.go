package api

import (
	"crypto/rand"
	"math/big"
)

// base62 生成 client_id 用的字母表。
//
// 不用 base64：client_id 会出现在 URL 查询串、配置文件、日志里，
// `+` `/` `=` 在这些地方都要转义或被截断，而转义过一次的 client_id
// 和原值比对不上 —— 那种问题查起来极其费劲。
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// generateClientID 生成 `client_` + 22 位 base62（约 131 位熵）。
//
// # 为什么不让人自己填
//
// 让人填，他会填一个"好记的"：harbor、prod、test。
// 而好记等于好猜 —— client_id 本身不是机密，但可枚举的 client_id
// 让攻击者省掉侦察这一步，直接开始试 redirect_uri。
//
// # 为什么是这个形状
//
// `client_` 前缀 + 22 位随机，对齐 Auth0 / Okta 的惯例，也和参考实现一致。
// 有前缀的好处是它出现在别人的日志里时一眼能认出是什么，
// 不至于被当成一段乱码或某个 ID 去查。
func generateClientID() (string, error) {
	n := big.NewInt(int64(len(base62)))
	out := make([]byte, 22)
	for i := range out {
		v, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", err
		}
		out[i] = base62[v.Int64()]
	}
	return "client_" + string(out), nil
}
