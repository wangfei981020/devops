package handlers

import (
	"database/sql"
	"encoding/json"

	"ops-cmdb-backend/crypto"
)

// ⚠️ 本文件的 HTTP handler 已迁移到 internal/api/inventory/registrar
// （租户隔离 + 修掉了 `WHERE id=?` 的跨租户越权）。**不要在这里再写一份。**
//
// 我就重写过一次：以为注册商后端不存在（grep 只扫了 handlers/ 目录），
// 结果 gin 因路由重复注册直接 panic，Pod 起不来。
// 找接口在哪，要连 internal/api/ 一起 grep。
//
// 这里只保留 LoadCredential —— 它有 6 个尚未迁移的调用方
// （domain_sync / certs / dns_write / domain_registrar_expiry / scheduler）。

// LoadCredential 供 ACME / 域名同步等内部模块解密取用某注册商凭据。**不经 HTTP 暴露。**
//
// ⚠️ 这个函数**不做租户过滤**，是迁移期的临时状态：它的 6 个调用方
// （domain_sync / certs / dns_write / domain_registrar_expiry / scheduler）
// 都还在旧架构里，那边同样没有租户上下文。调用方迁到 store 层时必须一起换掉。
//
// ⚠️ 上面那些 HTTP handler 走的是 sc（带租户过滤）；只有这一个是裸 DB。
// 不要照着它写新的查询。
func LoadCredential(db *sql.DB, cipher *crypto.Cipher, registrarID int) (provider string, cred map[string]string, err error) {
	var enc string
	err = db.QueryRow(`SELECT provider, COALESCE(credential_enc,'') FROM registrars WHERE id=?`, registrarID).Scan(&provider, &enc)
	if err != nil {
		return "", nil, err
	}
	cred = map[string]string{}
	if enc != "" {
		plain, e := cipher.Decrypt(enc)
		if e != nil {
			return provider, nil, e
		}
		_ = json.Unmarshal([]byte(plain), &cred)
	}
	return provider, cred, nil
}
