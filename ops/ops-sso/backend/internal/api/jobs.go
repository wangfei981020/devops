package api

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"time"

	"ops-sso-backend/internal/domain/evidence"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/internal/worker"
	"ops-sso-backend/logx"
)

// RegisterJobs 挂上全部后台任务。
//
// 每个任务都必须是**幂等**的：多副本下偶尔重复执行一次，结果不能变坏。
// 这是我们敢用"拿不到锁就跳过"这种轻量协调方式的前提。
func RegisterJobs(r *worker.Runner, d Deps) {
	r.Add(worker.Job{
		Name: "expire_grants", Interval: 30 * time.Second,
		Run: func(ctx context.Context) (int, error) { return expireGrants(ctx, d) },
	})
	r.Add(worker.Job{
		Name: "purge_auth_states", Interval: time.Hour,
		Run: func(ctx context.Context) (int, error) {
			if d.IdP == nil {
				return 0, nil
			}
			n, err := d.IdP.PurgeStates(ctx)
			return int(n), err
		},
	})
	r.Add(worker.Job{
		// 拨测执行器。醒得比拨测频率密，因为真正的频率由每条探针的
		// interval_sec 决定 —— 这里只负责"看谁到点了"。
		Name: "probe_run", Interval: probeTick,
		Run: func(ctx context.Context) (int, error) { return runProbes(ctx, d) },
	})
	r.Add(worker.Job{
		Name: "audit_anchor", Interval: 6 * time.Hour,
		Run: func(ctx context.Context) (int, error) { return anchorChain(ctx, d) },
	})
}

// expireGrants 到期回收临时提权。
//
// # 回收必须做三件事，缺一件"临时"就是假的
//
//  1. 标记为已过期（数据库状态）
//  2. **杀掉他的提权票据** —— 否则手里那张 30 分钟的票还能继续用
//  3. 写审计，注明是"到期自动回收"而不是人工收回
//
// 只做第 1 件是最常见的实现，也是最没用的：权限"收回"了，
// 人却还能接着操作到票据自然过期。
func expireGrants(ctx context.Context, d Deps) (int, error) {
	if d.Approval == nil {
		return 0, nil
	}
	// 后台任务没有请求上下文，租户要显式带上。
	// 目前只有默认租户；多租户上线后这里要遍历活跃租户列表。
	tctx := store.WithTenant(ctx, store.TenantID(1))

	now := time.Now()
	expired, err := d.Approval.Expired(tctx, now, 200)
	if err != nil {
		return 0, err
	}
	var n int
	for _, r := range expired {
		if err := d.Approval.MarkExpired(tctx, r.ID, "expired"); err != nil {
			logx.Line("worker", "回收提权失败 id="+itoa(r.ID)+": "+err.Error())
			continue
		}
		if d.MFA != nil {
			// 连带杀票据 —— 这一步漏了，前面那步就白做
			if killed, err := d.MFA.RevokeUserTickets(tctx, r.RequesterID); err == nil && killed > 0 {
				logx.Line("worker", "到期回收连带注销提权票据 "+itoa(killed)+" 张")
			}
		}
		d.Audit.Write(tctx, auditEntry{
			TenantID: 1, ActorID: 0, ActorName: "system",
			Action: "approval.expired", ObjectType: "access_request", ObjectID: r.ID,
			Detail: map[string]any{"requester_id": r.RequesterID, "app_id": r.AppID},
		})
		n++
	}
	return n, nil
}

// anchorChain 给审计链头签一个外部锚点。
//
// # 为什么必须做
//
// 哈希链能发现"改一条、删一条、插一条"，但防不住**整条链从某点起全部重算**。
// 锚点把某一时刻的链头用 Ed25519 签下来送出系统之外，
// 那个时刻之前的部分就再也改不动了 —— 这是链唯一能对抗整体重算的手段。
//
// # 私钥从哪来
//
// OAP_ANCHOR_KEY（十六进制种子）。没配就跳过，并**每次都记日志** ——
// 静默不签会让客户以为锚点在工作，直到真出事时才发现从来没签过。
func anchorChain(ctx context.Context, d Deps) (int, error) {
	if d.AnchorKey == nil {
		logx.Line("worker", "未配置 OAP_ANCHOR_KEY，审计锚点未签发（链仍可校验，但防不住整链重算）")
		return 0, nil
	}
	tctx := store.WithTenant(ctx, store.TenantID(1))

	var seq int64
	var hash string
	err := d.Store.Raw().QueryRowContext(tctx,
		`SELECT last_seq, last_hash FROM audit_chain_heads WHERE tenant_id = 1`).Scan(&seq, &hash)
	if err != nil || seq == 0 || hash == "" {
		return 0, nil // 还没有审计记录
	}

	// 链头没动过就不重复签：同一个 seq 签十次没有额外意义，
	// 只会让锚点表变成一堆噪音，真要查的时候反而难找
	var exists int
	_ = d.Store.Raw().QueryRowContext(tctx,
		`SELECT COUNT(*) FROM audit_anchors WHERE tenant_id = 1 AND seq = ?`, seq).Scan(&exists)
	if exists > 0 {
		return 0, nil
	}

	a := evidence.Sign(d.AnchorKey, seq, hash, time.Now())
	if _, err := d.Store.Raw().ExecContext(tctx, `INSERT INTO audit_anchors
		(tenant_id, seq, hash, signature, public_key) VALUES (1, ?, ?, ?, ?)`,
		a.Seq, a.Hash, a.Signature, a.PublicKey); err != nil {
		return 0, err
	}
	return 1, nil
}

// AnchorKeyFromHex 从十六进制种子生成 Ed25519 私钥。
func AnchorKeyFromHex(s string) ed25519.PrivateKey {
	seed, err := hex.DecodeString(s)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil
	}
	return ed25519.NewKeyFromSeed(seed)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
