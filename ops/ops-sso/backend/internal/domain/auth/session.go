// Package auth 是认证域：本地口令、会话、应急通道。
//
// # 与授权的边界
//
// 认证回答「你是谁」，授权（access / pathpolicy 包）回答「你能干什么」。
// 本包**绝不**做任何 allow/deny 判断 —— 混在一起会让「登录成功」被误当成「有权限」。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"ops-sso-backend/internal/store"
)

var (
	ErrBadCredential = errors.New("auth: 用户名或口令不对")
	ErrDisabled      = errors.New("auth: 账号已停用")
	ErrSessionGone   = errors.New("auth: 会话无效或已过期")
	ErrCodeInvalid   = errors.New("auth: 应急口令无效")
	ErrCodeUsed      = errors.New("auth: 应急口令已被使用")
	ErrCodeExpired   = errors.New("auth: 应急口令已过期")
	// ErrNoLocalPassword 该账号没有本地口令（比如上游 IdP 登录进来的）。
	ErrNoLocalPassword = errors.New("auth: 该账号没有本地口令")
	// ErrSamePassword 新旧口令一样。
	ErrSamePassword = errors.New("auth: 新口令不能与旧口令相同")
	// ErrWeakPassword 新口令不合规。
	ErrWeakPassword = errors.New("auth: 新口令不合规")
	// ErrBackend 认证链路本身故障（数据库不可达等）。
	// 与"口令不对"严格分开 —— 混为一谈会让排障方向从一开始就是错的。
	ErrBackend = errors.New("auth: 认证服务暂时不可用")
)

// SessionTTL 会话有效期。
//
// 8 小时 = 一个工作日。更长会让「下班后忘了锁屏」变成风险，
// 更短会让人一天登好几次，然后开始想办法绕过。
const SessionTTL = 8 * time.Hour

// Session 一次登录。
type Session struct {
	ID       int64
	UserID   int64
	TenantID int64
	Source   string // local / oidc / break_glass
	IssuedAt time.Time
	ExpireAt time.Time
}

// Identity 已认证的身份，注入到请求上下文。
type Identity struct {
	UserID       int64
	TenantID     int64
	Username     string
	DisplayName  string
	Source       string
	IsBreakGlass bool
	SessionID    int64
	// MustChange 必须先改密才能做别的事。
	//
	// 放进 Identity 而不是每次去查库：它在**每个请求**的中间件里都要判，
	// 而它只在改密那一刻变一次。
	MustChange bool

	// RoleCode 这个人在**当前租户**里的角色（user_tenants.role_code）。
	//
	// 放在 Identity 里而不是每次查库，理由同 MustChange：每个请求的
	// 中间件都要判，而它很少变。
	//
	// ⚠️ 角色是**租户内**的，不是全局的：同一个人可以在 A 租户是管理员、
	// 在 B 租户只是普通成员。所以取的时候必须带上会话里那个 tenant_id。
	RoleCode string
}

// RoleAdmin 租户管理员。控制台的写操作与大部分读操作都要求它。
const RoleAdmin = "admin"

// IsAdmin 是不是当前租户的管理员。
func (i Identity) IsAdmin() bool { return i.RoleCode == RoleAdmin }

// Service 认证服务。
type Service struct {
	st *store.Store
}

func New(st *store.Store) *Service { return &Service{st: st} }

