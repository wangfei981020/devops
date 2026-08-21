package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/dnsource"
	"ops-cmdb-backend/internal/cluster"
	"ops-cmdb-backend/logx"
)

// 续费互斥见 renewOne：用 cluster.Mutex 做**跨副本**的去重。
//
//	⚠️ 这里原本是一个进程内的 sync.Map。单副本时够用，
//	多副本下双击的两个请求会落到不同 Pod，各自那份 map 都是空的，
//	于是双双放行 —— **扣两次钱**。而且症状极其滞后：
//	扩容前一切正常，扩容后要到对账才发现。

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
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if in.Period < 1 || in.Period > 10 {
		httpx.Invalid(c, "years", "1-10")
		return
	}
	ciID, _ := parseID(ciid)
	// 防重的锁统一在 renewOne 里加 —— 单个续费和批量续费都走那条路径。
	//
	//	原来这里和 renewOne 里各加了一次，为了不自锁，内层用 `ciID*-1`
	//	当另一个 key。两把锁保护同一件事、其中一把还用负数做命名空间，
	//	读的人得先想明白"为什么是负的"才敢改。合并成一把之后，
	//	"同一个域名同时只能续一次"这句话在代码里只有一处实现。
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
	// 疑似多续费单独给一个字段，不并进 warning：
	// 台账没写上是"记录问题"，多扣费是"钱的问题"，两者的下一步完全不同
	if r.OverpayNote != "" {
		out["overpay_note"] = r.OverpayNote
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
	// SafeRetry=true：**已经回查确认没扣费**，且错误是瞬时类（限流/超时/5xx）。
	// 只有同时满足这两条才允许自动重试——少一条都可能变成第二次扣款。
	SafeRetry bool
	// Attempts 实际打了几次厂商接口（批量重试时由调用方填）
	Attempts int
	// OverpayNote 疑似多续费时的提示。空 = 没有异常。
	//
	// 这是一条**成功路径上的警告**：续费确实成功了，但到期日前进的幅度
	// 超出了按年数推算的范围。不能报成失败（钱扣了、域名续了），
	// 也不能不报（那正是重复扣费唯一会留下的痕迹）。
	OverpayNote string
}

