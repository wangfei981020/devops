package license

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ops-kit/licensekit"
)

// Store 授权在数据库里的那一面。
//
// 授权存库而不是存文件/环境变量，是因为**指纹必须由数据库派生**（LICENSING §4）：
// 多副本下每个 Pod 的宿主机标识都不同，用宿主机派生会让绑定安装的授权
// 在部分副本上校验失败 —— 表现是「重启后偶发提示未授权」，只在一部分请求上出现。
type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

type row struct {
	Token         string
	MismatchSince time.Time // 零值 = 从未发现不匹配
}

// Fingerprint 本套安装的指纹。
//
// ⚠️ 算法必须与其他产品**逐字节一致** —— 整份 license 只有一个 install_id，
// 客户在这套环境里报上来的指纹要能同时对上所有装在同一个库上的产品。
// 早先本产品自己算 sha256("oap-install|"+@@server_uuid) 取前 16 字节，
// 与共享内核的 HMAC(install_uuid|@@server_id) 完全不同，
// 结果是签发方按别的产品报的指纹签一张，本产品必然判不匹配。
func (s *Store) Fingerprint(ctx context.Context) (string, error) {
	var installUUID string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT install_uuid FROM install_identity WHERE id=1`).Scan(&installUUID); err != nil {
		return "", fmt.Errorf("读取安装标识: %w", err)
	}
	var sysID uint64
	// 取不到时**返回错误，不要退回 0**：退回 0 会让所有取不到的环境
	// 算出同一个指纹 —— 那等于把防拷贝这道防线悄悄关掉，且状态显示一切正常。
	if err := s.DB.QueryRowContext(ctx, `SELECT @@server_id`).Scan(&sysID); err != nil {
		return "", fmt.Errorf("读取数据库实例标识: %w", err)
	}
	return licensekit.Fingerprint(installUUID, sysID), nil
}

// load 取当前授权。没有授权时返回 nil, nil（不是错误）。
func (s *Store) load(ctx context.Context) (*row, error) {
	var r row
	var mismatch sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT code, mismatch_since FROM licenses WHERE active=1 ORDER BY id DESC LIMIT 1`).
		Scan(&r.Token, &mismatch)
	if errors.Is(err, sql.ErrNoRows) {
		// "没有授权"和"查不出来"是两件事：前者退回未激活是对的，
		// 后者退回未激活就是把一次数据库抖动变成了全公司的授权吊销
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if mismatch.Valid {
		r.MismatchSince = mismatch.Time
	}
	return &r, nil
}

