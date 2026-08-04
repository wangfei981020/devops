package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/dnsource"
	"opsplatform-cmdb-backend/logx"
)

// renewInFlight 按域名 ci_id 的续费互斥：防双击/重试并发导致重复扣费。
var renewInFlight sync.Map

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
		logx.JCtx(ctx, "domain_renew", "detail_fail_degraded", map[string]any{"domain": domain, "error": err.Error()})
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
	// 畸形/空 body 直接拒，绝不静默默认续 1 年（真金白银）
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：" + err.Error()})
		return
	}
	if in.Period < 1 || in.Period > 10 {
		c.JSON(400, gin.H{"error": "请指定续费年数（1-10 年）"})
		return
	}
	ciID, _ := parseID(ciid)
	// 防重：同域名续费串行化，双击/重试第二个请求直接拒（避免重复扣费）
	if _, busy := renewInFlight.LoadOrStore(ciID, true); busy {
		c.JSON(http.StatusConflict, gin.H{"error": "该域名正在续费中，请勿重复提交"})
		return
	}
	defer renewInFlight.Delete(ciID)

	_, _, domain, _, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "domain_renew", "renew_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()

	r := h.renewOne(ctx, currentUser(c), ciID, domain, in.Period, in.QuotedCurrency, in.QuotedAmount)
	if r.Err != "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": r.Err})
		return
	}
	SetAuditTarget(c, fmt.Sprintf("%s %d年 order=%s", domain, in.Period, r.OrderID))
	out := gin.H{"ok": true, "dry_run": r.DryRun, "env": r.Env,
		"order_id": r.OrderID, "expiry_before": r.ExpiryBefore, "expiry_after": r.ExpiryAfter,
		"ledger_saved": r.LedgerSaved, "msg": r.Msg}
	if !r.LedgerSaved {
		out["warning"] = fmt.Sprintf("续费已成功但台账写入失败，请人工补录：域名 %s 订单 %s %d年", domain, r.OrderID, in.Period)
	}
	c.JSON(200, out)
}

// renewOneResult 一次续费的结果。Err 非空表示这一个失败了，
// 批量场景下继续处理下一个而不是整体中断。
type renewOneResult struct {
	// Uncertain=true：厂商响应没拿到，但回查确认已扣费。
	// 这类必须在 UI 上和普通成功区分开——用户要去账单核对订单号。
	Uncertain    bool
	OrderID      string
	ExpiryBefore string
	ExpiryAfter  string
	DryRun       bool
	Env          string
	LedgerSaved  bool
	Msg          string
	Err          string
}

