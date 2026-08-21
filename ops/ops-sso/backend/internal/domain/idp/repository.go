package idp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ops-sso-backend/internal/secrets"
	"ops-sso-backend/internal/store"
)

// Repo 身份源配置、登录中间状态、以及用户落地。
type Repo struct {
	st    *store.Store
	box   *secrets.Box
	cache *KeyCache
	http  *http.Client
}

func NewRepo(st *store.Store, box *secrets.Box) *Repo {
	return &Repo{
		st: st, box: box, cache: NewKeyCache(),
		// 超时必须设。默认的 http.Client 没有超时 ——
		// 上游卡住时，我们的 goroutine 会一直挂着，连接池慢慢耗尽，
		// 最后表现成"整个系统变慢"，而根因在别人家。
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// ListEnabled 登录页要显示哪些按钮。
//
// **不返回 client_secret**，连密文都不返回 —— 登录页是未认证接口。
func (r *Repo) ListEnabled(ctx context.Context) ([]Config, error) {
	p := r.st.Platform("idp/repository.go")
	rows, err := p.Query(ctx, `SELECT id, name, protocol FROM idp_configs
		WHERE enabled = 1 AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Config
	for rows.Next() {
		var c Config
		var proto string
		if err := rows.Scan(&c.ID, &c.Name, &proto); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get 取完整配置（含解密后的 client_secret）。只在服务端流程里用。
func (r *Repo) Get(ctx context.Context, id int64) (Config, string, error) {
	p := r.st.Platform("idp/repository.go")
	var (
		c         Config
		scopes    string
		jit       int
		enabled   int
		jitGroups string
		secEnc    []byte
	)
	err := p.QueryRow(ctx, `SELECT id, name, issuer, client_id, client_secret_enc,
		       auth_url, token_url, jwks_url, redirect_uri, scopes,
		       subject_claim, name_claim, email_claim, groups_claim, dept_claim,
		       jit_create, jit_groups, enabled
		FROM idp_configs WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&c.ID, &c.Name, &c.Issuer, &c.ClientID, &secEnc,
			&c.AuthURL, &c.TokenURL, &c.JWKSURL, &c.RedirectURI, &scopes,
			&c.SubjectClaim, &c.NameClaim, &c.EmailClaim, &c.GroupsClaim, &c.DeptClaim,
			&jit, &jitGroups, &enabled)
	if err != nil {
		return Config{}, "", err
	}
	c.Scopes = strings.Fields(scopes)
	c.JITCreate = jit == 1
	c.Enabled = enabled == 1
	for _, g := range strings.Split(jitGroups, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(g), 10, 64); err == nil && id > 0 {
			c.JITDefaultGroups = append(c.JITDefaultGroups, id)
		}
	}
	var clientSecret string
	if len(secEnc) > 0 {
		plain, err := r.box.Open(secEnc)
		if err != nil {
			return Config{}, "", fmt.Errorf("idp: client_secret 无法解开（换过 OAP_SECRET_KEY？）: %w", err)
		}
		clientSecret = string(plain)
	}
	return c, clientSecret, nil
}

// SaveState 存登录中间状态。
func (r *Repo) SaveState(ctx context.Context, s AuthState) error {
	p := r.st.Platform("idp/repository.go")
	_, err := p.Exec(ctx, `INSERT INTO idp_auth_states
		(state, nonce, code_verifier, config_id, next_url) VALUES (?, ?, ?, ?, ?)`,
		s.State, s.Nonce, s.CodeVerifier, s.ConfigID, s.Next)
	return err
}

// ConsumeState 取出并**作废**一个 state。
//
// 用「先 UPDATE 再 SELECT」而不是反过来：并发下同一个 state 只能有一个人拿到，
// 否则授权码被截获时，攻击者可以和用户同时用同一个 state 换 token。
func (r *Repo) ConsumeState(ctx context.Context, state string) (AuthState, error) {
	p := r.st.Platform("idp/repository.go")
	res, err := p.Exec(ctx, `UPDATE idp_auth_states SET consumed_at = NOW()
		WHERE state = ? AND consumed_at IS NULL`, state)
	if err != nil {
		return AuthState{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 没抢到 = 不存在，或已经被用过。两者都是 ErrStateMismatch：
		// 区分开会告诉攻击者"这个 state 存在过"
		return AuthState{}, ErrStateMismatch
	}
	var s AuthState
	if err := p.QueryRow(ctx, `SELECT state, nonce, code_verifier, config_id, next_url, created_at
		FROM idp_auth_states WHERE state = ?`, state).
		Scan(&s.State, &s.Nonce, &s.CodeVerifier, &s.ConfigID, &s.Next, &s.CreatedAt); err != nil {
		return AuthState{}, err
	}
	if s.Expired(time.Now()) {
		return AuthState{}, ErrStateExpired
	}
	return s, nil
}

// PurgeStates 清理过期的中间状态。不清的话这张表会无限增长。
func (r *Repo) PurgeStates(ctx context.Context) (int64, error) {
	p := r.st.Platform("idp/repository.go")
	res, err := p.Exec(ctx, `DELETE FROM idp_auth_states WHERE created_at < ?`,
		time.Now().Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ── 与上游通信 ──────────────────────────────────────────────────

// ExchangeCode 用授权码换 id_token。
func (r *Repo) ExchangeCode(ctx context.Context, c Config, clientSecret, code, verifier string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURI)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", clientSecret)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("idp: 换 token 失败（上游不可达）: %w", err)
	}
	defer resp.Body.Close()
	// 限制读取大小：上游返回一个巨大响应时不该把我们的内存吃光
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		// ⚠️ 不把上游响应原文透给客户端 —— 里面可能有 client_secret 回显
		return "", fmt.Errorf("idp: 上游拒绝了授权码（HTTP %d）", resp.StatusCode)
	}
	var tr struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("idp: 上游返回的不是合法 JSON")
	}
	if tr.IDToken == "" {
		return "", fmt.Errorf("idp: 上游没有返回 id_token（scope 里带 openid 了吗）")
	}
	return tr.IDToken, nil
}

