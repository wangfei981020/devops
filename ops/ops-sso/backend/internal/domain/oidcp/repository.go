package oidcp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"ops-sso-backend/internal/secrets"
	"ops-sso-backend/internal/store"
)

type Repo struct {
	st  *store.Store
	box *secrets.Box

	// 签名密钥缓存：每次签 token 都从库里读 + 解密 + 解析 PEM，
	// 在登录高峰会成为瓶颈。密钥变更很少，缓存代价极低。
	mu     sync.RWMutex
	keys   []SigningKey
	loaded time.Time
}

func NewRepo(st *store.Store, box *secrets.Box) *Repo {
	return &Repo{st: st, box: box}
}

// GetClient 按 client_id 取客户端。
func (r *Repo) GetClient(ctx context.Context, clientID string) (Client, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return Client{}, err
	}
	var (
		c                       Client
		redirects, postLogout   string
		scopes, claims          string
		pub, pkce, enabled, ttl int
	)
	err = q.QueryRow(`SELECT id, app_id, client_id, redirect_uris, post_logout_uris,
		       scopes, public_client, require_pkce, id_token_ttl_sec, claims_profile, enabled
		FROM oidc_clients WHERE tenant_id = ? AND client_id = ? AND deleted_at IS NULL`,
		clientID).Scan(&c.ID, &c.AppID, &c.ClientID, &redirects, &postLogout,
		&scopes, &pub, &pkce, &ttl, &claims, &enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Client{}, ErrUnknownClient
		}
		return Client{}, err
	}
	c.RedirectURIs = splitLines(redirects)
	c.PostLogout = splitLines(postLogout)
	c.Scopes = strings.Fields(scopes)
	c.Claims = strings.Split(claims, ",")
	c.PublicClient = pub == 1
	c.RequirePKCE = pkce == 1
	c.Enabled = enabled == 1
	c.IDTokenTTL = time.Duration(ttl) * time.Second
	return c, nil
}

// VerifySecret 校验 client_secret。
//
// 公开客户端（SPA）没有 secret，靠 PKCE 保证 —— 那种情况直接放行到 PKCE 校验。
func (r *Repo) VerifySecret(ctx context.Context, clientID, secret string) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	var hash []byte
	var pub int
	if err := q.QueryRow(`SELECT client_secret_hash, public_client FROM oidc_clients
		WHERE tenant_id = ? AND client_id = ? AND deleted_at IS NULL`,
		clientID).Scan(&hash, &pub); err != nil {
		return ErrUnknownClient
	}
	if pub == 1 {
		return nil
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(secret)) != nil {
		return ErrBadSecret
	}
	return nil
}

// CreateClient 注册一个下游客户端。返回明文 secret —— **只在这一刻出现一次**。
func (r *Repo) CreateClient(ctx context.Context, c Client, actorID int64) (string, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return "", err
	}
	secret, err := randomSecret()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	pub, pkce := 0, 1
	if c.PublicClient {
		pub = 1
	}
	if !c.RequirePKCE {
		pkce = 0
	}
	ttl := int(c.IDTokenTTL / time.Second)
	if ttl <= 0 {
		ttl = 3600
	}
	if _, err := q.Insert(`INSERT INTO oidc_clients
		(tenant_id, app_id, client_id, client_secret_hash, redirect_uris, post_logout_uris,
		 scopes, public_client, require_pkce, id_token_ttl_sec, claims_profile, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.AppID, c.ClientID, hash, strings.Join(c.RedirectURIs, "\n"),
		strings.Join(c.PostLogout, "\n"), strings.Join(c.Scopes, " "),
		pub, pkce, ttl, strings.Join(c.Claims, ","), actorID); err != nil {
		return "", fmt.Errorf("oidcp: 注册客户端失败: %w", err)
	}
	return secret, nil
}

// ListClients 列出客户端。**绝不返回 secret**，连哈希都不返回。
func (r *Repo) ListClients(ctx context.Context) ([]Client, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, app_id, client_id, redirect_uris, post_logout_uris,
		       scopes, public_client, require_pkce, id_token_ttl_sec, claims_profile, enabled
		FROM oidc_clients WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		var c Client
		var redirects, postLogout, scopes, claims string
		var pub, pkce, enabled, ttl int
		if err := rows.Scan(&c.ID, &c.AppID, &c.ClientID, &redirects, &postLogout,
			&scopes, &pub, &pkce, &ttl, &claims, &enabled); err != nil {
			return nil, err
		}
		c.RedirectURIs = splitLines(redirects)
		c.PostLogout = splitLines(postLogout)
		c.Scopes = strings.Fields(scopes)
		c.Claims = strings.Split(claims, ",")
		c.PublicClient, c.RequirePKCE, c.Enabled = pub == 1, pkce == 1, enabled == 1
		c.IDTokenTTL = time.Duration(ttl) * time.Second
		out = append(out, c)
	}
	return out, rows.Err()
}

// ── 授权码 ──────────────────────────────────────────────────────

func (r *Repo) SaveCode(ctx context.Context, ac AuthCode) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	_, err = q.Insert(`INSERT INTO oidc_auth_codes
		(tenant_id, code_hash, client_id, user_id, redirect_uri, nonce, scope,
		 code_challenge, code_challenge_method, auth_time, session_id, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ac.Hash, ac.ClientID, ac.UserID, ac.RedirectURI, ac.Nonce, ac.Scope,
		ac.Challenge, ac.Method, ac.AuthTime, ac.SessionID, ac.ExpiresAt)
	return err
}