// renewOne 续一个域名。单个续费和批量续费共用这一份——
// 防重、防超付推算、台账落库、dry_run 处理任何一条都不该有两份实现。
//
//	调用方负责：解析 ciID/domain、校验 period、设置超时。
//	本函数负责：加互斥锁、调厂商、刷到期日、落台账。
//	operator 由调用方传入：页面上的单个续费传 currentUser(c)，
//	后台批量任务传发起人——后台跑的时候早就没有 gin.Context 了。
func (h *SyncHandler) renewOne(ctx context.Context, operator string, ciID int64, domain string, period int, quotedCur string, quotedAmt float64) renewOneResult {
	// 批量入口没有单个入口那层锁，这里再加一次（同一域名被两处同时续 = 扣两笔）
	if _, busy := renewInFlight.LoadOrStore(ciID*-1, true); busy {
		return renewOneResult{Err: "该域名正在续费中，请勿重复提交"}
	}
	defer renewInFlight.Delete(ciID * -1)

	wa, _, _, _, err := h.writeAdapterForDomain(fmt.Sprint(ciID))
	if err != nil {
		return renewOneResult{Err: err.Error()}
	}

	// 续费前到期日（防超付：与续费后对比，看年数是否只前进了所选 period）
	var expiryBefore sql.NullString
	_ = h.DB.QueryRow(`SELECT DATE_FORMAT(expiry_at,'%Y-%m-%d') FROM domains WHERE ci_id=?`, ciID).Scan(&expiryBefore)

	logx.JCtx(ctx, "domain_renew", "renew_start", map[string]any{"domain": domain, "period": period, "quoted_currency": quotedCur, "quoted_amount": quotedAmt, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": operator})
	res, err := wa.RenewDomain(ctx, domain, period)
	if err != nil {
		// ⚠️ 续费请求报错 **不等于** 没扣费。
		//
		//	GoDaddy 的续费是非幂等写：请求超时、连接中断、网关 5xx 时，
		//	订单很可能已经受理并扣款，只是响应没回到我们这边。
		//	直接报"失败"有两个恶果：账没记（对不上账单），以及人以为没扣
		//	去重试一次——那就真扣两笔了。
		//
		//	所以失败后必须回查到期日核对：到期日往前走了就是实际成功。
		logx.JCtx(ctx, "domain_renew", "renew_fail", map[string]any{"domain": domain, "period": period, "error": err.Error()})

		if newExp, ok := verifyRenewedByExpiry(ctx, wa, domain, expiryBefore.String, period); ok {
			logx.JCtx(ctx, "domain_renew", "renew_fail_but_actually_done", map[string]any{
				"domain": domain, "period": period, "expiry_before": expiryBefore.String,
				"expiry_after": newExp, "error": err.Error()})
			// 当成功处理：订单号拿不到（响应丢了），但钱确实扣了，台账必须记上，
			// 并把这个情况明确告诉用户——让他拿到期日去 GoDaddy 账单对订单号
			expiryAfter := sql.NullString{String: newExp, Valid: true}
			logExec(h.DB, "续费刷到期(超时后核对)", `UPDATE domains SET expiry_at=? WHERE ci_id=?`, newExp, ciID)
			ledgerOK := true
			if _, e := h.DB.Exec(`INSERT INTO domain_renewals
				(domain_ci_id, domain, period, quoted_currency, quoted_amount, actual_amount, actual_currency, order_id, expiry_before, expiry_after, operator, env, dry_run, raw_resp)
				VALUES (?, ?, ?, ?, ?, 0, '', '', ?, ?, ?, ?, 0, ?)`,
				ciID, domain, period, quotedCur, quotedAmt,
				nullableStr(expiryBefore), nullableStr(expiryAfter), operator, wa.EnvLabel(),
				"响应失败但经到期日核对确认已续费: "+truncate(err.Error(), 300)); e != nil {
				ledgerOK = false
			}
			return renewOneResult{
				ExpiryBefore: expiryBefore.String, ExpiryAfter: newExp,
				Env: wa.EnvLabel(), LedgerSaved: ledgerOK,
				Uncertain: true,
				Msg: fmt.Sprintf("续费请求未收到响应（%s），但核对到期日已从 %s 变为 %s，"+
					"判定**已实际扣费**。订单号请到 GoDaddy 账单按日期核对，不要重试。",
					truncate(err.Error(), 80), expiryBefore.String, newExp),
			}
		}
		return renewOneResult{Err: "续费失败：" + err.Error() +
			"（已回查到期日，未发现变化，判定未扣费；若账单上出现扣费请人工核对）"}
	}
	// 真续成功后拉最新到期日刷库（dry_run 不改厂商，跳过刷库）。
	// GoDaddy 续费后到期日有延迟未即时更新——若拉到的没前进，用「原到期 + 续费年数」推算，避免台账显示前后相同。
	var expiryAfter sql.NullString
	if !wa.DryRun() {
		expected := addYearsDate(expiryBefore.String, period)
		newExp := ""
		if d, e := wa.GetDomainDetail(ctx, domain); e == nil && d.Expires != nil {
			got := d.Expires.Format("2006-01-02")
			if expiryBefore.Valid && got <= expiryBefore.String && expected != "" {
				newExp = expected // 厂商延迟未更新，按推算
			} else {
				newExp = got
			}
		} else {
			if e != nil {
				logx.JCtx(ctx, "domain_renew", "renew_refresh_expiry_fail", map[string]any{"domain": domain, "error": e.Error()})
			}
			newExp = expected // 详情拉不到，用推算
		}
		if newExp != "" {
			expiryAfter = sql.NullString{String: newExp, Valid: true}
			logExec(h.DB, "续费刷到期", `UPDATE domains SET expiry_at=? WHERE ci_id=?`, newExp, ciID)
		}
	}
	dry := 0
	if wa.DryRun() {
		dry = 1
	}
	// 厂商实际扣费金额（GoDaddy 若返回则入库，供对账；多为 0）
	actualAmt := float64(res.AmountMicro) / 1_000_000.0
	// 落续费记录台账——落库失败必须告警（钱已扣，台账不能悄悄丢）
	ledgerSaved := true
	if _, e := h.DB.Exec(`INSERT INTO domain_renewals
		(domain_ci_id, domain, period, quoted_currency, quoted_amount, actual_amount, actual_currency, order_id, expiry_before, expiry_after, operator, env, dry_run, raw_resp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ciID, domain, period, quotedCur, quotedAmt, actualAmt, res.Currency, res.OrderID,
		nullableStr(expiryBefore), nullableStr(expiryAfter), operator, wa.EnvLabel(), dry, res.RawBody); e != nil {
		ledgerSaved = false
		logx.JCtx(ctx, "domain_renew", "ledger_save_fail", map[string]any{"domain": domain, "order_id": res.OrderID, "period": period, "error": e.Error()})
	}

	logx.JCtx(ctx, "domain_renew", "renew_done", map[string]any{"domain": domain, "period": period, "order_id": res.OrderID, "expiry_before": expiryBefore.String, "expiry_after": expiryAfter.String, "ledger_saved": ledgerSaved, "env": wa.EnvLabel(), "dry_run": wa.DryRun()})
	return renewOneResult{
		OrderID: res.OrderID, ExpiryBefore: expiryBefore.String, ExpiryAfter: expiryAfter.String,
		DryRun: wa.DryRun(), Env: wa.EnvLabel(), LedgerSaved: ledgerSaved,
		Msg: renewMsg(wa, domain, period),
	}
}

// verifyRenewedByExpiry 续费请求失败后，回查到期日确认是不是其实已经续上了。
//
//	为什么需要它：GoDaddy 续费是非幂等写。超时/中断/5xx 时订单可能已经受理，
//	钱扣了但响应没回来。此时报"失败"会让人重试，重试就是第二次扣款。
//	到期日是唯一能从外部确认"到底成没成"的凭据。
//
//	用**独立的 context**：调用方那个多半已经因超时被 cancel 了。
//	先等几秒——厂商侧订单落库有延迟，立刻查往往还是旧值。
func verifyRenewedByExpiry(parent context.Context, wa dnsource.WriteAdapter, domain, expiryBefore string, period int) (string, bool) {
	if expiryBefore == "" {
		return "", false // 没有基准就无法判断，宁可报失败让人工核对
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 25*time.Second)
	defer cancel()

	// 最多试 3 次，每次间隔 4 秒：厂商订单落库有延迟
	for i := 0; i < 3; i++ {
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(4 * time.Second):
		}
		d, err := wa.GetDomainDetail(ctx, domain)
		if err != nil || d.Expires == nil {
			continue
		}
		got := d.Expires.Format("2006-01-02")
		if got > expiryBefore {
			return got, true
		}
	}
	return "", false
}

// addYearsDate 给 "YYYY-MM-DD" 加 n 年；解析失败返回空串。
func addYearsDate(d string, n int) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return ""
	}
	return t.AddDate(n, 0, 0).Format("2006-01-02")
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
	rows, err := h.DB.Query(`SELECT id, domain, period, quoted_currency, quoted_amount, actual_amount, actual_currency, order_id,
		DATE_FORMAT(expiry_before,'%Y-%m-%d'), DATE_FORMAT(expiry_after,'%Y-%m-%d'),
		operator, env, dry_run, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM domain_renewals WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type row struct {
		ID             int64   `json:"id"`
		Domain         string  `json:"domain"`
		Period         int     `json:"period"`
		Currency       string  `json:"quoted_currency"`
		Amount         float64 `json:"quoted_amount"`
		ActualAmount   float64 `json:"actual_amount"`
		ActualCurrency string  `json:"actual_currency"`
		OrderID        string  `json:"order_id"`
		ExpiryBefore   string  `json:"expiry_before"`
		ExpiryAfter    string  `json:"expiry_after"`
		Operator       string  `json:"operator"`
		Env            string  `json:"env"`
		DryRun         int     `json:"dry_run"`
		CreatedAt      string  `json:"created_at"`
	}
	list := []row{}
	for rows.Next() {
		var r row
		var eb, ea sql.NullString
		if rows.Scan(&r.ID, &r.Domain, &r.Period, &r.Currency, &r.Amount, &r.ActualAmount, &r.ActualCurrency, &r.OrderID, &eb, &ea,
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
	logx.JCtx(ctx, "domain_renew", "autorenew_start", map[string]any{"enabled": in.Enabled, "domain": domain, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	if err := wa.SetAutoRenew(ctx, domain, in.Enabled); err != nil {
		logx.JCtx(ctx, "domain_renew", "autorenew_fail", map[string]any{"domain": domain, "enabled": in.Enabled, "error": err.Error()})
		c.JSON(http.StatusBadGateway, gin.H{"error": "设置自动续费失败：" + err.Error()})
		return
	}
	SetAuditTarget(c, domain)
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
