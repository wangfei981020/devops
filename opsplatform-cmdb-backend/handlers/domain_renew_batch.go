package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 批量续费：粘一串域名进来，一次续完。
//
//	⚠️ 这是真金白银的操作，所以刻意做成**两步**：
//	  1. preview —— 只做匹配和体检，告诉你哪些能续、哪些域名库里没有、
//	     哪些数据源不支持写回，以及每个的当前到期日和预计续到哪天
//	  2. 执行 —— 用户看清楚了再点
//	一步到位的设计在这里是危险的：打错一个字母就会跳过某个该续的域名，
//	而"跳过"在批量结果里很容易被一眼扫过去。
//
//	执行时**串行**：并发打注册商 API 既可能触发限流，也让"到底扣了几笔钱"
//	在出错时更难查清。域名数量本来就不大，串行的代价可以接受。

// batchRenewMax 一次最多处理多少个域名。
// 不是技术限制，是安全阀：粘贴板一次贴进几百个域名再点确认，
// 出错时的代价太大。需要更多就分批做。
const batchRenewMax = 50

type batchRenewItem struct {
	Domain       string `json:"domain"`
	CIID         int64  `json:"ci_id,omitempty"`
	Status       string `json:"status"` // ok / not_found / unsupported / duplicated
	Reason       string `json:"reason,omitempty"`
	ExpiryBefore string `json:"expiry_before,omitempty"`
	ExpiryExpect string `json:"expiry_expect,omitempty"` // 预计续到（preview 用）
	ExpiryAfter  string `json:"expiry_after,omitempty"`  // 实际续到（执行后）
	OrderID      string `json:"order_id,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
	Env          string `json:"env,omitempty"`
	LedgerSaved  bool   `json:"ledger_saved,omitempty"`
}

// parseDomainList 从一坨文本里抽域名：换行/逗号/分号/空格都当分隔符。
// 顺手去空、转小写、剥掉可能被粘进来的协议头和末尾斜杠。
func parseDomainList(raw string) []string {
	repl := strings.NewReplacer("\r", "\n", ",", "\n", ";", "\n", "\t", "\n", " ", "\n")
	parts := strings.Split(repl.Replace(raw), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		d := strings.TrimSpace(strings.ToLower(p))
		d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
		d = strings.TrimSuffix(d, "/")
		if d == "" {
			continue
		}
		out = append(out, d)
	}
	return out
}

// resolveBatchDomains 把域名列表解析成待处理项：查 ci_id、查当前到期日、去重。
func (h *SyncHandler) resolveBatchDomains(names []string, period int) []batchRenewItem {
	seen := map[string]bool{}
	items := make([]batchRenewItem, 0, len(names))
	for _, name := range names {
		if seen[name] {
			// 重复的只处理一次——同一个域名续两遍就是扣两笔钱
			items = append(items, batchRenewItem{Domain: name, Status: "duplicated",
				Reason: "列表里重复出现，只会处理第一次"})
			continue
		}
		seen[name] = true

		var ciID int64
		var expiry sql.NullString
		err := h.DB.QueryRow(`SELECT c.id, DATE_FORMAT(d.expiry_at,'%Y-%m-%d')
			FROM cis c JOIN domains d ON d.ci_id = c.id
			WHERE c.type='domain' AND LOWER(c.name)=?`, name).Scan(&ciID, &expiry)
		if err != nil {
			items = append(items, batchRenewItem{Domain: name, Status: "not_found",
				Reason: "域名台账里没有这个域名（检查拼写，或先同步/录入）"})
			continue
		}

		it := batchRenewItem{Domain: name, CIID: ciID, Status: "ok",
			ExpiryBefore: expiry.String, ExpiryExpect: addYearsDate(expiry.String, period)}

		// 能不能写回厂商：数据源没配写凭据的话，续费根本发不出去，
		// 与其到执行时才一个个失败，不如在预览阶段就标出来
		if _, _, _, _, err := h.writeAdapterForDomain(fmt.Sprint(ciID)); err != nil {
			it.Status = "unsupported"
			it.Reason = err.Error()
		}
		items = append(items, it)
	}
	return items
}

// PreviewBatchRenew POST /domains/renew-batch/preview
// body {domains: "多行文本或数组", period}
func (h *SyncHandler) PreviewBatchRenew(c *gin.Context) {
	var in struct {
		Domains any `json:"domains"`
		Period  int `json:"period"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：" + err.Error()})
		return
	}
	if in.Period < 1 || in.Period > 10 {
		in.Period = 1 // 预览阶段容错：只影响"预计续到"的展示，不扣费
	}
	names := normalizeDomainsInput(in.Domains)
	if len(names) == 0 {
		c.JSON(400, gin.H{"error": "没有解析出任何域名"})
		return
	}
	over := 0
	if len(names) > batchRenewMax {
		over = len(names) - batchRenewMax
		names = names[:batchRenewMax]
	}

	items := h.resolveBatchDomains(names, in.Period)
	okCount := 0
	for _, it := range items {
		if it.Status == "ok" {
			okCount++
		}
	}
	out := gin.H{"items": items, "total": len(items), "renewable": okCount, "period": in.Period}
	if over > 0 {
		// 静默截断等于骗人：多出来的那些用户以为也会续
		out["truncated"] = over
		out["warning"] = fmt.Sprintf("一次最多处理 %d 个域名，超出的 %d 个未纳入本次，请分批操作", batchRenewMax, over)
	}
	c.JSON(200, out)
}

