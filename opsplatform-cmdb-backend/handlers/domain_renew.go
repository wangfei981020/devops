package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 域名续费 / 自动续费（写回 GoDaddy）。续费⚠️会真实扣费——UI 二次确认 + 尊重数据源 dry_run（预演不真扣）。
// 全链路日志：每个失败分支都打 [域名续费] 标签，方便生产排错。

// GodaddyDetail 取域名厂商侧到期/自动续费状态（续费弹窗打开时拉）。GET /domains/:ciid/godaddy-detail
func (h *SyncHandler) GodaddyDetail(c *gin.Context) {
	ciid := c.Param("ciid")
	wa, _, domain, _, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "detail_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	out := gin.H{"domain": domain, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "detail_ok": true}
	// 域名详情（到期/自动续费/隐私）——失败不整体报错，仍返回价，前端降级展示
	d, err := wa.GetDomainDetail(ctx, domain)
	if err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "detail_fail_degraded", map[string]any{"domain": domain, "error": err.Error()})
		out["detail_ok"] = false
	} else {
		out["renew_auto"] = d.RenewAuto
		out["privacy"] = d.Privacy
		out["status"] = d.Status
		if d.Expires != nil {
			out["expires"] = d.Expires.Format("2006-01-02")
		}
	}
	// 续费价（估算，查不到返回零值不阻断）
	if p, _ := wa.GetRenewalPrice(ctx, domain); p.AmountMicro > 0 {
		out["price_per_year"] = float64(p.AmountMicro) / 1_000_000.0
		out["currency"] = p.Currency
	}
	c.JSON(200, out)
}