// markMismatch 记下"首次发现指纹不匹配"的时刻。
//
// ⚠️ 带 `mismatch_since IS NULL` 条件是必须的：不加的话每次装载都会刷新它，
// 宽限期就永远从"刚刚"开始算 —— 等于永久宽限，而状态一直显示"宽限期内"，
// 没有任何迹象表明这道防线没在工作。
func (s *Store) markMismatch(ctx context.Context, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE licenses SET mismatch_since=? WHERE active=1 AND mismatch_since IS NULL`, at)
	return err
}

// noteMismatch 首次发现指纹不匹配时落库，返回该用于判定的 mismatchSince。
//
// 直接比指纹而不是问 Manager：这一步发生在装载之前，那时内核还没判过。
// 三个条件与内核 evaluate 里的完全一致，改一处必须改另一处。
func (s *Store) noteMismatch(ctx context.Context, p *licensekit.Payload, fp string, known time.Time) time.Time {
	if p == nil || p.InstallID == "" || fp == "" || p.InstallID == fp || !known.IsZero() {
		return known
	}
	now := time.Now()
	if err := s.markMismatch(ctx, now); err != nil {
		return known // 落不上就保持零值，下一轮还会再试
	}
	return now
}

// Reload 从库里装载一次授权到内存。
//
// 读不到、或验不过 → 退回未激活，**不是启动失败**：
// 没有授权的系统仍然要能跑（访问判定不查 license），
// 因为一个"连不上供应商就把全公司挡在门外"的系统没人敢装。
func (s *Store) Reload(ctx context.Context, mgr *Manager) error {
	fp, err := s.Fingerprint(ctx)
	if err != nil {
		return err
	}
	r, err := s.load(ctx)
	if err != nil {
		return err
	}
	if r == nil {
		mgr.Clear()
		return nil
	}
	p, err := licensekit.VerifyEmbedded(r.Token)
	if err != nil {
		// 验签失败 = **未激活**，不是"过期"。说成过期会把客户引去续费，
		// 而真正的问题是这串码本身不对（粘贴时少了一段、换了签发环境）。
		mgr.Clear()
		return nil
	}
	// 先落库再装载：反过来的话，首次发现不匹配的**那一次**拿到的还是零值，
	// 宽限期按旧规则判，要等下一轮才用上真值。
	mgr.LoadAt(p, fp, s.noteMismatch(ctx, p, fp, r.MismatchSince))
	return nil
}

// Activate 写入一份新授权。验签在调用方做，这里只负责落库。
//
// 旧行置为 active=0 而不是删除：授权是钱的账，留痕比省一行值钱得多。
// 换授权时新行的 mismatch_since 是 NULL —— 新授权照当前指纹签的，
// 上一份的"何时开始不匹配"对它没有意义，带过来会让新授权一装上就在宽限期里。
func (s *Store) Activate(ctx context.Context, token, licenseID string, by int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE licenses SET active=0 WHERE active=1`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO licenses (code, license_id, active, installed_by) VALUES (?, ?, 1, ?)`,
		token, licenseID, by); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// 多副本收敛（LICENSING §12）
// ---------------------------------------------------------------------------

type dbSource struct{ store *Store }

// Revision 当前授权行的时间戳**加上安装指纹**。
//
// ⚠️ 指纹必须算进来。只跟 updated_at 的话，客户把库恢复到新实例
// （server_id 变了 → 指纹变了）时授权行一个字节都没动，
// revision 不变、Fetch 不会被调用，于是所有正在跑的副本继续认为一切正常。
func (s dbSource) Revision(ctx context.Context) (string, error) {
	var v sql.NullTime
	err := s.store.DB.QueryRowContext(ctx,
		`SELECT updated_at FROM licenses WHERE active=1 ORDER BY id DESC LIMIT 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // 空串 = 确认没有授权，Watch 会 Clear
	}
	if err != nil {
		return "", err
	}
	fp, err := s.store.Fingerprint(ctx)
	if err != nil {
		// 保持原状。绝不能退回一个"没有指纹"的 revision —— 那会被当成一次
		// 真实变化，进而用空指纹装载，把指纹校验整个跳过
		return "", err
	}
	return v.Time.Format(time.RFC3339Nano) + "|" + fp, nil
}

func (s dbSource) Fetch(ctx context.Context) (*licensekit.Payload, string, time.Time, error) {
	fp, err := s.store.Fingerprint(ctx)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	r, err := s.store.load(ctx)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	if r == nil {
		// nil payload = **确认没有授权**，Watch 会 Clear。
		// 与"查不出来"（上面返回 error）严格区分：后者只保持原状。
		return nil, fp, time.Time{}, nil
	}
	p, err := licensekit.VerifyEmbedded(r.Token)
	if err != nil {
		return nil, fp, time.Time{}, nil
	}
	return p, fp, s.store.noteMismatch(ctx, p, fp, r.MismatchSince), nil
}

// StartWatch 起一个 goroutine 把本副本收敛到库里的最新授权。
//
// 20s 间隔对应「激活后 60s 内全部 Pod 生效」的验收标准，留了充足余量。
func (s *Store) StartWatch(ctx context.Context, mgr *Manager) {
	go mgr.Watch(ctx, dbSource{store: s}, 20*time.Second)
}
