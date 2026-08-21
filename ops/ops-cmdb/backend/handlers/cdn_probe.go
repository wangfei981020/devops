package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// CDN token 权限体检。
//
// ⚠️ 与其它所有 /api/cdn/* 接口不同：**这个接口实时调 Cloudflare，不读库**。
//
// 为什么必须实时：list_cdn_rules 等接口读的是 cdn_rules 表，那是上次同步落库的快照。
// 改完 CF 权限后去查它，看到的仍是旧的「明细获取失败: 403」，据此会得出
// 「权限没生效」的错误结论——本项目已经踩过一次。体检必须绕开快照。

// ProbeToken GET /api/cdn/token-check?zone=&account_id=
func (h *CDNHandler) ProbeToken(c *gin.Context) {
	zone := c.Query("zone")

	type acct struct {
		ID   int
		Name string
	}
	var accts []acct

	if id := c.Query("account_id"); id != "" {
		var a acct
		if err := h.DB.QueryRow(`SELECT a.id, COALESCE(d.name,'')
			FROM cdn_accounts a LEFT JOIN cdns d ON d.id=a.cdn_id WHERE a.id=?`, id).
			Scan(&a.ID, &a.Name); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusOK, gin.H{"ok": false, "error": "没有这个 CDN 账号: " + id, "error_key": "error.cdnAccountUnknown", "error_params": gin.H{"id": id}})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		accts = append(accts, a)
	} else {
		rows, err := h.DB.Query(`SELECT a.id, COALESCE(d.name,'')
			FROM cdn_accounts a LEFT JOIN cdns d ON d.id=a.cdn_id ORDER BY a.id`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var a acct
			if rows.Scan(&a.ID, &a.Name) == nil {
				accts = append(accts, a)
			}
		}
	}

	if len(accts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": "CMDB 里没有配置任何 CDN 账号——请先到「管理 → 数据源」新增 CDN 账号", "error_key": "error.noCdnAccount",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	results := make([]gin.H, 0, len(accts))
	for _, a := range accts {
		cli, err := h.clientFor(strconv.Itoa(a.ID))
		if err != nil {
			// 拿不到 client 本身就是一种结论（没配 token / 厂商不支持），
			// 不能静默跳过——否则看起来像「这个账号没问题」。
			results = append(results, gin.H{
				"account_id": a.ID, "provider": a.Name,
				"ok": false, "error": err.Error(),
			})
			continue
		}
		p := cli.ProbeToken(ctx, zone)
		results = append(results, gin.H{
			"account_id": a.ID, "provider": a.Name, "ok": true, "probe": p,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"realtime": true,
		"note": "本结果为实时探测 Cloudflare 所得，不读 CMDB 库。" +
			"若要让 list_cdn_rules 等查询接口也反映新权限，体检通过后还需触发一次 CDN 账号同步。",
		"accounts": results,
	})
}
