package api

import (
	"context"
	"database/sql"
	"time"

	"ops-sso-backend/internal/domain/evidence"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

// auditEntry 一条控制台操作审计。
//
// 与 access_events（网关的每次访问判定）分开：
// 前者是"谁改了配置"，后者是"谁访问了什么"。混在一张表里，
// 查配置变更时会被上百万条访问记录淹没。
type auditEntry struct {
	TenantID   int64
	ActorID    int64
	ActorName  string
	Action     string // 形如 policy.create / group.delete，不是中文
	ObjectType string
	ObjectID   int64
	ClientIP   string
	RequestID  string
	Detail     map[string]any
}

// Auditor 审计写入，带哈希链。
type Auditor struct{ st *store.Store }

func NewAuditor(st *store.Store) *Auditor { return &Auditor{st: st} }

// Write 写一条审计，并挂到该租户的哈希链上。
//
// # 为什么整个过程在一个事务里
//
// 取链头 → 算哈希 → 写记录 → 推进链头，这四步必须原子。
// 不然两个并发写会读到同一个链头，各自算出的 prev 相同 —— 链就叉了，
// 之后每次校验都会报断裂，而实际上没人篡改过任何东西。
//
// # 写失败只记日志，不阻断业务
//
// 审计写不进去时让用户的操作也失败，会把一个次要故障放大成主流程不可用。
// 但必须留下痕迹，否则就成了静默丢失 —— 那比不做审计更糟。
func (a *Auditor) Write(ctx context.Context, e auditEntry) {
	if a == nil || a.st == nil {
		return
	}
	if err := a.writeChained(ctx, e); err != nil {
		logx.Line("audit", "写审计失败（业务未受影响）: "+err.Error()+" action="+e.Action)
	}
}

func (a *Auditor) writeChained(ctx context.Context, e auditEntry) error {
	tx, err := a.st.Raw().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 行锁串行化同一租户的写入。SELECT ... FOR UPDATE 在这里不是性能问题：
	// 配置变更是低频操作，几十毫秒的串行代价换一条不会叉的链，很划算。
	var lastSeq int64
	var lastHash string
	err = tx.QueryRowContext(ctx,
		`SELECT last_seq, last_hash FROM audit_chain_heads WHERE tenant_id = ? FOR UPDATE`,
		e.TenantID).Scan(&lastSeq, &lastHash)
	if err == sql.ErrNoRows {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO audit_chain_heads (tenant_id, last_seq, last_hash) VALUES (?, 0, ?)`,
			e.TenantID, evidence.GenesisHash); err != nil {
			return err
		}
		lastSeq, lastHash = 0, evidence.GenesisHash
	} else if err != nil {
		return err
	}
	if lastHash == "" {
		lastHash = evidence.GenesisHash
	}

	// ★ 必须截断到秒。
	//
	// audit_logs.created_at 是 DATETIME（无小数秒），而 evidence.Canonical
	// 用的是 UnixNano。不截断的话，写入时按纳秒算哈希、读回来只剩秒 ——
	// 重算出的哈希必然不同，于是**每一条记录都会被报成"内容被改过"**。
	// 一个天天喊狼来了的防篡改机制，比没有还糟：人会直接不看它。
	at := time.Now().Truncate(time.Second)
	seq := lastSeq + 1
	fields := auditFields(e)
	hash := evidence.Next(lastHash, seq, at, fields)

	var detail any
	if e.Detail != nil {
		// 存规范化后的形式：库里看到的和参与哈希的是同一份内容。
		// （MySQL 的 JSON 列还会再规范化一次，所以校验侧也要规范化后再比 ——
		//  见 evidence.CanonicalJSON 的注释。）
		detail = evidence.CanonicalJSON(e.Detail)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs
		(tenant_id, actor_id, actor_name, action, object_type, object_id, detail,
		 request_id, client_ip, created_at, chain_seq, prev_hash, entry_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TenantID, e.ActorID, e.ActorName, e.Action, e.ObjectType, e.ObjectID, detail,
		e.RequestID, e.ClientIP, at, seq, lastHash, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE audit_chain_heads SET last_seq = ?, last_hash = ? WHERE tenant_id = ?`,
		seq, hash, e.TenantID); err != nil {
		return err
	}
	return tx.Commit()
}

// auditFields 参与哈希的字段集合。
//
// **改这个函数会让历史记录全部校验不过** —— 因为老记录是按老字段集算的哈希。
// 真要改，必须走"新版本从某个 seq 起用新算法"的方式，并在校验时按 seq 分段。
// 直接改的后果是：某天有人点了"校验完整性"，看到"全部断裂"。
func auditFields(e auditEntry) map[string]any {
	f := map[string]any{
		"actor_id":    e.ActorID,
		"actor_name":  e.ActorName,
		"action":      e.Action,
		"object_type": e.ObjectType,
		"object_id":   e.ObjectID,
		"client_ip":   e.ClientIP,
	}
	if e.Detail != nil {
		// ⚠️ 必须是规范化形式，不能是 json.Marshal 的原始输出 ——
		// 那份字节存进 MySQL 的 JSON 列后会被重排，读回来就对不上了。
		f["detail"] = evidence.CanonicalJSON(e.Detail)
	}
	return f
}

// VerifyChain 校验某租户的审计链。
//
// 返回**具体断在哪一条**，而不是笼统的"校验失败"——
// 后者除了让人心慌之外没有任何用处。
func (a *Auditor) VerifyChain(ctx context.Context, tenantID int64, limit int) (evidence.VerifyResult, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	// 按 seq 升序取最近一段。整链校验在记录多了以后会很慢，
	// 所以默认只校验最近一段 + 依赖锚点保证更早的部分
	rows, err := a.st.Raw().QueryContext(ctx, `SELECT chain_seq, created_at, prev_hash, entry_hash,
		       actor_id, actor_name, action, object_type, object_id, client_ip, detail
		FROM (
		  SELECT * FROM audit_logs WHERE tenant_id = ? AND chain_seq > 0
		  ORDER BY chain_seq DESC LIMIT ?
		) t ORDER BY chain_seq ASC`, tenantID, limit)
	if err != nil {
		return evidence.VerifyResult{}, err
	}
	defer rows.Close()

	var entries []evidence.Entry
	for rows.Next() {
		var (
			seq, actorID, objID        int64
			at                         time.Time
			prev, hash                 string
			actor, action, objType, ip string
			detail                     sql.NullString
		)
		if err := rows.Scan(&seq, &at, &prev, &hash, &actorID, &actor, &action,
			&objType, &objID, &ip, &detail); err != nil {
			return evidence.VerifyResult{}, err
		}
		f := map[string]any{
			"actor_id": actorID, "actor_name": actor, "action": action,
			"object_type": objType, "object_id": objID, "client_ip": ip,
		}
		if detail.Valid {
			// 读回来的是 MySQL 规范化过的字节（键按长度排序、带空格），
			// 必须重新规范化才能和写入时算哈希用的那份对上。
			f["detail"] = evidence.CanonicalJSONString(detail.String)
		}
		entries = append(entries, evidence.Entry{
			Seq: seq, At: at, Fields: f, PrevHash: prev, Hash: hash,
		})
	}
	return evidence.Verify(entries), rows.Err()
}
