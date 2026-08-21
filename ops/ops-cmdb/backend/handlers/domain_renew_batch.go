package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/api/middleware"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
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
	// Tenant 发起这批续费的租户。
	//
	// ⚠️ 必须带着走。进度里有域名、订单号、到期日和金额——
	// 少了它，任何租户拿到 job_id 就能查到别的租户续了哪些域名、花了多少。
	// 原来内存和库两条路径都没有租户判定，库那条还固定写在 PlatformTenant 下。
	Tenant store.TenantID
}

var (
	batchJobs   = map[string]*batchRenewJob{}
	batchJobsMu sync.RWMutex
)

// persistJob 把进度写进库，让**任何副本**都能回答"这批跑到哪了"。
//
// ⚠️ 进程内那份 map 保留着（读得更快、且是权威的实时值），
// 库里这份是给**别的副本**读的。两者不一致时以库为准的时刻只有一个：
// 本副本内存里压根没有这个 job —— 那说明它是别人起的。
//
// 写失败只记日志不中断：续费本身比进度显示重要得多，
// 不能因为进度写不进去就把一个正在扣费的任务搞崩。
func (h *SyncHandler) persistJob(j *batchRenewJob) {
	// 用**发起这批续费的租户**，不是 PlatformTenant。
	// 写在平台租户下的话，所有租户的续费进度混在一张表的同一个分区里，
	// 而读取侧又不做校验 —— 等于谁拿到 id 谁就能看
	sc, err := h.Store.Tenant(store.ForJob(context.Background(), j.Tenant, "renew_batch"))
	if err != nil {
		logx.Line("domain_renew", "进度落库取作用域失败: "+err.Error())
		return
	}
	items, _ := json.Marshal(j.Items)
	fin := 0
	if j.Finished {
		fin = 1
	}
	if _, err := sc.Exec(`INSERT INTO domain_renew_jobs
		(id, tenant_id, total, done, succeeded, failed, uncertain, finished, msg, items_json, operator)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE done=VALUES(done), succeeded=VALUES(succeeded),
		  failed=VALUES(failed), uncertain=VALUES(uncertain), finished=VALUES(finished),
		  msg=VALUES(msg), items_json=VALUES(items_json)`,
		j.ID, j.Total, j.Done, j.Succeeded, j.Failed, j.Uncertain, fin, truncate(j.Msg, 480),
		string(items), j.Operator); err != nil {
		logx.Line("domain_renew", "进度落库失败（不影响续费本身）: "+err.Error())
	}
	// 顺手清 2 小时前的：进度是临时数据，追溯看 domain_renewals
	sc.Exec(`DELETE FROM domain_renew_jobs WHERE tenant_id = ? AND started_at < DATE_SUB(NOW(), INTERVAL 2 HOUR)`)
}

// loadJob 从库里读进度 —— 供**别的副本**回答轮询。
func (h *SyncHandler) loadJob(id string, tenant store.TenantID) *batchRenewJob {
	// 按**请求方的租户**查。查不到就是查不到 —— 别的租户的任务
	// 对这个人来说本来就不存在，不该因为 id 猜对了就返回
	sc, err := h.Store.Tenant(store.ForJob(context.Background(), tenant, "renew_batch"))
	if err != nil {
		return nil
	}
	j := batchRenewJob{Tenant: tenant}
	var fin int
	var items string
	if err := sc.QueryRow(`SELECT id, total, done, succeeded, failed, uncertain, finished, msg,
		COALESCE(items_json,'[]') FROM domain_renew_jobs WHERE tenant_id = ? AND id=?`, id).
		Scan(&j.ID, &j.Total, &j.Done, &j.Succeeded, &j.Failed, &j.Uncertain, &fin, &j.Msg, &items); err != nil {
		return nil
	}
	j.Finished = fin == 1
	_ = json.Unmarshal([]byte(items), &j.Items)
	return &j
}

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

// getJob 取本副本内存里的任务。
//
// ⚠️ 必须校验租户：这份 map 是全进程共享的，不带判定的话
// 任何租户拿到 job_id 就能读到别的租户的域名、订单号和金额。
func getJob(id string, tenant store.TenantID) *batchRenewJob {
	batchJobsMu.RLock()
	defer batchJobsMu.RUnlock()
	j := batchJobs[id]
	if j == nil || j.Tenant != tenant {
		return nil
	}
	return j
}

// batchRenewMax 一次最多处理多少个域名。
// 不是技术限制，是安全阀：粘贴板一次贴进几百个域名再点确认，
// 出错时的代价太大。需要更多就分批做。
const batchRenewMax = 50

