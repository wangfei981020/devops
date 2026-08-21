package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/evidence"
	"ops-sso-backend/internal/store"
)

// ══════════════════════════════════════════════════════════════════
// 审计锚点
// ══════════════════════════════════════════════════════════════════
//
// # 锚点解决的是哈希链解决不了的那件事
//
// 链能发现"改一条、删一条、插一条"。它防不住**从某点起整条重算** ——
// 有库权限的人把 audit_logs 全部重写一遍，链自洽，校验照样通过。
//
// 锚点把某一时刻的链头签下来。之后再想重算，那个时刻之前的部分
// 会和已签的 (seq, hash) 对不上 —— 除非他连签名也伪造，而那需要私钥。
//
// # ⚠️ 锚点留在库里等于没锚
//
// 能重写链的人同样能删改 audit_anchors。所以**必须把锚点导出到系统之外**
// （对象存储、邮件、工单、第三方存证），这一步现在是人工的：
// 导出接口给一份可离线核验的 JSON，导到哪由客户决定。
// 界面上要把这句话说清楚 —— 一个存在本库里的锚点，
// 给人的是"已经有防护了"的错觉，而它一点防护都没有。

func anchorJSON(a evidence.Anchor, chainHash string, chainKnown bool) gin.H {
	h := gin.H{
		"seq": a.Seq, "hash": a.Hash, "created_at": a.CreatedAt,
		"public_key": a.PublicKey,
		// 签名本身是否有效：能证明"这个锚点是我们签的、没被改过"
		"signature_ok": evidence.VerifyAnchor(a) == nil,
	}
	// 和**当前链**对不对得上：这一项才是能发现"整链被重算"的那个信号。
	// 签名有效 + 对不上 = 链在锚定之后被重写过。
	if chainKnown {
		h["matches_chain"] = chainHash == a.Hash
	}
	return h
}

// listAnchors 锚点清单，逐条核验。
func listAnchors(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()
	tctx := store.WithTenant(ctx, store.TenantID(identity(c).TenantID))

	rows, err := d.Store.Raw().QueryContext(tctx,
		`SELECT seq, hash, signature, public_key, created_at, exported_to
		 FROM audit_anchors WHERE tenant_id = ? ORDER BY seq DESC LIMIT 100`,
		int64(identity(c).TenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		a          evidence.Anchor
		exportedTo string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.a.Seq, &r.a.Hash, &r.a.Signature, &r.a.PublicKey,
			&r.a.CreatedAt, &r.exportedTo); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 逐个锚点去问"当时那个 seq 现在的哈希是多少"。
	// 用 IN 一次查完，锚点最多 100 条，不做 N+1。
	chain := map[int64]string{}
	if len(list) > 0 {
		ids := make([]int64, 0, len(list))
		for _, r := range list {
			ids = append(ids, r.a.Seq)
		}
		ph, args := placeholders(ids)
		q, err := d.Store.Tenant(ctx)
		if err != nil {
			return nil, err
		}
		cr, err := q.Query(`SELECT chain_seq, entry_hash FROM audit_logs
			WHERE tenant_id = ? AND chain_seq IN`+ph, args...)
		if err != nil {
			return nil, err
		}
		defer cr.Close()
		for cr.Next() {
			var seq int64
			var hash string
			if err := cr.Scan(&seq, &hash); err != nil {
				return nil, err
			}
			chain[seq] = hash
		}
		if err := cr.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		h, known := chain[r.a.Seq]
		j := anchorJSON(r.a, h, known)
		// 锚点对应的那条记录已经不在了 —— 那本身就是个信号：
		// 要么被删了，要么链被整体重写过。不能显示成"暂时查不到"。
		j["entry_missing"] = !known
		j["exported_to"] = r.exportedTo
		out = append(out, j)
	}
	return gin.H{
		"items": out, "total": len(out),
		// 没配私钥时锚点根本签不出来。界面要据此说"没在锚定"，
		// 而不是显示一个空列表让人以为"暂时还没到时间"。
		"signing_enabled": d.AnchorKey != nil,
	}, nil
}

// createAnchor 立刻签一个锚点。
//
// 定时任务每 6 小时签一次；这个按钮给的是"我现在要一个" ——
// 比如刚做完一批敏感变更，想立刻把这一刻钉死。
func createAnchor(c *gin.Context, d Deps) (any, error) {
	if d.AnchorKey == nil {
		return nil, apierr.New(http.StatusPreconditionFailed, apierr.CodeAnchorNoKey, nil)
	}
	n, err := anchorChain(c.Request.Context(), d)
	if err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "audit.anchor", ObjectType: "audit", ClientIP: clientIP(c),
		Detail: map[string]any{"created": n},
	})
	// created=0 是正常的：链头没动过就不重复签，同一个 seq 签十次没有意义。
	// 但要如实返回，别让界面显示"已签发"而实际上什么都没发生。
	return gin.H{"created": n}, nil
}

// exportAnchors 导出成可离线核验的文件。
//
// **这一步是锚点唯一真正起作用的地方**：把它存到本系统之外去。
// 存在库里的锚点，能重写链的人一样能改。
func exportAnchors(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()
	rows, err := d.Store.Raw().QueryContext(ctx,
		`SELECT seq, hash, signature, public_key, created_at
		 FROM audit_anchors WHERE tenant_id = ? ORDER BY seq`, int64(identity(c).TenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []gin.H{}
	for rows.Next() {
		var a evidence.Anchor
		if err := rows.Scan(&a.Seq, &a.Hash, &a.Signature, &a.PublicKey, &a.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, gin.H{
			"seq": a.Seq, "hash": a.Hash, "signature": a.Signature,
			"public_key": a.PublicKey, "created_at": a.CreatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	d.Audit.Write(ctx, auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "audit.anchor_export", ObjectType: "audit", ClientIP: clientIP(c),
		Detail: map[string]any{"count": len(items)},
	})

	return gin.H{
		"product":     "ops-access-plane",
		"tenant_id":   int64(identity(c).TenantID),
		"exported_at": time.Now(),
		"anchors":     items,
		// 核验方法写进文件里：三个月后拿到这个文件的人，
		// 不该还要去翻我们的文档才知道怎么用。
		"howto": "每条锚点用 public_key 验 signature（Ed25519，消息为 " +
			`"oap-anchor/v1|<seq>|<hash>|<created_at unix 秒>"）。` +
			"验过之后，把 hash 和系统里第 seq 条审计的 entry_hash 比对：" +
			"对不上说明那条锚点之后链被重写过。",
	}, nil
}