// VerifyToken 拉 JWKS 并验签。kid 未命中时强刷一次 —— 上游轮换密钥当天要能自愈。
func (r *Repo) VerifyToken(ctx context.Context, c Config, idToken string) error {
	now := time.Now()
	jwks, need := r.cache.Get(c.JWKSURL, now)
	if need {
		var err error
		if jwks, err = r.fetchJWKS(ctx, c.JWKSURL); err != nil {
			return err
		}
		r.cache.Put(c.JWKSURL, jwks, now)
	}

	err := VerifySignature(idToken, jwks)
	if errors.Is(err, ErrKeyNotFound) && r.cache.AllowForceRefresh(c.JWKSURL, now) {
		// IdP 轮换了密钥：先发新 kid、再撤旧 kid。缓存里没有新 kid 就直接拒绝的话，
		// 轮换当天所有人都登不上，而这通常发生在半夜且没有预告。
		fresh, ferr := r.fetchJWKS(ctx, c.JWKSURL)
		if ferr != nil {
			return err
		}
		r.cache.Put(c.JWKSURL, fresh, now)
		return VerifySignature(idToken, fresh)
	}
	return err
}

func (r *Repo) fetchJWKS(ctx context.Context, jwksURL string) (JWKS, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return JWKS{}, err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return JWKS{}, fmt.Errorf("idp: 拉 JWKS 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return JWKS{}, fmt.Errorf("idp: 拉 JWKS 失败（HTTP %d）", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return JWKS{}, err
	}
	return ParseJWKS(b)
}

// ── 用户落地 ────────────────────────────────────────────────────

// Resolve 把上游身份换成本地用户 ID。
//
// 找不到时按 JIT 配置决定：建人，还是拒绝。
// **默认拒绝** —— 上游能登录 ≠ 该给他访问权。
func (r *Repo) Resolve(ctx context.Context, c Config, id Identity, tenantID int64) (int64, bool, error) {
	p := r.st.Platform("idp/repository.go")

	var uid int64
	var status string
	err := p.QueryRow(ctx, `SELECT id, status FROM users
		WHERE source = 'oidc' AND idp_config_id = ? AND username = ? AND deleted_at IS NULL`,
		c.ID, id.ExternalID).Scan(&uid, &status)
	switch {
	case err == nil:
		if status != "active" {
			// 离职断权之后又从上游登进来：必须继续拒绝。
			// 这里放行的话，断权就只挡得住本地账号。
			return 0, false, fmt.Errorf("idp: 账号已停用")
		}
		// 每次登录同步显示名与邮箱：人改了名字，审计里也该跟着变
		_, _ = p.Exec(ctx, `UPDATE users SET display_name = ?, email = ? WHERE id = ?`,
			id.DisplayName, id.Email, uid)
		return uid, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, false, err
	}

	if !c.JITCreate {
		return 0, false, ErrJITDisabled
	}

	res, err := p.Exec(ctx, `INSERT INTO users
		(username, display_name, email, employee_id, source, idp_config_id, status)
		VALUES (?, ?, ?, ?, 'oidc', ?, 'active')`,
		id.ExternalID, id.DisplayName, id.Email, id.ExternalID, c.ID)
	if err != nil {
		return 0, false, err
	}
	uid, _ = res.LastInsertId()

	if _, err := p.Exec(ctx, `INSERT INTO user_tenants (user_id, tenant_id, role_code)
		VALUES (?, ?, 'member') ON DUPLICATE KEY UPDATE role_code = role_code`,
		uid, tenantID); err != nil {
		return uid, true, err
	}

	// JIT 建人后落到配置的默认组。**空列表是合理的默认值**：
	// 不进任何组 = 没有任何授权命中他 = 默认拒绝。
	// 他能登录，但看不到任何应用 —— 这正是我们要的：登录与授权分开。
	q, err := r.st.Tenant(store.WithTenant(ctx, store.TenantID(tenantID)))
	if err == nil {
		for _, gid := range c.JITDefaultGroups {
			_, _ = q.Insert(`INSERT IGNORE INTO user_group_members (tenant_id, group_id, user_id)
				VALUES (?, ?, ?)`, gid, uid)
		}
	}
	return uid, true, nil
}