// ConsumeCode 兑换授权码。**一次性**：抢不到就是被别人先用了。
//
// 先 UPDATE 再 SELECT，用影响行数判定 —— 反过来写的话，
// 并发请求会同时通过 SELECT 的检查，然后各自换到一个 token。
func (r *Repo) ConsumeCode(ctx context.Context, code string) (AuthCode, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return AuthCode{}, err
	}
	h := HashCode(code)
	res, err := q.Exec(`UPDATE oidc_auth_codes SET used_at = NOW()
		WHERE tenant_id = ? AND code_hash = ? AND used_at IS NULL`, h)
	if err != nil {
		return AuthCode{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 不存在、或已被用过。两者返回同一个错误：
		// 区分开等于告诉攻击者"这个 code 确实存在过"
		return AuthCode{}, ErrCodeInvalid
	}

	var ac AuthCode
	if err := q.QueryRow(`SELECT code_hash, client_id, user_id, redirect_uri, nonce, scope,
		       code_challenge, code_challenge_method, auth_time, session_id, expires_at
		FROM oidc_auth_codes WHERE tenant_id = ? AND code_hash = ?`, h).
		Scan(&ac.Hash, &ac.ClientID, &ac.UserID, &ac.RedirectURI, &ac.Nonce, &ac.Scope,
			&ac.Challenge, &ac.Method, &ac.AuthTime, &ac.SessionID, &ac.ExpiresAt); err != nil {
		return AuthCode{}, err
	}
	if time.Now().After(ac.ExpiresAt) {
		return AuthCode{}, ErrCodeExpired
	}
	return ac, nil
}

// PurgeCodes 清理过期授权码。不清的话这张表会无限增长。
func (r *Repo) PurgeCodes(ctx context.Context) (int64, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	res, err := q.Exec(`DELETE FROM oidc_auth_codes
		WHERE tenant_id = ? AND expires_at < ?`, time.Now().Add(-time.Hour))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ── 签名密钥 ────────────────────────────────────────────────────

// Keys 取全部可用密钥，第一把是当前用于签名的。
//
// 首次调用时若库里没有密钥，**自动生成一把** —— 客户装完不需要额外做任何事
// 就能开始接应用。需要人工执行的步骤，早晚有一次会被漏掉。
func (r *Repo) Keys(ctx context.Context) ([]SigningKey, error) {
	r.mu.RLock()
	if len(r.keys) > 0 && time.Since(r.loaded) < 5*time.Minute {
		defer r.mu.RUnlock()
		return r.keys, nil
	}
	r.mu.RUnlock()

	p := r.st.Platform("oidcp/repository.go")
	rows, err := p.Query(ctx, `SELECT kid, algorithm, private_pem_enc, active
		FROM oidc_signing_keys WHERE retired_at IS NULL ORDER BY active DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	var keys []SigningKey
	for rows.Next() {
		var k SigningKey
		var enc []byte
		var active int
		if err := rows.Scan(&k.Kid, &k.Alg, &enc, &active); err != nil {
			rows.Close()
			return nil, err
		}
		pemBytes, err := r.box.Open(enc)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("oidcp: 签名私钥无法解开（换过 OAP_SECRET_KEY？）: %w", err)
		}
		blk, _ := pem.Decode(pemBytes)
		if blk == nil {
			rows.Close()
			return nil, errors.New("oidcp: 私钥 PEM 损坏")
		}
		priv, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
		if err != nil {
			rows.Close()
			return nil, err
		}
		k.Private, k.Active = priv, active == 1
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		k, err := r.generateKey(ctx)
		if err != nil {
			return nil, err
		}
		keys = []SigningKey{k}
	}

	r.mu.Lock()
	r.keys, r.loaded = keys, time.Now()
	r.mu.Unlock()
	return keys, nil
}

func (r *Repo) generateKey(ctx context.Context) (SigningKey, error) {
	k, err := NewSigningKey()
	if err != nil {
		return SigningKey{}, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k.Private),
	})
	enc, err := r.box.Seal(pemBytes)
	if err != nil {
		return SigningKey{}, err
	}
	jwk, _ := json.Marshal(k.PublicJWK())
	p := r.st.Platform("oidcp/repository.go")
	if _, err := p.Exec(ctx, `INSERT INTO oidc_signing_keys
		(kid, algorithm, private_pem_enc, public_jwk, active) VALUES (?, ?, ?, ?, 1)`,
		k.Kid, k.Alg, enc, string(jwk)); err != nil {
		return SigningKey{}, err
	}
	return k, nil
}

// ActiveKey 当前用于签名的那把。
func (r *Repo) ActiveKey(ctx context.Context) (SigningKey, error) {
	keys, err := r.Keys(ctx)
	if err != nil {
		return SigningKey{}, err
	}
	for _, k := range keys {
		if k.Active {
			return k, nil
		}
	}
	if len(keys) > 0 {
		return keys[0], nil
	}
	return SigningKey{}, errors.New("oidcp: 没有可用的签名密钥")
}

// ── 下游会话（全局登出）──────────────────────────────────────────

func (r *Repo) RecordSession(ctx context.Context, gateSessionID, userID int64, clientID, sid string) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	_, err = q.Insert(`INSERT INTO oidc_sessions
		(tenant_id, gate_session_id, client_id, user_id, sid) VALUES (?, ?, ?, ?, ?)`,
		gateSessionID, clientID, userID, sid)
	return err
}

// SessionsOfGate 关口会话对应的所有下游会话。登出时按它逐个通知。
func (r *Repo) SessionsOfGate(ctx context.Context, gateSessionID int64) ([]map[string]string, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT client_id, sid FROM oidc_sessions
		WHERE tenant_id = ? AND gate_session_id = ? AND revoked_at IS NULL`, gateSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var cid, sid string
		if err := rows.Scan(&cid, &sid); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"client_id": cid, "sid": sid})
	}
	return out, rows.Err()
}