// hashToken 会话令牌只存哈希 —— 库被拖走也无法拿去登录。
// 这是最便宜的一道防线，代价只有一次 SHA-256。
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// newToken 生成 32 字节随机令牌。
//
// 用 crypto/rand 而不是 math/rand：后者的输出可预测，
// 而"可预测的会话令牌"等于任何人都能伪造任何人的登录态。
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// LoginLocal 本地账号登录。
//
// 返回明文令牌（只在这一刻存在），库里只留哈希。
func (s *Service) LoginLocal(ctx context.Context, username, password string, tenantID int64, ip, ua string) (string, Identity, error) {
	p := s.st.Platform("auth/session.go")

	var (
		uid        int64
		status     string
		display    string
		breakGlass int
		hash       []byte
	)
	err := p.QueryRow(ctx, `SELECT u.id, u.status, u.display_name, u.is_break_glass, c.password_hash
		FROM users u JOIN local_credentials c ON c.user_id = u.id
		WHERE u.username = ? AND u.source = 'local' AND u.deleted_at IS NULL`, username).
		Scan(&uid, &status, &display, &breakGlass, &hash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// 用户不存在与口令错误返回**同一个错误**：区分开等于送一个用户名枚举接口。
		// 这里仍然跑一次 bcrypt，让两条路径耗时相近，不给计时攻击留缝。
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalid"), []byte(password))
		return "", Identity{}, ErrBadCredential
	case err != nil:
		// ★ 数据库故障绝不能伪装成「口令不对」。
		// 第一版就是这么写的，结果 MySQL 掉连接时所有人被告知"用户名或口令不对"——
		// 运维会去查用户表，而真正的问题在数据库连接上，方向一开始就是错的。
		return "", Identity{}, fmt.Errorf("%w: %v", ErrBackend, err)
	}
	if status != "active" {
		return "", Identity{}, ErrDisabled
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return "", Identity{}, ErrBadCredential
	}
	var mustChange int
	_ = p.QueryRow(ctx, `SELECT must_change FROM local_credentials WHERE user_id = ?`, uid).Scan(&mustChange)

	source := "local"
	_ = source
	if breakGlass == 1 {
		// 应急账号不能只靠口令进 —— 必须走 LoginBreakGlass 带一次性口令。
		// 否则"应急账号"就只是个永久有效的后门账号。
		return "", Identity{}, ErrBadCredential
	}
	tok, id, err := s.issue(ctx, uid, tenantID, source, ip, ua, display, username, false)
	if err != nil {
		return "", Identity{}, err
	}
	id.MustChange = mustChange == 1
	return tok, id, nil
}

// issue 建会话。
func (s *Service) issue(ctx context.Context, uid, tenantID int64, source, ip, ua, display, username string, bg bool) (string, Identity, error) {
	tok, err := newToken()
	if err != nil {
		return "", Identity{}, err
	}
	p := s.st.Platform("auth/session.go")
	res, err := p.Exec(ctx, `INSERT INTO auth_sessions
		(user_id, tenant_id, token_hash, source, client_ip, user_agent, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uid, tenantID, hashToken(tok), source, ip, trunc(ua, 255), time.Now().Add(SessionTTL))
	if err != nil {
		return "", Identity{}, fmt.Errorf("create session: %w", err)
	}
	sid, _ := res.LastInsertId()
	_, _ = p.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = ?`, uid)

	return tok, Identity{
		UserID: uid, TenantID: tenantID, Username: username, DisplayName: display,
		Source: source, IsBreakGlass: bg, SessionID: sid,
	}, nil
}

// Authenticate 校验令牌，返回身份。
//
// 顺带刷新 last_seen_at —— 「这个会话还活着吗」是排障时最常问的，
// 没有这个字段就只能靠登录时间猜。
func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	if strings.TrimSpace(token) == "" {
		return Identity{}, ErrSessionGone
	}
	p := s.st.Platform("auth/session.go")
	var (
		id      Identity
		expires time.Time
		revoked *time.Time
		bg      int
		status  string
	)
	var mustChange sql.NullInt64
	var roleCode sql.NullString
	err := p.QueryRow(ctx, `SELECT s.id, s.user_id, s.tenant_id, s.source, s.expires_at, s.revoked_at,
		       u.username, u.display_name, u.is_break_glass, u.status, c.must_change,
		       ut.role_code
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		LEFT JOIN local_credentials c ON c.user_id = u.id
		-- LEFT JOIN：租户归属没了也要让请求继续走到"没权限"，
		-- 而不是变成"会话无效"。两者的排查方向完全不同。
		LEFT JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = s.tenant_id
		WHERE s.token_hash = ?`, hashToken(token)).
		Scan(&id.SessionID, &id.UserID, &id.TenantID, &id.Source, &expires, &revoked,
			&id.Username, &id.DisplayName, &bg, &status, &mustChange, &roleCode)
	if err != nil {
		return Identity{}, ErrSessionGone
	}
	if revoked != nil {
		return Identity{}, ErrSessionGone
	}
	if time.Now().After(expires) {
		return Identity{}, ErrSessionGone
	}
	// 账号被停用（离职断权）时，已建立的会话必须立刻失效。
	// 只在登录时查 status 是不够的 —— 那样断权后人还能继续用到会话过期。
	if status != "active" {
		return Identity{}, ErrDisabled
	}
	id.IsBreakGlass = bg == 1
	id.MustChange = mustChange.Valid && mustChange.Int64 == 1
	// 空角色 = 普通成员。**绝不能反过来当成管理员** ——
	// 数据缺失时默认给最小权限，是这类判断唯一安全的方向。
	id.RoleCode = strings.TrimSpace(roleCode.String)
	_, _ = p.Exec(ctx, `UPDATE auth_sessions SET last_seen_at = NOW() WHERE id = ?`, id.SessionID)
	return id, nil
}

