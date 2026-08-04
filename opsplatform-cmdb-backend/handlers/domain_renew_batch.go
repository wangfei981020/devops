package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"sync"
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

// ── 异步执行 ────────────────────────────────────────────────────────
//
//	批量续费改成"提交后立即返回 + 轮询进度"，不再让 HTTP 请求一直挂着。
//
//	为什么必须异步：前端 axios 超时 30 秒，而单个域名续费要走一次
//	GoDaddy 写 API（慢时十几秒），失败还要回查核对（最长再 25 秒）。
//	4 个域名就可能超过 30 秒——**前端断开了，后端还在继续跑**。
//	用户看到的是"超时/部分失败"，实际钱一分不少地扣了。
//	这不只是体验问题，是对账问题。
//
//	进度存内存：进程重启会丢，但真正的账在 domain_renewals 表里，
//	丢的只是"这一批的实时进度"，不影响追溯。

type batchRenewJob struct {
	ID        string
	Total     int
	Done      int
	Succeeded int
	Failed    int
	Uncertain int
	Items     []batchRenewItem
	Finished  bool
	Msg       string
	StartedAt time.Time
	Operator  string
}

var (
	batchJobs   = map[string]*batchRenewJob{}
	batchJobsMu sync.RWMutex
)

// putJob / getJob 带锁访问；顺手清理 2 小时前的旧任务，避免内存无限涨
func putJob(j *batchRenewJob) {
	batchJobsMu.Lock()
	defer batchJobsMu.Unlock()
	batchJobs[j.ID] = j
	for id, old := range batchJobs {
		if old.Finished && time.Since(old.StartedAt) > 2*time.Hour {
			delete(batchJobs, id)
		}
	}
}

func getJob(id string) *batchRenewJob {
	batchJobsMu.RLock()
	defer batchJobsMu.RUnlock()
	return batchJobs[id]
}

// batchRenewMax 一次最多处理多少个域名。
// 不是技术限制，是安全阀：粘贴板一次贴进几百个域名再点确认，
// 出错时的代价太大。需要更多就分批做。
const batchRenewMax = 50

type batchRenewItem struct {
	Domain       string  `json:"domain"`
	CIID         int64   `json:"ci_id,omitempty"`
	Status       string  `json:"status"` // ok / not_found / unsupported / duplicated
	Reason       string  `json:"reason,omitempty"`
	ExpiryBefore string  `json:"expiry_before,omitempty"`
	ExpiryExpect string  `json:"expiry_expect,omitempty"`  // 预计续到（preview 用）
	ExpiryAfter  string  `json:"expiry_after,omitempty"`   // 实际续到（执行后）
	PricePerYear float64 `json:"price_per_year,omitempty"` // 单价/年（估算，查不到为 0）
	Currency     string  `json:"currency,omitempty"`
	OrderID      string  `json:"order_id,omitempty"`
	DryRun       bool    `json:"dry_run,omitempty"`
	Env          string  `json:"env,omitempty"`
	LedgerSaved  bool    `json:"ledger_saved,omitempty"`
	// Uncertain：厂商响应没拿到但回查确认已扣费。必须和普通成功区分——
	// 这类没有订单号，要人去账单核对，而且**绝不能重试**
	Uncertain bool   `json:"uncertain,omitempty"`
	Msg       string `json:"msg,omitempty"`
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

// fillBatchPrices 给可续费的域名取续费报价。
//
//	报价是**只读**的（GetRenewalPrice 不扣费），所以可以并发——
//	这点和续费本身不同，续费必须串行。但仍然限并发数：
//	一次 50 个域名同时打注册商 API 容易触发限流，反而一个价都拿不到。
//
//	取不到价不阻断：前端显示「—」即可。花钱前看不到金额是不行的，
//	但"某个域名查不到价"不该让整批预览失败。
func (h *SyncHandler) fillBatchPrices(ctx context.Context, items []batchRenewItem) {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := range items {
		if items[i].Status != "ok" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			wa, _, domain, _, err := h.writeAdapterForDomain(fmt.Sprint(items[idx].CIID))
			if err != nil {
				return
			}
			p, err := wa.GetRenewalPrice(ctx, domain)
			if err != nil || p.AmountMicro <= 0 {
				logx.Line("domain_renew", fmt.Sprintf("批量预览取价失败 domain=%s: %v", domain, err))
				return
			}
			// 并发写不同下标是安全的（各 goroutine 只碰自己那一个元素）
			items[idx].PricePerYear = float64(p.AmountMicro) / 1_000_000.0
			items[idx].Currency = p.Currency
		}(i)
	}
	wg.Wait()
}

