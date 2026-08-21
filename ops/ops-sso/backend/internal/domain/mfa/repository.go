package mfa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ops-sso-backend/internal/secrets"
	"ops-sso-backend/internal/store"
)

var (
	ErrNotEnrolled = errors.New("mfa: 该用户还没绑定二次验证")
	ErrBadCode     = errors.New("mfa: 验证码不对")
	ErrAlreadyDone = errors.New("mfa: 已经绑定过了")
)

type Repo struct {
	st  *store.Store
	box *secrets.Box
}

func NewRepo(st *store.Store, box *secrets.Box) *Repo { return &Repo{st: st, box: box} }

// BeginEnroll 生成密钥并返回二维码内容。
//
// **此时还不算绑定成功** —— 必须先用验证器验一次（ConfirmEnroll）。
// 少了这一步，用户扫码失败却以为绑好了，下次需要验证时就被永久挡在外面。
func (r *Repo) BeginEnroll(ctx context.Context, userID int64, issuer, account string) (secret, uri string, err error) {
	secret, err = NewSecret()
	if err != nil {
		return "", "", err
	}
	enc, err := r.box.Seal([]byte(secret))
	if err != nil {
		return "", "", err
	}
	p := r.st.Platform("mfa/repository.go")
	// 重新绑定时覆盖旧密钥，并把 confirmed_at 清空 —— 旧密钥立刻失效，
	// 但在确认之前用户仍然处于"未绑定"状态
	if _, err = p.Exec(ctx, `INSERT INTO mfa_secrets (user_id, secret_enc, confirmed_at)
		VALUES (?, ?, NULL)
		ON DUPLICATE KEY UPDATE secret_enc = VALUES(secret_enc), confirmed_at = NULL`,
		userID, enc); err != nil {
		return "", "", fmt.Errorf("mfa: 保存密钥失败: %w", err)
	}
	return secret, ProvisioningURI(issuer, account, secret), nil
}

// ConfirmEnroll 用一次验证码确认绑定。
func (r *Repo) ConfirmEnroll(ctx context.Context, userID int64, code string, now time.Time) error {
	secret, confirmed, err := r.load(ctx, userID)
	if err != nil {
		return err
	}
	if confirmed {
		return ErrAlreadyDone
	}
	if !Verify(secret, code, now) {
		return ErrBadCode
	}
	p := r.st.Platform("mfa/repository.go")
	_, err = p.Exec(ctx, `UPDATE mfa_secrets SET confirmed_at = NOW() WHERE user_id = ?`, userID)
	return err
}

// Enrolled 是否已完成绑定。
func (r *Repo) Enrolled(ctx context.Context, userID int64) (bool, error) {
	_, confirmed, err := r.load(ctx, userID)
	if errors.Is(err, ErrNotEnrolled) {
		return false, nil
	}
	return confirmed, err
}

func (r *Repo) load(ctx context.Context, userID int64) (secret string, confirmed bool, err error) {
	p := r.st.Platform("mfa/repository.go")
	var enc []byte
	var at sql.NullTime
	if err = p.QueryRow(ctx, `SELECT secret_enc, confirmed_at FROM mfa_secrets WHERE user_id = ?`,
		userID).Scan(&enc, &at); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotEnrolled
		}
		return "", false, err
	}
	plain, err := r.box.Open(enc)
	if err != nil {
		// 密钥换过了 / 密文损坏。**不能静默当成"没绑定"** ——
		// 那会让人重新绑一遍，旧票据的语义也跟着糊掉。如实报错。
		return "", false, fmt.Errorf("mfa: 密钥无法解开（换过 OAP_SECRET_KEY？）: %w", err)
	}
	return string(plain), at.Valid, nil
}

// IssueTicket 验一次码，换一张短时提权票据。
func (r *Repo) IssueTicket(ctx context.Context, userID, appID int64, scope, ticketRef, code string,
	ttl time.Duration, now time.Time) (Ticket, error) {

	secret, confirmed, err := r.load(ctx, userID)
	if err != nil {
		return Ticket{}, err
	}
	if !confirmed {
		return Ticket{}, ErrNotEnrolled
	}
	if !Verify(secret, code, now) {
		return Ticket{}, ErrBadCode
	}

	q, err := r.st.Tenant(ctx)
	if err != nil {
		return Ticket{}, err
	}
	exp := now.Add(ttl)
	res, err := q.Insert(`INSERT INTO step_up_tickets
		(tenant_id, user_id, app_id, scope, ticket_ref, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, userID, appID, scope, ticketRef, exp)
	if err != nil {
		return Ticket{}, fmt.Errorf("mfa: 签发票据失败: %w", err)
	}
	id, _ := res.LastInsertId()
	return Ticket{ID: id, UserID: userID, AppID: appID, Scope: scope,
		TicketRef: ticketRef, IssuedAt: now, ExpiresAt: exp}, nil
}

// FindValidTicket 网关用：这个人此刻对这个路径有没有有效票据。
//
// 每个 challenge 请求都会走它，所以只查一次、在内存里筛作用域 ——
// 把作用域匹配写进 SQL 的 LIKE 会让索引失效。
func (r *Repo) FindValidTicket(ctx context.Context, userID, appID int64, path string, now time.Time) (*Ticket, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, scope, ticket_ref, issued_at, expires_at
		FROM step_up_tickets
		WHERE tenant_id = ? AND user_id = ? AND app_id = ? AND revoked_at IS NULL AND expires_at > ?
		ORDER BY expires_at DESC LIMIT 20`, userID, appID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.Scope, &t.TicketRef, &t.IssuedAt, &t.ExpiresAt); err != nil {
			return nil, err
		}
		t.UserID, t.AppID = userID, appID
		if t.Valid(now) && t.Covers(path) {
			return &t, nil
		}
	}
	return nil, rows.Err()
}

// TouchTicket 记一次使用。
//
// 只计数不限次：限次会让一个正常操作（比如批量删 20 个）反复弹验证码，
// 人就会开始把 TTL 调到 8 小时 —— 那才是真正的风险。
func (r *Repo) TouchTicket(ctx context.Context, id int64) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return
	}
	_, _ = q.Exec(`UPDATE step_up_tickets SET used_count = used_count + 1
		WHERE tenant_id = ? AND id = ?`, id)
}

// RevokeUserTickets 离职断权 / 权限回收时，把票据一并作废。
//
// 漏掉这一步的话：权限已经收回，但他手里那张 30 分钟的票还能用 ——
// 「临时」二字就又变成假的了。
func (r *Repo) RevokeUserTickets(ctx context.Context, userID int64) (int64, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	res, err := q.Exec(`UPDATE step_up_tickets SET revoked_at = NOW()
		WHERE tenant_id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > NOW()`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