// RevokeGateSessions 关口登出时，把下游会话一并标记为已注销。
func (r *Repo) RevokeGateSessions(ctx context.Context, gateSessionID int64) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	_, err = q.Exec(`UPDATE oidc_sessions SET revoked_at = NOW()
		WHERE tenant_id = ? AND gate_session_id = ? AND revoked_at IS NULL`, gateSessionID)
	return err
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const az = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = az[int(v)%len(az)]
	}
	return string(out), nil
}

// LoadSubject 取签发 token 需要的用户信息。
func (r *Repo) LoadSubject(ctx context.Context, userID int64) (Subject, error) {
	p := r.st.Platform("oidcp/repository.go")
	var s Subject
	s.UserID = userID
	if err := p.QueryRow(ctx, `SELECT username, display_name, email, employee_id, source
		FROM users WHERE id = ? AND deleted_at IS NULL`, userID).
		Scan(&s.Username, &s.DisplayName, &s.Email, &s.EmployeeID, &s.AuthSource); err != nil {
		return Subject{}, err
	}
	// 用户组：下游最常用的授权依据。给的是组名而不是 ID ——
	// 下游配置里写 "研发" 比写 "201" 可读得多，也不会因为我们重建数据而失效
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return s, nil // 拿不到租户上下文时只是少了组，不该让登录整个失败
	}
	rows, err := q.Query(`SELECT g.name FROM user_group_members m
		JOIN user_groups g ON g.id = m.group_id AND g.tenant_id = m.tenant_id
		WHERE m.tenant_id = ? AND m.user_id = ? AND g.deleted_at IS NULL`, userID)
	if err != nil {
		return s, nil
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			s.Groups = append(s.Groups, n)
		}
	}
	return s, nil
}

// VerifyOwnToken 校验我们自己签发的 token（userinfo 端点用）。
//
// 与 idp 包里验上游 token 的逻辑分开：那边验的是**别人**签的，
// 这边验的是**自己**签的。合成一个函数会让"信任谁的密钥"这件事变模糊。
func (r *Repo) VerifyOwnToken(token string, keys []SigningKey, issuer string, now time.Time) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, errors.New("oidcp: token 格式不对")
	}
	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, err
	}
	var hdr struct{ Alg, Kid string }
	if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
		return 0, err
	}
	if hdr.Alg != "RS256" {
		return 0, errors.New("oidcp: 只接受 RS256")
	}

	var key *SigningKey
	for i := range keys {
		if keys[i].Kid == hdr.Kid {
			key = &keys[i]
			break
		}
	}
	if key == nil {
		return 0, errors.New("oidcp: 找不到对应的签名密钥")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.Private.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		return 0, errors.New("oidcp: 签名校验失败")
	}

	plRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	var cl struct {
		Sub string  `json:"sub"`
		Iss string  `json:"iss"`
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(plRaw, &cl); err != nil {
		return 0, err
	}
	if cl.Iss != issuer {
		return 0, errors.New("oidcp: issuer 不匹配")
	}
	if now.Unix() > int64(cl.Exp) {
		return 0, errors.New("oidcp: token 已过期")
	}
	var uid int64
	if _, err := fmt.Sscanf(cl.Sub, "%d", &uid); err != nil || uid <= 0 {
		return 0, errors.New("oidcp: sub 不合法")
	}
	return uid, nil
}