// batchRenewGap 两个域名之间的间隔；batchRenewRetries 每个域名最多**额外**重试几次。
//
//	为什么要间隔：GoDaddy 对写接口的限流比读接口严得多，连着打几次
//	很容易被拒。而限流失败在批量结果里的样子就是"莫名其妙有一个没续上"。
//
//	⚠️ 重试有前置条件，不是无脑重试：必须 renewOne 已经**回查到期日确认没扣费**
//	（res.SafeRetry）。续费是非幂等写，"失败了就再来一次"在这里等于第二次扣款。
const (
	batchRenewGap     = 3 * time.Second
	batchRenewRetries = 2
	batchRetryBackoff = 8 * time.Second
)

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
	// OverpayNote 疑似多续费的提示（到期日前进幅度超出按年数推算的范围）。
	// 这条出现在**成功**的条目上——续费成功了，但可能多扣了钱
	OverpayNote string `json:"overpay_note,omitempty"`
	// Attempts：实际打了几次厂商接口。>1 说明重试过，展示出来是为了
	// 让人在对账时知道"这个域名不止发过一次请求"，别把它当普通一次性操作看。
	Attempts int `json:"attempts,omitempty"`
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
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if in.Period < 1 || in.Period > 10 {
		in.Period = 1 // 预览阶段容错：只影响"预计续到"的展示，不扣费
	}
	names := normalizeDomainsInput(in.Domains)
	if len(names) == 0 {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.noDomainsParsed", nil, nil)
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
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	// 和单个续费一样：绝不静默默认续 1 年
	if in.Period < 1 || in.Period > 10 {
		httpx.Invalid(c, "years", "1-10")
		return
	}
	names := normalizeDomainsInput(in.Domains)
	if len(names) == 0 {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.noDomainsParsed", nil, nil)
		return
	}
	if len(names) > batchRenewMax {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.renewBatchLimit", nil,
			map[string]any{"max": batchRenewMax, "got": len(names)})
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
		httpx.FailKeyWith(c, httpx.CodeBadRequest, "error.noRenewableDomains", nil,
			map[string]any{"items": items})
		return
	}
	if in.ConfirmCount > 0 && in.ConfirmCount != renewable {
		httpx.FailKeyWith(c, httpx.CodeConflict, "error.renewCountChanged",
			map[string]any{"confirmed": in.ConfirmCount, "now": renewable},
			map[string]any{
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
		// ⚠️ 在这里定下租户，后台任务全程带着它走。
		// 后台跑的时候早就没有 gin.Context 了，事后取不到
		Tenant: middleware.TenantOf(c),
	}
	putJob(job)
	// 立刻落一次库：前端拿到 job_id 之后的第一次轮询就可能打到别的副本
	h.persistJob(job)

	logx.JCtx(c.Request.Context(), "domain_renew", "batch_start", map[string]any{
		"job": job.ID, "count": renewable, "period": in.Period, "operator": job.Operator})

	go h.runBatchRenew(job, in.Period, in.QuotedCurrency, in.QuotedAmount)

	SetAuditTarget(c, fmt.Sprintf("批量续费 %d 个域名 %d 年（任务 %s）", renewable, in.Period, job.ID))
	c.JSON(http.StatusAccepted, gin.H{
		"job_id": job.ID, "total": renewable, "accepted": true,
		"msg_key":    "domains:renewStarted",
		"msg_params": map[string]any{"count": renewable},
		"msg":        fmt.Sprintf("已开始为 %d 个域名续费，可关闭弹窗，任务在后台继续", renewable),
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
			h.persistJob(job)
		}
	}()

	// 执行前取一次价：台账要记下单时的报价
	priceCtx, cancelPrice := context.WithTimeout(context.Background(), 25*time.Second)
	h.fillBatchPrices(priceCtx, job.Items)
	cancelPrice()

	first := true
	for i := range job.Items {
		if job.Items[i].Status != "ok" {
			continue
		}
		// 域名之间留间隔：连着打注册商的写接口很容易撞限流，
		// 而限流失败在批量里表现为"莫名其妙有一个没续上"。
		// 域名数量本来就不大，这点时间换的是结果确定性。
		if !first {
			time.Sleep(batchRenewGap)
		}
		first = false

		qCur, qAmt := qCurIn, qAmtIn
		if job.Items[i].PricePerYear > 0 {
			qCur = job.Items[i].Currency
			qAmt = job.Items[i].PricePerYear * float64(period)
		}

		res := h.renewOneWithRetry(job, i, period, qCur, qAmt)

		batchJobsMu.Lock()
		if res.Err != "" {
			job.Items[i].Status = "failed"
			job.Items[i].Reason = res.Err
			job.Items[i].Attempts = res.Attempts
			job.Failed++
		} else {
			job.Items[i].Attempts = res.Attempts
			job.Items[i].OrderID = res.OrderID
			job.Items[i].ExpiryAfter = res.ExpiryAfter
			job.Items[i].DryRun = res.DryRun
			job.Items[i].Env = res.Env
			job.Items[i].LedgerSaved = res.LedgerSaved
			job.Items[i].Msg = res.Msg
			job.Items[i].OverpayNote = res.OverpayNote
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
		// 每条都落一次：轮询间隔 2 秒，落库的开销远小于"进度不动"给人的困惑
		h.persistJob(job)
	}

	batchJobsMu.Lock()
	job.Finished = true
	defer h.persistJob(job)
	retried := 0
	for _, it := range job.Items {
		if it.Attempts > 1 {
			retried++
		}
	}
	// 结果必须把三态说全。原先只报一个成功数，"有一个没续上"就这么被吞掉了——
	// 用户是看 GoDaddy 账单才发现少了一个的。
	job.Msg = fmt.Sprintf("续费完成：共 %d 个，成功 %d 个", job.Total, job.Succeeded-job.Uncertain)
	if job.Uncertain > 0 {
		job.Msg += fmt.Sprintf("，待核对 %d 个（响应超时但已确认扣费，去账单按日期找订单号，切勿重试）", job.Uncertain)
	}
	if job.Failed > 0 {
		job.Msg += fmt.Sprintf("，失败 %d 个（下方逐条列了原因）", job.Failed)
	}
	if retried > 0 {
		job.Msg += fmt.Sprintf("；其中 %d 个重试过（重试前均已回查确认未扣费）", retried)
	}
	batchJobsMu.Unlock()

	logx.Line("domain_renew", fmt.Sprintf("批量续费任务 %s 完成：成功 %d 待核对 %d 失败 %d",
		job.ID, job.Succeeded, job.Uncertain, job.Failed))
}

// renewOneWithRetry 续一个域名，失败且**确认安全**时重试。
//
//	这是整个批量续费里最需要小心的地方。续费是非幂等写：
//	同一个域名发两次请求 = 扣两笔钱。所以重试的门槛设得很高，
//	必须同时满足两条，缺一不可：
//
//	  1. res.SafeRetry —— renewOne 已经回查厂商的到期日，**读到了**且还是旧值，
//	     即这次请求确定没生效。"查不出来"不算，那种情况一律不重试。
//	  2. 错误是瞬时类（限流/超时/5xx）—— 凭据错、域名不属于本账户这类
//	     重试多少次都一样，白打 API 还多一次限流配额。
//
//	重试前退避一段时间：正是限流导致的失败，立刻重试必然又被拒。
func (h *SyncHandler) renewOneWithRetry(job *batchRenewJob, idx, period int, qCur string, qAmt float64) renewOneResult {
	it := &job.Items[idx]
	var res renewOneResult

	for attempt := 1; attempt <= batchRenewRetries+1; attempt++ {
		// 每个域名每次尝试独立超时：一个卡住不该把后面的都拖死
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		res = h.renewOne(ctx, job.Operator, it.CIID, it.Domain, period, qCur, qAmt)
		cancel()
		res.Attempts = attempt

		if res.Err == "" || !res.SafeRetry || attempt > batchRenewRetries {
			if attempt > 1 {
				logx.Line("domain_renew", fmt.Sprintf("批量续费 %s 第 %d 次尝试结束：err=%q uncertain=%v",
					it.Domain, attempt, res.Err, res.Uncertain))
			}
			return res
		}

		logx.Line("domain_renew", fmt.Sprintf(
			"批量续费 %s 第 %d 次失败但已确认未扣费，%v 后重试：%s",
			it.Domain, attempt, batchRetryBackoff, res.Err))

		// 让轮询的前端看得到"在重试"，而不是干等着以为卡死了
		batchJobsMu.Lock()
		it.Msg = fmt.Sprintf("第 %d 次尝试失败（已确认未扣费），正在重试…", attempt)
		it.Attempts = attempt
		batchJobsMu.Unlock()

		time.Sleep(batchRetryBackoff)
	}
	return res
}

// BatchRenewStatus GET /domains/renew-batch/:id —— 轮询进度
func (h *SyncHandler) BatchRenewStatus(c *gin.Context) {
	// 先看本副本内存（实时、权威）。两条路径都按请求方租户过滤
	tenant := middleware.TenantOf(c)
	job := getJob(c.Param("id"), tenant)
	fromMemory := job != nil
	if job == nil {
		// ⚠️ 内存里没有**不代表任务不存在**：多副本下这次轮询可能被
		// 负载均衡打到了另一个副本。回落到库里那份进度。
		// 不这么做的话，任务刚起来一秒就会看到「任务不存在或已过期（超过 2 小时）」——
		// 那句话是假的，而用户看到它时钱正在被扣，他会以为没开始然后再点一次。
		job = h.loadJob(c.Param("id"), tenant)
	}
	if job == nil {
		httpx.FailKey(c, httpx.CodeNotFound, "error.renewTaskGone", nil, nil)
		return
	}
	if fromMemory {
		batchJobsMu.RLock()
		defer batchJobsMu.RUnlock()
	}
	c.JSON(200, gin.H{
		"job_id": job.ID, "total": job.Total, "done": job.Done,
		"succeeded": job.Succeeded, "failed": job.Failed, "uncertain": job.Uncertain,
		"finished": job.Finished, "msg": job.Msg, "items": job.Items,
	})
}