// sumBatchTotals 按币种汇总预计域名费，并返回取到价的域名个数。
//
//	混币种不硬加成一个数——USD 20 + CNY 150 加起来是没有意义的，
//	前端按币种分别展示。取不到价的（PricePerYear<=0）不计入，
//	由调用方用 renewable-priced 告诉用户"还有几个没算进去"。
func sumBatchTotals(items []batchRenewItem, period int) (map[string]float64, int) {
	totals := map[string]float64{}
	priced := 0
	for _, it := range items {
		if it.Status != "ok" || it.PricePerYear <= 0 {
			continue
		}
		cur := it.Currency
		if cur == "" {
			cur = "USD" // 厂商没返回币种时的兜底，与单个续费一致
		}
		totals[cur] += it.PricePerYear * float64(period)
		priced++
	}
	return totals, priced
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

	// 取报价：整体限时，超时的域名没价格但不影响其余
	priceCtx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	h.fillBatchPrices(priceCtx, items)
	cancel()

	totals, priced := sumBatchTotals(items, in.Period)

	out := gin.H{"items": items, "total": len(items), "renewable": okCount, "period": in.Period,
		"totals": totals, "priced": priced, "unpriced": okCount - priced}
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

	// 起一个后台任务，立即把 job_id 返回给前端去轮询。
	// 这里刻意不复用请求的 context——请求一返回它就被 cancel 了，
	// 而续费必须跑完（钱已经在扣了，中途放弃只会让账更乱）。
	job := &batchRenewJob{
		ID: fmt.Sprintf("br-%d", time.Now().UnixNano()), Total: renewable,
		Items: items, StartedAt: time.Now(), Operator: currentUser(c),
	}
	putJob(job)

	logx.JCtx(c.Request.Context(), "domain_renew", "batch_start", map[string]any{
		"job": job.ID, "count": renewable, "period": in.Period, "operator": job.Operator})

	go h.runBatchRenew(job, in.Period, in.QuotedCurrency, in.QuotedAmount)

	SetAuditTarget(c, fmt.Sprintf("批量续费 %d 个域名 %d 年（任务 %s）", renewable, in.Period, job.ID))
	c.JSON(http.StatusAccepted, gin.H{
		"job_id": job.ID, "total": renewable, "accepted": true,
		"msg": fmt.Sprintf("已开始为 %d 个域名续费，可关闭弹窗，任务在后台继续", renewable),
	})
}

// runBatchRenew 后台跑批量续费。串行——并发打注册商 API 既可能触发限流，
// 也让"到底扣了几笔"在出错时更难查清。
func (h *SyncHandler) runBatchRenew(job *batchRenewJob, period int, qCurIn string, qAmtIn float64) {
	defer func() {
		if r := recover(); r != nil {
			// panic 也要把任务收尾，否则前端会一直转圈等一个永远不来的结果
			logx.Line("domain_renew", fmt.Sprintf("批量续费任务 %s panic: %v", job.ID, r))
			batchJobsMu.Lock()
			job.Finished = true
			job.Msg = "任务异常中断，请到「续费记录」核对实际扣费情况"
			batchJobsMu.Unlock()
		}
	}()

	// 执行前取一次价：台账要记下单时的报价
	priceCtx, cancelPrice := context.WithTimeout(context.Background(), 25*time.Second)
	h.fillBatchPrices(priceCtx, job.Items)
	cancelPrice()

	for i := range job.Items {
		if job.Items[i].Status != "ok" {
			continue
		}
		qCur, qAmt := qCurIn, qAmtIn
		if job.Items[i].PricePerYear > 0 {
			qCur = job.Items[i].Currency
			qAmt = job.Items[i].PricePerYear * float64(period)
		}

		// 每个域名独立超时：一个卡住不该把后面的都拖死
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		res := h.renewOne(ctx, job.Operator, job.Items[i].CIID, job.Items[i].Domain, period, qCur, qAmt)
		cancel()

		batchJobsMu.Lock()
		if res.Err != "" {
			job.Items[i].Status = "failed"
			job.Items[i].Reason = res.Err
			job.Failed++
		} else {
			job.Items[i].OrderID = res.OrderID
			job.Items[i].ExpiryAfter = res.ExpiryAfter
			job.Items[i].DryRun = res.DryRun
			job.Items[i].Env = res.Env
			job.Items[i].LedgerSaved = res.LedgerSaved
			job.Items[i].Msg = res.Msg
			if res.Uncertain {
				job.Items[i].Status = "uncertain"
				job.Items[i].Uncertain = true
				job.Uncertain++
			} else {
				job.Items[i].Status = "renewed"
			}
			job.Succeeded++
		}
		job.Done++
		batchJobsMu.Unlock()
	}

	batchJobsMu.Lock()
	job.Finished = true
	job.Msg = fmt.Sprintf("续费完成：成功 %d 个", job.Succeeded)
	if job.Uncertain > 0 {
		job.Msg += fmt.Sprintf("（其中 %d 个响应超时但已确认扣费，需去账单核对订单号）", job.Uncertain)
	}
	if job.Failed > 0 {
		job.Msg += fmt.Sprintf("，失败 %d 个", job.Failed)
	}
	batchJobsMu.Unlock()

	logx.Line("domain_renew", fmt.Sprintf("批量续费任务 %s 完成：成功 %d 待核对 %d 失败 %d",
		job.ID, job.Succeeded, job.Uncertain, job.Failed))
}

// BatchRenewStatus GET /domains/renew-batch/:id —— 轮询进度
func (h *SyncHandler) BatchRenewStatus(c *gin.Context) {
	job := getJob(c.Param("id"))
	if job == nil {
		c.JSON(404, gin.H{"error": "任务不存在或已过期（超过 2 小时）。实际结果请看「续费记录」"})
		return
	}
	batchJobsMu.RLock()
	defer batchJobsMu.RUnlock()
	c.JSON(200, gin.H{
		"job_id": job.ID, "total": job.Total, "done": job.Done,
		"succeeded": job.Succeeded, "failed": job.Failed, "uncertain": job.Uncertain,
		"finished": job.Finished, "msg": job.Msg, "items": job.Items,
	})
}
