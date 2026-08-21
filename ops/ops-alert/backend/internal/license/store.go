package license

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ops-alert-backend/logx"
	"ops-kit/licensekit"
)

// Store 授权在数据库里的那一面。
//
// ⚠️ 本文件与 ops-cmdb/backend/internal/license/store.go **逐字相同**（除包路径）。
// 这不是复制粘贴的懒惰：它是 LICENSING §4/§12 那套判据的实现，
// 里面每一条注释都对应一次真实的踩坑（宽限期重置、指纹漏算进 revision、
// "查不出来"被当成"没有授权"）。两处各写一份必然分叉，
// 而分叉的方向永远是**多给权限**。将来要动，两边一起动，
// 或者把它上收进 ops-kit/licensekit —— 但别只改一边。
//
// 授权存库而不是存文件/环境变量，是因为**指纹必须由数据库派生**
// （LICENSING §4）：多副本下每个 Pod 的宿主机标识都不同，
// 用宿主机派生会让绑定安装的授权在部分副本上校验失败 ——
// 表现是「重启后偶发提示未授权」，只在一部分请求上出现。
type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

// row 库里那一行的原始形态。
type row struct {
	Token         string
	MismatchSince time.Time // 零值 = 从未发现不匹配
	UpdatedAt     time.Time
}

// Fingerprint 本套安装的指纹。
//
// 两个输入都取自数据库：
//   - install_uuid  迁移里由 MySQL 的 UUID() 生成，一套库一个
//   - system id     MySQL 实例自己的标识
//
// ⚠️ 绝不能换成宿主机 MAC / hostname / machine-id，理由见 Store 的注释。
func (s *Store) Fingerprint(ctx context.Context) (string, error) {
	var installUUID string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT install_uuid FROM install_identity WHERE id=1`).Scan(&installUUID); err != nil {
		return "", fmt.Errorf("读取安装标识: %w", err)
	}
	sysID, err := s.systemIdentifier(ctx)
	if err != nil {
		return "", err
	}
	return licensekit.Fingerprint(installUUID, sysID), nil
}

// systemIdentifier 数据库实例的标识。
//
// 取 @@server_id：它是这个 MySQL 实例的身份，客户把库整个搬到另一个实例上
// （也就是"复制了一套安装"）时会变，而这正是指纹要捕捉的事。
//
// ⚠️ 取不到时**返回错误，不要退回 0**。退回 0 会让所有取不到的环境
// 算出同一个指纹 —— 那等于把防拷贝这道防线悄悄关掉，而且状态显示一切正常。
func (s *Store) systemIdentifier(ctx context.Context) (uint64, error) {
	var v uint64
	if err := s.DB.QueryRowContext(ctx, `SELECT @@server_id`).Scan(&v); err != nil {
		return 0, fmt.Errorf("读取数据库实例标识: %w", err)
	}
	return v, nil
}

// load 取当前授权。没有授权时返回 nil, nil（不是错误）。
func (s *Store) load(ctx context.Context) (*row, error) {
	var r row
	var mismatch sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT token, mismatch_since, updated_at FROM licenses WHERE id=1`).
		Scan(&r.Token, &mismatch, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// "没有授权"和"查不出来"是两件事，调用方要能分开处理：
		// 前者退回未激活是对的，后者退回未激活就是把一次数据库抖动
		// 变成了全公司的授权吊销
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
		`UPDATE licenses SET mismatch_since=? WHERE id=1 AND mismatch_since IS NULL`, at)
	return err
}