// Revoke 注销单个会话。
func (s *Service) Revoke(ctx context.Context, sessionID int64, reason string) error {
	p := s.st.Platform("auth/session.go")
	_, err := p.Exec(ctx, `UPDATE auth_sessions SET revoked_at = NOW(), revoke_reason = ?
		WHERE id = ? AND revoked_at IS NULL`, reason, sessionID)
	return err
}

// RevokeAllOfUser 杀掉某人的全部会话。离职断权、临时授权到期回收都走它。
//
// 返回杀掉的数量 —— 断权链路上要显示「杀活动会话 7 个」，那个数字来自这里。
func (s *Service) RevokeAllOfUser(ctx context.Context, userID int64, reason string) (int64, error) {
	p := s.st.Platform("auth/session.go")
	res, err := p.Exec(ctx, `UPDATE auth_sessions SET revoked_at = NOW(), revoke_reason = ?
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > NOW()`, reason, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeOthersOfUser 踢掉某人**除当前会话外**的全部会话。
//
// 改密时用。保留当前会话是刻意的：否则用户改完密码立刻被自己踢出去，
// 会以为改失败了，然后再改一次。
func (s *Service) RevokeOthersOfUser(ctx context.Context, userID, keepSessionID int64, reason string) (int64, error) {
	p := s.st.Platform("auth/session.go")
	res, err := p.Exec(ctx, `UPDATE auth_sessions SET revoked_at = NOW(), revoke_reason = ?
		WHERE user_id = ? AND id <> ? AND revoked_at IS NULL AND expires_at > NOW()`,
		reason, userID, keepSessionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListSessionsOfUser 我的设备与会话。
func (s *Service) ListSessionsOfUser(ctx context.Context, userID int64) ([]map[string]any, error) {
	p := s.st.Platform("auth/session.go")
	rows, err := p.Query(ctx, `SELECT id, source, client_ip, user_agent, created_at, last_seen_at, expires_at
		FROM auth_sessions WHERE user_id = ? AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id                     int64
			source, ip, ua         string
			created, seen, expires time.Time
		)
		if err := rows.Scan(&id, &source, &ip, &ua, &created, &seen, &expires); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "source": source, "client_ip": ip, "user_agent": ua,
			"created_at": created, "last_seen_at": seen, "expires_at": expires,
		})
	}
	return out, rows.Err()
}

// ── 应急通道（P0-3）────────────────────────────────────────────────

// codeAlphabet 用 base32 去掉易混字符后的字母表：口令是**打印出来给人念/敲**的，
// 0/O、1/I 混淆会在最要命的时候浪费时间。
var codeEncoding = base32.NewEncoding("ABCDEFGHJKLMNPQRSTUVWXYZ23456789").WithPadding(base32.NoPadding)

// IssueBreakGlassCodes 为应急账号签发一批一次性口令。
//
// **明文只在这一刻返回一次**，库里只有哈希。调用方负责打印封存 ——
// 存在任何联网系统里都会重新引入「应急通道依赖了它要救的东西」这个环形依赖。
func (s *Service) IssueBreakGlassCodes(ctx context.Context, userID int64, n int, ttl time.Duration, issuedBy int64, batch string) ([]string, error) {
	if n <= 0 || n > 20 {
		n = 5
	}
	p := s.st.Platform("auth/session.go")

	// 旧批次整批作废：两批同时有效等于口令数量翻倍，且没人知道哪些还在流通
	if _, err := p.Exec(ctx, `UPDATE break_glass_codes SET revoked_at = NOW()
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL`, userID); err != nil {
		return nil, err
	}

	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		code := codeEncoding.EncodeToString(raw) // 16 位
		pretty := code[:4] + "-" + code[4:8] + "-" + code[8:12] + "-" + code[12:]
		if _, err := p.Exec(ctx, `INSERT INTO break_glass_codes
			(user_id, code_hash, batch, issued_by, expires_at) VALUES (?, ?, ?, ?, ?)`,
			userID, hashToken(pretty), batch, issuedBy, time.Now().Add(ttl)); err != nil {
			return nil, err
		}
		out = append(out, pretty)
	}
	return out, nil
}

// LoginBreakGlass 用一次性口令登录应急账号。
//
// 不需要短信、不需要上游 IdP、不需要任何外部系统 —— 这正是它存在的意义。
func (s *Service) LoginBreakGlass(ctx context.Context, username, code string, tenantID int64, ip, ua string) (string, Identity, error) {
	p := s.st.Platform("auth/session.go")

	var uid int64
	var display, status string
	var bg int
	if err := p.QueryRow(ctx, `SELECT id, display_name, status, is_break_glass FROM users
		WHERE username = ? AND deleted_at IS NULL`, username).Scan(&uid, &display, &status, &bg); err != nil {
		return "", Identity{}, ErrBadCredential
	}
	if bg != 1 {
		return "", Identity{}, ErrBadCredential
	}
	if status != "active" {
		return "", Identity{}, ErrDisabled
	}

	var (
		codeID  int64
		used    *time.Time
		revoked *time.Time
		expires time.Time
	)
	err := p.QueryRow(ctx, `SELECT id, used_at, revoked_at, expires_at FROM break_glass_codes
		WHERE user_id = ? AND code_hash = ?`, uid, hashToken(strings.ToUpper(strings.TrimSpace(code)))).
		Scan(&codeID, &used, &revoked, &expires)
	if err != nil {
		return "", Identity{}, ErrCodeInvalid
	}
	if used != nil {
		return "", Identity{}, ErrCodeUsed
	}
	if revoked != nil {
		return "", Identity{}, ErrCodeInvalid
	}
	if time.Now().After(expires) {
		return "", Identity{}, ErrCodeExpired
	}

	// 先置为已用再发会话：反过来的话，发会话失败时口令已经被消耗掉，
	// 而应急场景下手里可能只剩这一张纸。
	res, err := p.Exec(ctx, `UPDATE break_glass_codes SET used_at = NOW(), used_ip = ?
		WHERE id = ? AND used_at IS NULL`, ip, codeID)
	if err != nil {
		return "", Identity{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 并发下被别人抢先用掉了 —— 一次性必须是真的一次性
		return "", Identity{}, ErrCodeUsed
	}

	return s.issue(ctx, uid, tenantID, "break_glass", ip, ua, display, username, true)
}

// IssueForExternal 给已通过上游认证的用户发会话。
//
// 单独一个入口而不是复用 LoginLocal：外部身份**不校验本地口令**，
// 但仍然要走同一套会话签发与状态检查 —— 账号被停用时必须拒绝，
// 否则离职断权就只挡得住本地账号。
func (s *Service) IssueForExternal(ctx context.Context, userID, tenantID int64, source, ip, ua string) (string, Identity, error) {
	p := s.st.Platform("auth/session.go")
	var username, display, status string
	if err := p.QueryRow(ctx, `SELECT username, display_name, status FROM users
		WHERE id = ? AND deleted_at IS NULL`, userID).Scan(&username, &display, &status); err != nil {
		return "", Identity{}, ErrBadCredential
	}
	if status != "active" {
		return "", Identity{}, ErrDisabled
	}
	return s.issue(ctx, userID, tenantID, source, ip, ua, display, username, false)
}

// BootstrapPasswords 已知的初装弱口令。
//
// 这些是文档里公开写着的默认值，装完必须改。生产模式启动时会逐个比对，
// 只要还有账号在用其中之一，服务**拒绝启动** —— 见 CheckBootstrapPasswords。
//
// 为什么不干脆禁止设置：本地开发、演示环境、客户 POC 都需要一个好记的口令，
// 一刀切禁止的结果是人去改代码绕过，那更糟。给方便，但堵死它上生产的路。
var BootstrapPasswords = []string{"admin123", "Admin@123", "123456", "password"}

// MinPasswordLen 本地口令的最小长度。
const MinPasswordLen = 12

// SetLocalPassword 设置本地口令（初始化与改密共用）。
//
// bootstrap=true 时跳过长度检查，并标记 must_change —— 只给初装用。
func (s *Service) SetLocalPassword(ctx context.Context, userID int64, password string) error {
	return s.setPassword(ctx, userID, password, false)
}

// ChangeOwnPassword 用户自己改密。
//
// # 为什么要验旧口令
//
// 会话被盗时，攻击者能做的最有价值的一件事就是改密 —— 那样受害者
// 连登都登不回来。验旧口令让"偷到会话"和"接管账号"之间还隔着一道。
//
// # 为什么新口令要查黑名单
//
// 强制改密的意义是把初装弱口令换掉。允许从 admin123 改成 password 的话，
// 这一步只是走了个过场。
func (s *Service) ChangeOwnPassword(ctx context.Context, userID int64, oldPwd, newPwd string) error {
	p := s.st.Platform("auth/session.go")
	var hash []byte
	if err := p.QueryRow(ctx, `SELECT password_hash FROM local_credentials WHERE user_id = ?`,
		userID).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoLocalPassword
		}
		return fmt.Errorf("%w: %v", ErrBackend, err)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(oldPwd)) != nil {
		return ErrBadCredential
	}
	if oldPwd == newPwd {
		return ErrSamePassword
	}
	if utf8.RuneCountInString(newPwd) < MinPasswordLen {
		return fmt.Errorf("%w: 至少 %d 位", ErrWeakPassword, MinPasswordLen)
	}
	for _, weak := range BootstrapPasswords {
		if newPwd == weak {
			return fmt.Errorf("%w: 这是已知的初装口令，换一个", ErrWeakPassword)
		}
	}
	// 改密成功即清掉 must_change
	return s.setPassword(ctx, userID, newPwd, false)
}

// SetBootstrapPassword 设置初装口令。允许弱口令，但强制标记"必须改密"，
// 且生产模式下这个口令会让服务起不来。
func (s *Service) SetBootstrapPassword(ctx context.Context, userID int64, password string) error {
	return s.setPassword(ctx, userID, password, true)
}

// BootstrapScan 生产模式启动前的自检结果。
type BootstrapScan struct {
	// Settled 用着弱口令、而且**没有**必须改密标记的账号。
	// 这是真正的危险状态：有人把 admin123 当成了长期口令。
	Settled []string
	// Pending 用着弱口令、但被标记了必须改密的账号。
	// 它只能改自己的口令，别的什么都做不了（passwordChangeGuard 拦着）。
	Pending []string
}

// CheckBootstrapPasswords 生产模式启动前的自检。
//
// # 为什么分成两类，而不是一律拒绝启动
//
// 第一版是"只要有弱口令就拒启"。它把「默认 admin123 + 首次登录强制改密」
// 这条产品流程在生产模式下**整个变成不可达** —— 服务根本起不来，
// 没人有机会登进去改。我自己在本地 k8s 上撞到了这一点。
//
// 现在的判据是「这个弱口令是不是长期状态」：
//
//	Pending  已标记必须改密 → 放行启动，但每次启动都刺眼地喊一遍。
//	         这个账号除了改自己的口令做不了任何事，是个受控的过渡态。
//	Settled  没有该标记 → 拒绝启动。有人主动把弱口令设成了常态口令，
//	         那正是这道闸要拦的事。
//
// ⚠️ Pending 不是没有风险：admin123 是文档里公开写着的，
// 谁先登进去谁就能把它改成自己的。真正的生产，应当在
// `oapctl bootstrap-admin` 那一步就直接给强口令（部署手册 §三 就是这么写的），
// 而不是先弱后改 —— "回头再改"是永远不会发生的那件事。
func (s *Service) CheckBootstrapPasswords(ctx context.Context) (BootstrapScan, error) {
	var scan BootstrapScan
	p := s.st.Platform("auth/session.go")
	rows, err := p.Query(ctx, `SELECT u.username, c.password_hash, c.must_change
		FROM local_credentials c JOIN users u ON u.id = c.user_id
		WHERE u.deleted_at IS NULL AND u.status = 'active'`)
	if err != nil {
		return scan, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var hash []byte
		var mustChange bool
		if err := rows.Scan(&name, &hash, &mustChange); err != nil {
			return scan, err
		}
		for _, weak := range BootstrapPasswords {
			if bcrypt.CompareHashAndPassword(hash, []byte(weak)) == nil {
				if mustChange {
					scan.Pending = append(scan.Pending, name)
				} else {
					scan.Settled = append(scan.Settled, name)
				}
				break
			}
		}
	}
	return scan, rows.Err()
}

func (s *Service) setPassword(ctx context.Context, userID int64, password string, bootstrap bool) error {
	if !bootstrap && len(password) < MinPasswordLen {
		return fmt.Errorf("auth: 本地口令至少 %d 位", MinPasswordLen)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	mustChange := 0
	if bootstrap {
		mustChange = 1
	}
	p := s.st.Platform("auth/session.go")
	_, err = p.Exec(ctx, `INSERT INTO local_credentials (user_id, password_hash, must_change)
		VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE
		  password_hash = VALUES(password_hash), must_change = VALUES(must_change)`,
		userID, h, mustChange)
	return err
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