// RenewDomain 续费。POST /domains/:ciid/renew  body {period, quoted_amount, quoted_currency}
// 续费后落 domain_renewals 记录（报价/订单号/到期前后/操作人），防超付可查。
func (h *SyncHandler) RenewDomain(c *gin.Context) {
	ciid := c.Param("ciid")
	var in struct {
		Period         int     `json:"period"`
		QuotedAmount   float64 `json:"quoted_amount"`
		QuotedCurrency string  `json:"quoted_currency"`
	}
	_ = c.ShouldBindJSON(&in)
	if in.Period <= 0 {
		in.Period = 1
	}
	if in.Period > 10 {
		c.JSON(400, gin.H{"error": "续费年数最多 10 年"})
		return
	}
	wa, _, domain, _, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "renew_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ciID, _ := parseID(ciid)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()

	// 续费前到期日（防超付：与续费后对比，看年数是否只前进了所选 period）
	var expiryBefore sql.NullString
	_ = h.DB.QueryRow(`SELECT DATE_FORMAT(expiry_at,'%Y-%m-%d') FROM domains WHERE ci_id=?`, ciID).Scan(&expiryBefore)

	logx.JCtx(c.Request.Context(), "domain_renew", "renew_start", map[string]any{"domain": domain, "period": in.Period, "quoted_currency": in.QuotedCurrency, "quoted_amount": in.QuotedAmount, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	res, err := wa.RenewDomain(ctx, domain, in.Period)
	if err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "renew_fail", map[string]any{"domain": domain, "period": in.Period, "error": err.Error()})
		c.JSON(http.StatusBadGateway, gin.H{"error": "续费失败：" + err.Error()})
		return
	}
	// 真续成功后拉最新到期日刷库（dry_run 不改厂商，跳过刷库）
	var expiryAfter sql.NullString
	if !wa.DryRun() {
		if d, e := wa.GetDomainDetail(ctx, domain); e == nil && d.Expires != nil {
			expiryAfter = sql.NullString{String: d.Expires.Format("2006-01-02"), Valid: true}
			logExec(h.DB, "续费刷到期", `UPDATE domains SET expiry_at=? WHERE ci_id=?`, *d.Expires, ciID)
		} else if e != nil {
			logx.JCtx(c.Request.Context(), "domain_renew", "renew_refresh_expiry_fail", map[string]any{"domain": domain, "error": e.Error()})
		}
	}
	dry := 0
	if wa.DryRun() {
		dry = 1
	}
	// 落续费记录台账
	logExec(h.DB, "续费记录", `INSERT INTO domain_renewals
		(domain_ci_id, domain, period, quoted_currency, quoted_amount, order_id, expiry_before, expiry_after, operator, env, dry_run, raw_resp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ciID, domain, in.Period, in.QuotedCurrency, in.QuotedAmount, res.OrderID,
		nullableStr(expiryBefore), nullableStr(expiryAfter), currentUser(c), wa.EnvLabel(), dry, res.RawBody)

	WriteAudit(h.DB, c, "domain_renew", fmt.Sprintf("%s %d年 order=%s", domain, in.Period, res.OrderID))
	logx.JCtx(c.Request.Context(), "domain_renew", "renew_done", map[string]any{"domain": domain, "period": in.Period, "order_id": res.OrderID, "expiry_before": expiryBefore.String, "expiry_after": expiryAfter.String, "env": wa.EnvLabel(), "dry_run": wa.DryRun()})
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"order_id": res.OrderID, "expiry_before": expiryBefore.String, "expiry_after": expiryAfter.String,
		"msg": renewMsg(wa, domain, in.Period)})
}

// nullableStr 把 NullString 转为可写入的值（无效→nil）。
func nullableStr(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}

// ListRenewals 续费记录历史。GET /renewals?domain_ci_id=&limit=&offset=
func (h *SyncHandler) ListRenewals(c *gin.Context) {
	where := []string{"1=1"}
	args := []any{}
	if v := c.Query("domain_ci_id"); v != "" {
		where = append(where, "domain_ci_id=?")
		args = append(args, v)
	}
	cond := strings.Join(where, " AND ")
	var total int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM domain_renewals WHERE `+cond, args...).Scan(&total)
	limit := 20
	if l, e := strconv.Atoi(c.Query("limit")); e == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, e := strconv.Atoi(c.Query("offset")); e == nil && o > 0 {
		offset = o
	}
	rows, err := h.DB.Query(`SELECT id, domain, period, quoted_currency, quoted_amount, order_id,
		DATE_FORMAT(expiry_before,'%Y-%m-%d'), DATE_FORMAT(expiry_after,'%Y-%m-%d'),
		operator, env, dry_run, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM domain_renewals WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type row struct {
		ID           int64   `json:"id"`
		Domain       string  `json:"domain"`
		Period       int     `json:"period"`
		Currency     string  `json:"quoted_currency"`
		Amount       float64 `json:"quoted_amount"`
		OrderID      string  `json:"order_id"`
		ExpiryBefore string  `json:"expiry_before"`
		ExpiryAfter  string  `json:"expiry_after"`
		Operator     string  `json:"operator"`
		Env          string  `json:"env"`
		DryRun       int     `json:"dry_run"`
		CreatedAt    string  `json:"created_at"`
	}
	list := []row{}
	for rows.Next() {
		var r row
		var eb, ea sql.NullString
		if rows.Scan(&r.ID, &r.Domain, &r.Period, &r.Currency, &r.Amount, &r.OrderID, &eb, &ea,
			&r.Operator, &r.Env, &r.DryRun, &r.CreatedAt) != nil {
			continue
		}
		r.ExpiryBefore = eb.String
		r.ExpiryAfter = ea.String
		list = append(list, r)
	}
	c.JSON(200, gin.H{"total": total, "items": list})
}

// SetAutoRenew 开/关自动续费（不扣费）。POST /domains/:ciid/auto-renew  body {enabled}
func (h *SyncHandler) SetAutoRenew(c *gin.Context) {
	ciid := c.Param("ciid")
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	wa, _, domain, _, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "autorenew_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	logx.JCtx(c.Request.Context(), "domain_renew", "autorenew_start", map[string]any{"enabled": in.Enabled, "domain": domain, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	if err := wa.SetAutoRenew(ctx, domain, in.Enabled); err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "autorenew_fail", map[string]any{"domain": domain, "enabled": in.Enabled, "error": err.Error()})
		c.JSON(http.StatusBadGateway, gin.H{"error": "设置自动续费失败：" + err.Error()})
		return
	}
	WriteAudit(h.DB, c, "domain_auto_renew", domain)
	state := "开启"
	if !in.Enabled {
		state = "关闭"
	}
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"msg": prefixDry(wa, "已"+state+"自动续费（"+wa.EnvLabel()+"）")})
}

func renewMsg(wa interface {
	DryRun() bool
	EnvLabel() string
}, domain string, period int) string {
	return prefixDry(wa, fmt.Sprintf("已续费 %s %d 年（%s）", domain, period, wa.EnvLabel()))
}

func prefixDry(wa interface {
	DryRun() bool
	EnvLabel() string
}, msg string) string {
	if wa.DryRun() {
		return "【预演·未真发/未扣费】" + msg + " —— 关掉数据源 dry_run 才会真正执行"
	}
	return msg
}