// isTransientRenewErr 判断这个错误重试一次有没有意义。
//
//	限流、超时、网关 5xx 是"等一会就好"，值得重试；
//	凭据错、域名不属于该账户、余额不足是确定性失败，重试只是白打一次 API。
//	注意：这个函数**只管"值不值得"**，"安不安全"由到期日回查负责，两者必须都成立。
func isTransientRenewErr(err error) bool {
	var rl *dnsource.RateLimitError
	if errors.As(err, &rl) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, k := range []string{
		"timeout", "deadline exceeded", "connection reset", "eof", "broken pipe",
		"too many requests", "429", "500", "502", "503", "504", "temporarily",
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// renewOne 续一个域名。单个续费和批量续费共用这一份——
// 防重、防超付推算、台账落库、dry_run 处理任何一条都不该有两份实现。
//
//	调用方负责：解析 ciID/domain、校验 period、设置超时。
//	本函数负责：加互斥锁、调厂商、刷到期日、落台账。
//	operator 由调用方传入：页面上的单个续费传 currentUser(c)，
//	后台批量任务传发起人——后台跑的时候早就没有 gin.Context 了。
func (h *SyncHandler) renewOne(ctx context.Context, operator string, ciID int64, domain string, period int, quotedCur string, quotedAmt float64) renewOneResult {
	// 单个续费与批量续费都经过这里，锁只在这一处加。
	//
	//	ttl 60s：续费要调厂商 API，外层超时是 40s，留 20s 余量。
	//	比临界区短的话，操作还没做完锁就放了，等于没锁。
	unlock, err := h.Mu.Lock(ctx, fmt.Sprintf("renew:%d", ciID), 60*time.Second)
	if err != nil {
		if errors.Is(err, cluster.ErrBusy) {
			return renewOneResult{Err: "该域名正在续费中，请勿重复提交"}
		}
		// 锁本身出故障时**不能放行**：这是扣钱路径，
		// "锁挂了就当没锁"等于把最坏情况留给了钱。
		logx.JCtx(ctx, "domain_renew", "lock_fail", map[string]any{"ci_id": ciID, "error": err.Error()})
		return renewOneResult{Err: "无法获取续费锁，已中止（避免重复扣费）：" + err.Error()}
	}
	defer unlock()

	wa, _, _, _, err := h.writeAdapterForDomain(fmt.Sprint(ciID))
	if err != nil {
		return renewOneResult{Err: err.Error()}
	}

	// 续费前到期日。续费后与它对比，看年数是否只前进了所选 period（防超付）。
	//
	// ⚠️ 这个比较在很长一段时间里**只存在于这条注释里**，代码中并没有实现：
	// 拿到什么到期日就原样收下写台账。厂商若多续了年数，没有任何地方会发现。
	// 实现见下方 overpay_suspected 分支。
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

		newExp, renewed, verified := verifyRenewedByExpiry(ctx, wa, domain, expiryBefore.String, period)
		if renewed {
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
					"判定已实际扣费。订单号请到 GoDaddy 账单按日期核对，不要重试。",
					truncate(err.Error(), 80), expiryBefore.String, newExp),
			}
		}
		// 走到这里 = 没扣费，或者没查出来。两者对"能不能自动重试"是相反的结论。
		if verified {
			// 只有这一条路径允许重试：我们**读到了**厂商侧的到期日，而且它还是旧值。
			// 任何"查不出来"的情况都不给这个标记。
			transient := isTransientRenewErr(err)
			tail := "（已回查到期日确认未扣费；该错误重试也不会变，请先排查原因——凭据、域名归属、账户余额都可能）"
			if transient {
				tail = "（已回查到期日确认未扣费，属可重试的瞬时故障）"
			}
			return renewOneResult{Err: "续费失败：" + err.Error() + tail, SafeRetry: transient}
		}
		return renewOneResult{Err: "续费失败：" + err.Error() +
			"（到期日回查也未成功，无法确认是否扣费，请到 GoDaddy 账单核对后再决定是否重试）"}
	}
	// 真续成功后拉最新到期日刷库（dry_run 不改厂商，跳过刷库）。
	// GoDaddy 续费后到期日有延迟未即时更新——若拉到的没前进，用「原到期 + 续费年数」推算，避免台账显示前后相同。
	var expiryAfter sql.NullString
	// 疑似多续费的提示。空 = 没有异常
	overpayNote := ""
	if !wa.DryRun() {
		expected := addYearsDate(expiryBefore.String, period)
		newExp := ""
		if d, e := wa.GetDomainDetail(ctx, domain); e == nil && d.Expires != nil {
			got := d.Expires.Format("2006-01-02")
			if expiryBefore.Valid && got <= expiryBefore.String && expected != "" {
				newExp = expected // 厂商延迟未更新，按推算
			} else {
				newExp = got
				// ⚠️ 真正的防超付检查。
				//
				// 上面那句注释（"与续费后对比，看年数是否只前进了所选 period"）
				// 一直写着，但这个比较**从来没被实现过**——代码拿到什么到期日
				// 就原样收下写进台账。GoDaddy 若因为重复提交/账户设置
				// 多续了年数，没有任何地方会发现。
				//
				// 判据要留余量：厂商的到期日会因时区、宽限期、闰年差几天，
				// 掐死成"必须等于 expected"会天天误报，而天天误报的告警
				// 等于没有告警。超出 45 天才算异常。
				if over := daysBetween(expected, got); expected != "" && over > overpayToleranceDays {
					logx.JCtx(ctx, "domain_renew", "overpay_suspected", map[string]any{
						"level": "WARN", "domain": domain, "period": period,
						"expiry_before": expiryBefore.String, "expected": expected,
						"actual": got, "over_days": over,
						"msg": "厂商侧到期日比按年数推算的多出很多，疑似多续/重复扣费，请核对 GoDaddy 账单",
					})
					overpayNote = fmt.Sprintf(
						"⚠️ 到期日为 %s，比按 %d 年推算的 %s 多了 %d 天，疑似多续费，请核对 GoDaddy 账单",
						got, period, expected, over)
				}
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
		Msg: renewMsg(wa, domain, period), OverpayNote: overpayNote,
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
//	返回值三态，**不能压缩成两态**：
//
//	  renewed=true                → 已扣费（到期日前进了）
//	  renewed=false, verified=true → 确认没扣费（成功读到到期日，还是旧值）
//	  verified=false              → 查不出来（没基准，或详情接口也挂了）
//
//	区别在于「能不能重试」：只有中间那种是安全的。把"查了没变"和
//	"根本没查成"混为一谈，就会在厂商整体故障时对着一个可能已扣费的
//	域名再发一次续费——那是第二笔钱。
func verifyRenewedByExpiry(parent context.Context, wa dnsource.WriteAdapter, domain, expiryBefore string, period int) (newExp string, renewed, verified bool) {
	if expiryBefore == "" {
		return "", false, false // 没有基准就无法判断，宁可报失败让人工核对
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 25*time.Second)
	defer cancel()

	// 最多试 3 次，每次间隔 4 秒：厂商订单落库有延迟
	for i := 0; i < 3; i++ {
		select {
		case <-ctx.Done():
			return "", false, verified
		case <-time.After(4 * time.Second):
		}
		d, err := wa.GetDomainDetail(ctx, domain)
		if err != nil || d.Expires == nil {
			continue
		}
		got := d.Expires.Format("2006-01-02")
		if got > expiryBefore {
			return got, true, true
		}
		verified = true // 读到了、且还是旧值——这次续费确实没生效
	}
	return "", false, verified
}

// addYearsDate 给 "YYYY-MM-DD" 加 n 年；解析失败返回空串。
// overpayToleranceDays 到期日超出推算值多少天算异常。
//
//	厂商的到期日会因时区、宽限期、闰年与我们推算的差几天，属正常。
//	掐死成"必须等于推算值"会天天误报——而天天误报的告警等于没有告警。
//	45 天远小于最小续费单位（1 年），足以区分"差几天"和"多续了一年"。
const overpayToleranceDays = 45

// daysBetween 返回 to - from 的天数（都用 2006-01-02）。任一边解析不了返回 0。
//
//	⚠️ 解析失败返回 0 = "看不出异常"，而不是返回一个大数触发告警。
//	日期格式问题不该表现成一条"疑似多扣费"，那会把人引向完全错误的方向。
func daysBetween(from, to string) int {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(t.Sub(f).Hours() / 24)
}

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
		httpx.Fail(c, httpx.CodeUpstreamError, fmt.Errorf("设置自动续费失败: %w", err), nil)
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