// Activate 写入一份新授权。验签在调用方做，这里只负责落库。
//
// 换授权时把 mismatch_since 清空：新授权是照着当前指纹签的，
// 上一份的"何时开始不匹配"对它没有意义，留着会让新授权一装上就已经在宽限期里。
func (s *Store) Activate(ctx context.Context, token, by string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO licenses (id, token, mismatch_since, updated_by) VALUES (1, ?, NULL, ?)
		 ON DUPLICATE KEY UPDATE token=VALUES(token), mismatch_since=NULL, updated_by=VALUES(updated_by)`,
		token, by)
	return err
}

// noteMismatch 在**首次**发现指纹不匹配时把时刻落库，返回该用于判定的 mismatchSince。
//
// # 为什么两条装载路径都要走它
//
// 启动时的 Reload 会调，运行中由 Watch 触发的 Fetch 也会调。
// 早先只有 Reload 记这一笔，于是运行期间发生的迁库（主从切换、
// 库恢复到新实例）虽然被 Watch 感知到了，却没有人记下"何时开始不匹配"——
// 宽限期退回「从到期日起算」的宽松旧规则，长周期 license 上等于没绑定。
//
// # 为什么直接比指纹，而不是问 Manager 的状态
//
// 这一步发生在 LoadAt 之前，那时内核还没有判过。三个条件与内核 evaluate
// 里的完全一致（签发时绑了、当前算得出、两者不等），改其中一处必须改另一处。
func (s *Store) noteMismatch(ctx context.Context, p *licensekit.Payload, fp string, known time.Time) time.Time {
	if p == nil || p.InstallID == "" || fp == "" || p.InstallID == fp || !known.IsZero() {
		return known
	}
	now := time.Now()
	if err := s.markMismatch(ctx, now); err != nil {
		// 落不上就是防线没建起来，必须显式告警 ——
		// 静默失败的话，状态照常显示"宽限期内"，而这个宽限永远不会结束
		logx.J("license", "mark_mismatch_failed", map[string]any{
			"err":  err.Error(),
			"note": "首次指纹不匹配的时刻没能落库，宽限期会在每次重启后重新开始计算",
		})
		return known // 保持零值，下个周期还会再试
	}
	logx.J("license", "fingerprint_mismatch", map[string]any{
		"since": now.Format(time.RFC3339),
		"note": "安装指纹与授权不符（换库/迁移/复制部署），已进入 " +
			fmt.Sprintf("%d", licensekit.FingerprintGraceDays) + " 天宽限期",
	})
	return now
}

// Reload 从库里装载一次授权到内存，并在首次发现指纹不匹配时落库。
//
// 启动时调一次，之后由 Watch 周期性调用。
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
		// 验签失败 = **未激活**，不是"过期"。
		// 说成过期会把客户引去续费，而真正的问题是这串码本身不对
		// （粘贴时少了一段、换了签发环境、文件被改过）。
		mgr.Clear()
		logx.J("license", "verify_failed", map[string]any{
			"err":  err.Error(),
			"note": "授权串验签不通过，按未激活处理（不是过期）",
		})
		return nil
	}

	// 先落库再装载。顺序不能反 —— 反过来的话，首次发现不匹配的**那一次**
	// LoadAt 拿到的还是零值，宽限期按旧规则（从到期日起算）判，
	// 要等下一轮才用上真值。
	mismatchSince := s.noteMismatch(ctx, p, fp, r.MismatchSince)

	// ⚠️ 必须用 LoadAt 而不是 Load：Load 会退回"从到期日起算宽限期"的旧规则，
	// 那条路径下进程每重启一次宽限期就重置一次，§4 的防拷贝形同虚设。
	mgr.LoadAt(p, fp, mismatchSince)
	return nil
}

// ---------------------------------------------------------------------------
// 多副本收敛（LICENSING §12 / §13.4）
// ---------------------------------------------------------------------------

// dbSource 让 licensekit.Watch 能从数据库探测授权变化。
//
// 拆成 Revision / Fetch 两步是为了让轮询便宜：Revision 每周期都调，
// Fetch 只在 Revision 变了才调。
type dbSource struct {
	store *Store
	mgr   *Manager
}

// Revision 单行表的 updated_at **加上当前安装指纹**。两个主键级查询，够廉价。
//
// 空串 = 没有授权，Watch 会据此调 Clear()。
//
// ⚠️ 指纹必须算进来。只跟 updated_at 的话，客户把库恢复到新实例
// （server_id 变了 → 指纹变了）时 license 行一个字节都没动，
// revision 不变、Fetch 不会被调用，于是所有正在跑的副本继续认为一切正常，
// 直到某次重启才发现 —— 而那可能是几个月之后。
func (s dbSource) Revision(ctx context.Context) (string, error) {
	var v sql.NullTime
	err := s.store.DB.QueryRowContext(ctx, `SELECT updated_at FROM licenses WHERE id=1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	fp, err := s.store.Fingerprint(ctx)
	if err != nil {
		// 算不出指纹是真故障（库连不上）。返回错误让 Watch 保持原状，
		// 绝不能退回一个"没有指纹"的 revision —— 那会被当成一次真实变化，
		// 进而用空指纹去装载，把指纹校验整个跳过（见内核 evaluate 的条件）
		return "", err
	}
	return v.Time.Format(time.RFC3339Nano) + "|" + fp, nil
}

// Fetch 取完整授权。只在 Revision 变化时被调用。
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
		// nil payload = **确认没有授权**（被删了），Watch 会 Clear。
		// 与"查不出来"（上面返回 error）严格区分：后者只会保持原状。
		return nil, fp, time.Time{}, nil
	}
	p, err := licensekit.VerifyEmbedded(r.Token)
	if err != nil {
		// 验签失败也是"确认没有有效授权"，不是查询故障，所以返回 nil payload
		// 而不是 error —— 返回 error 会让副本停在旧的有效状态，
		// 于是"换了一串无效的码"这件事在部分副本上永远不生效。
		return nil, fp, time.Time{}, nil
	}
	// 与 Reload 走同一条落库路径：运行中发生的迁库由这里感知，
	// 不记下首次不匹配的时刻，指纹宽限期就会退回宽松的旧规则。
	return p, fp, s.store.noteMismatch(ctx, p, fp, r.MismatchSince), nil
}

// StartWatch 起一个 goroutine 把本副本收敛到库里的最新授权。
//
// 为什么需要：管理员把激活码粘进 A 副本，A 更新了自己的内存状态，
// 而 B、C 还停在旧状态直到各自重启 —— 现象是「激活了但一半请求仍报未授权」，
// 刷新几次好几次不好，极难排查。
//
// 20s 间隔对应「激活后 60s 内全部 Pod 生效」的验收标准，留了充足余量。
func (s *Store) StartWatch(ctx context.Context, mgr *Manager) {
	go mgr.Watch(ctx, dbSource{store: s, mgr: mgr}, 20*time.Second)
}