// normalizeDomainsInput 兼容两种入参：一坨文本，或者已经切好的数组
func normalizeDomainsInput(v any) []string {
	switch x := v.(type) {
	case string:
		return parseDomainList(x)
	case []any:
		var buf []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				buf = append(buf, parseDomainList(s)...)
			}
		}
		return buf
	}
	return nil
}

// BatchRenewDomains POST /domains/renew-batch
// body {domains, period, quoted_currency, quoted_amount, confirm_count}
//
//	confirm_count 是前端预览时看到的可续数量，服务端会核对：
//	对不上说明台账在预览之后变过（有人加/删了域名），此时拒绝执行，
//	让用户重新预览确认——避免"我以为在续 3 个，实际续了 8 个"。
func (h *SyncHandler) BatchRenewDomains(c *gin.Context) {
	var in struct {
		Domains        any     `json:"domains"`
		Period         int     `json:"period"`
		QuotedAmount   float64 `json:"quoted_amount"`
		QuotedCurrency string  `json:"quoted_currency"`
		ConfirmCount   int     `json:"confirm_count"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：" + err.Error()})
		return
	}
	// 和单个续费一样：绝不静默默认续 1 年
	if in.Period < 1 || in.Period > 10 {
		c.JSON(400, gin.H{"error": "请指定续费年数（1-10 年）"})
		return
	}
	names := normalizeDomainsInput(in.Domains)
	if len(names) == 0 {
		c.JSON(400, gin.H{"error": "没有解析出任何域名"})
		return
	}
	if len(names) > batchRenewMax {
		c.JSON(400, gin.H{"error": fmt.Sprintf("一次最多续费 %d 个域名，当前 %d 个，请分批操作", batchRenewMax, len(names))})
		return
	}

	items := h.resolveBatchDomains(names, in.Period)
	renewable := 0
	for _, it := range items {
		if it.Status == "ok" {
			renewable++
		}
	}
	if renewable == 0 {
		c.JSON(400, gin.H{"error": "没有可续费的域名", "items": items})
		return
	}
	if in.ConfirmCount > 0 && in.ConfirmCount != renewable {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("可续费数量已变化（确认时 %d 个，现在 %d 个），请重新预览后再执行", in.ConfirmCount, renewable),
			"items": items,
		})
		return
	}

	logx.JCtx(c.Request.Context(), "domain_renew", "batch_start", map[string]any{
		"count": renewable, "period": in.Period, "operator": currentUser(c)})

	succeeded, failed := 0, 0
	for i := range items {
		if items[i].Status != "ok" {
			continue
		}
		// 每个域名独立超时：一个卡住不该把后面的都拖死
		ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
		res := h.renewOne(ctx, c, items[i].CIID, items[i].Domain, in.Period, in.QuotedCurrency, in.QuotedAmount)
		cancel()

		if res.Err != "" {
			items[i].Status = "failed"
			items[i].Reason = res.Err
			failed++
			continue
		}
		items[i].Status = "renewed"
		items[i].OrderID = res.OrderID
		items[i].ExpiryAfter = res.ExpiryAfter
		items[i].DryRun = res.DryRun
		items[i].Env = res.Env
		items[i].LedgerSaved = res.LedgerSaved
		succeeded++
	}

	logx.JCtx(c.Request.Context(), "domain_renew", "batch_done", map[string]any{
		"succeeded": succeeded, "failed": failed, "period": in.Period})
	SetAuditTarget(c, fmt.Sprintf("批量续费 %d 年：成功 %d 个，失败 %d 个", in.Period, succeeded, failed))

	c.JSON(200, gin.H{
		"ok": failed == 0, "succeeded": succeeded, "failed": failed,
		"items": items,
		"msg":   fmt.Sprintf("续费完成：成功 %d 个，失败 %d 个", succeeded, failed),
	})
}
