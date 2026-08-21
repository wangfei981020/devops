package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/logx"
	"ops-cmdb-backend/n9e"
)

// 夜莺告警接入。
//
//	为什么 CMDB 要接告警：排障时最费时间的不是"看告警"，而是在告警系统和
//	资产系统之间来回切、自己在脑子里做关联——「这条告警的机器属于哪个项目」
//	「这个域名归谁」。事件中心已经聚合了到期/变更/同步失败/K8s Warning，
//	告警是缺的最后一块。补上之后，AI 经 MCP 一次问答就能同时拿到告警和资产关联。
//
//	接入方式复用 obs_endpoints（type='n9e'），和 Prometheus/Loki/KubeSphere
//	同一套观测端点管理、同一套凭据加密，不另造一张表。

type AlertHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewAlertHandler(db *sql.DB, cipher *crypto.Cipher) *AlertHandler {
	return &AlertHandler{DB: db, Cipher: cipher}
}

func (h *AlertHandler) Register(r *gin.RouterGroup) {
	r.GET("/alerts", h.List)
}

// n9eClient 按环境解析夜莺接入。
//
//	**没配置返回 (nil, nil)，不是错误**。这两种状态给用户看到的意思完全不同：
//	  未接入   → "去「管理 → 观测端点」配一个"
//	  接入了但坏 → "连不上/token 失效，去查"
//	把前者当错误报，用户会以为系统出问题了；反过来把后者当"无告警"，
//	更糟——那是把故障伪装成正常。
//	resolveEndpointFull 找不到数据源时返回 error，这里把它翻译成"未配置"。
func n9eClient(db *sql.DB, cipher *crypto.Cipher, env string) (*n9e.Client, error) {
	// ⚠️ 告警和指标的定位方式不一样，这里必须用「不限定」。
	//
	//	夜莺是**全局告警系统**：一套实例覆盖所有集群，接入时自然会绑到
	//	具体集群和环境上（生产那条 infra-n9e 绑的是 集群3 + PROD）。
	//	而这里原本传的是 clusterID=0 —— 在 resolveEndpointFull 里，
	//	0 的含义是"我要一个没绑集群的源"，于是绑了集群的 infra-n9e 被 continue 跳过；
	//	env 传空时同理，绑了 PROD 的也被跳过。两个条件同时命中，
	//	**只有"通用且不限环境"的数据源才认得出来**，而界面是鼓励选具体集群的。
	//	结果就是生产明明接了夜莺，告警页永远显示"未接入"。
	//
	//	env 为空 = 用户选了"全部环境"，同样要按不限定处理。
	lookupEnv := env
	if lookupEnv == "" {
		lookupEnv = anyEnv
	}
	url, token, _, err := resolveEndpointFull(db, cipher, "n9e", lookupEnv, anyCluster)
	if err != nil {
		logx.Line("alerts", fmt.Sprintf("夜莺接入未就绪（env=%s）：%v", env, err))
		return nil, nil
	}
	if url == "" {
		return nil, nil
	}
	return n9e.New(url, token), nil
}

// sevCode critical/warning/info → 夜莺的 1/2/3。认不出返回 0（= 不下推）
func sevCode(label string) int {
	switch label {
	case "critical":
		return 1
	case "warning":
		return 2
	case "info":
		return 3
	}
	return 0
}

// List GET /api/alerts?state=current|history&env=&hours=&limit=&severity=&q=
func (h *AlertHandler) List(c *gin.Context) {
	env := c.Query("env")
	state := c.DefaultQuery("state", "current")
	sevFilter := c.Query("severity") // critical/warning/info
	// ⚠️ 关键词过滤原来**根本没实现**，而前端一直在传 q ——
	// 于是搜索框看着能用，输什么都返回全量。搜索框比没有搜索框更坏：
	// 用户会以为"搜出来这些就是全部匹配项"。
	kw := strings.ToLower(strings.TrimSpace(c.Query("q")))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))

	cli, err := n9eClient(h.DB, h.Cipher, env)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, errors.New(SafeErr("解析夜莺接入", err)), nil)
		return
	}
	if cli == nil {
		// 没配不是错误，但必须说清楚——否则空列表会被当成"当前无告警"，
		// 那是把"没接入"伪装成"一切正常"
		c.JSON(200, gin.H{
			"list": []gin.H{}, "configured": false,
			"hint_key": "alerts:hint.n9eNotConfigured",
			// hint 保留给 MCP / 直接调 API 的人（读者是 AI 和运维）
			"hint": "尚未接入夜莺。请到「管理 → 观测端点」添加一个类型为 n9e 的接入点",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()

	var events []n9e.AlertEvent
	var total int64
	if state == "history" {
		hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
		if hours < 1 || hours > 24*30 {
			hours = 24
		}
		etime := time.Now().Unix()
		stime := etime - int64(hours)*3600
		events, total, err = cli.HistoryAlerts(ctx, stime, etime, limit)
	} else {
		// ⚠️ 有筛选条件时必须**多取**：夜莺是先按 limit 截断再返回的，
		// 拿回 limit 条再本地过滤，会把"筛掉了"显示成"没有"。
		// 级别能下推给夜莺，关键词不能（夜莺没这个参数），所以关键词只能靠多取。
		fetch := limit
		if kw != "" {
			fetch = 500
		}
		events, total, err = cli.CurrentAlertsSev(ctx, fetch, sevCode(sevFilter))
	}
	if err != nil {
		logx.Line("alerts", fmt.Sprintf("WARN 拉夜莺告警失败 env=%s state=%s: %v", env, state, err))
		c.JSON(502, gin.H{"error": SafeErr("拉取告警", err), "configured": true})
		return
	}

	list := make([]gin.H, 0, len(events))
	filtered, kwFiltered := 0, 0
	for _, e := range events {
		if sevFilter != "" && e.SeverityLabel() != sevFilter {
			filtered++
			continue
		}
		if kw != "" && !alertMatches(e, kw) {
			kwFiltered++
			continue
		}
		if len(list) >= limit {
			break // 关键词场景下多取了，这里裁回用户要的条数
		}
		item := gin.H{
			"id": e.ID, "rule_name": e.ShortRuleName(), "rule_note": e.RuleNote,
			"severity": e.SeverityLabel(), "object": e.Object(),
			// ⚠️ cluster / datasource 分开：夜莺顶层的 cluster 装的是**数据源名**。
			// 合成一个字段的话，「集群」列会整页显示 "VictoriaMetrics"。
			"cluster": e.ClusterName(), "datasource": e.Cluster,
			"group":         e.GroupName,
			"trigger_value": e.TriggerValue, "tags": e.BizTags(),
			"notified":     e.NotifyCurNumber,
			"recovered":    e.IsRecovered,
			"trigger_time": tsFmt(e.TriggerTime),
		}
		// 规则名被剥掉级别后缀时把原名带上，方便和夜莺控制台对照
		if e.ShortRuleName() != e.RuleName {
			item["rule_name_raw"] = e.RuleName
		}
		if e.RecoverTime > 0 {
			item["recover_time"] = tsFmt(e.RecoverTime)
		}
		list = append(list, item)
	}
	out := gin.H{"list": list, "returned": len(list), "total": total, "configured": true, "state": state}
	// 可选环境由**后端**给：env 决定用哪个夜莺接入点，前端硬编码一份的话，
	// 选到一个没配的环境会静默返回空 —— 看着像"这个环境很安静"。
	// 只列真的配了 n9e 接入点的环境，选项与现实一一对应。
	// ⚠️ 环境清单取不到 ≠ 没有绑环境的接入点。
	//	两者都返回空数组的话，前端会一样地把选择器藏起来 ——
	//	而"查不出来"这件事就此消失。本仓的三态约定：失败态不许退化成空态。
	if envs, err := n9eEnvs(h.DB); err != nil {
		// ⚠️ 走 SafeErr：原始 SQL 错误里带库名表名列名，
		//	而前端会把这段渲染进 DOM 的 title（生产实测泄露过，验收会话 NEW-1）。
		//	原始错误已在 n9eEnvs 里进了日志，排障拿得到。
		out["envs_error"] = SafeErr("查夜莺接入环境", err)
	} else {
		out["envs"] = envs
	}
	out["env"] = env
	if sevFilter != "" {
		out["severity_filter"] = sevFilter
		out["filtered_out"] = filtered
	}
	if kw != "" {
		out["q"] = kw
		out["kw_filtered_out"] = kwFiltered
	}
	// 「筛完是空」和「本来就没有」必须能分开：前者要提示放宽条件，后者是真太平。
	if len(list) == 0 && (filtered > 0 || kwFiltered > 0) {
		out["empty_reason"] = "filtered"
	}
	if total > int64(len(list)) {
		// 静默截断会让人以为"线上就这几条"。CMDB-019 同类问题。
		out["truncated"] = true
		out["hint"] = fmt.Sprintf("夜莺侧共 %d 条，本次只取回 %d 条（limit=%d）", total, len(list), limit)
	}
	c.JSON(200, out)
}

// alertMatches 关键词命中判定。kw 必须已经是小写。
//
//	匹配范围要覆盖**列表上看得见的所有内容**：用户搜的是他在屏幕上读到的字。
//	只匹配规则名的话，搜一个域名（它显示在「对象」列里）会搜不到，
//	而用户会理解成"没有这个域名的告警"。
func alertMatches(e n9e.AlertEvent, kw string) bool {
	for _, s := range []string{e.RuleName, e.RuleNote, e.Object(), e.GroupName, e.ClusterName(), e.Cluster} {
		if strings.Contains(strings.ToLower(s), kw) {
			return true
		}
	}
	// 标签也算：按 env=prod、team=app 这类维度找是排障常用姿势
	for k, v := range e.BizTags() {
		if strings.Contains(strings.ToLower(k), kw) || strings.Contains(strings.ToLower(v), kw) {
			return true
		}
	}
	return false
}

func tsFmt(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// collectAlerts 把夜莺告警并进事件中心的时间线。
//
//	只取**当前活跃**的：事件中心是"最近发生了什么"的视图，
//	把几千条已恢复的历史告警灌进去会把其他来源全淹掉。
//	要翻历史去告警页按时间筛。
func (h *EventCenterHandler) collectAlerts(ctx context.Context, cipher *crypto.Cipher, add func(evt)) {
	cli, err := n9eClient(h.DB, cipher, "")
	if err != nil || cli == nil {
		return // 没接入就跳过，不影响其他来源
	}
	events, total, err := cli.CurrentAlerts(ctx, 300)
	if err != nil {
		logx.Line("event_center", fmt.Sprintf("WARN 拉夜莺告警失败，事件中心少了告警这一路: %v", err))
		return
	}

	// **只收 critical / warning，info 级挡在外面**。
	//
	//	实测生产上有 298 条活跃告警（info 89 / warning 187 / critical 22）。
	//	全灌进来的话告警会占掉事件中心近一半版面，把变更、到期、K8s Warning
	//	全挤到看不见——事件中心就变成了第二个告警列表，失去"一眼看到
	//	平台最近出了什么事"的作用。info 级是"提醒"，要看去告警页。
	skipped := 0
	for _, e := range events {
		if e.IsRecovered {
			continue
		}
		if e.Severity >= 3 {
			skipped++
			continue
		}
		msg := e.RuleNote
		if e.TriggerValue != "" {
			msg = fmt.Sprintf("%s（当前值 %s）", msg, e.TriggerValue)
		}
		t := time.Unix(e.TriggerTime, 0)
		// 🔴 告警的时间戳是**开始烧的时刻**，不是"刚发生"。
		//
		//	这里只取未恢复的告警（活跃的），所以一条 4 个月前触发、
		//	到现在还没恢复的告警**确实是当前问题**，该出现在时间线上。
		//	但它的时间戳会让人读成"这是一条很久以前的旧事件"，
		//	而且 days 窗口对它不生效 —— 查「最近 7 天」却看到 4 月的日期，
		//	读的人只会以为筛选坏了（生产实测：2026-04-24 的告警出现在 days=7 里）。
		//
		//	⚠️ 不能靠 days 把它过滤掉：那会把一个烧了 4 个月还没人管的告警
		//	从"最近出了什么事"里**藏起来**，而它恰恰是最该看见的那种。
		//	正确做法是把"已持续多久"说出来 —— 持续时间本身就是严重度。
		if d := time.Since(t); d >= 48*time.Hour {
			msg = fmt.Sprintf("%s ⚠️ 已持续 %d 天未恢复（时间戳是开始触发的时刻，不是刚发生）",
				msg, int(d.Hours()/24))
		}
		add(evt{
			Time:    t.Format("2006-01-02 15:04:05"),
			Source:  "alert",
			Level:   e.SeverityLabel(),
			Object:  e.Object(),
			Title:   e.RuleName,
			Message: msg,
			Cluster: e.Cluster,
			sortTs:  t,
		})
	}
	if skipped > 0 {
		logx.Line("event_center", fmt.Sprintf(
			"告警接入：夜莺共 %d 条活跃，已收 critical/warning，另有 %d 条 info 级未纳入（去告警页看）",
			total, skipped))
	}
}

// n9eEnvs 列出配了夜莺接入点的环境，供告警页渲染环境选择器。
//
// ⚠️ 为什么必须由后端给：`env` 决定用哪个接入点。前端自己写一份环境清单的话，
// 选到一个没配夜莺的环境会得到一个空列表，而空列表在这一页会被读成
// 「这个环境没有告警」—— 把"看不见"伪装成"很太平"。
//
// 空串代表「不限环境」的那条接入点（resolveEndpointFull 里的 anyEnv 语义），
// 不列进选项：它是"全部"那一档，前端已经有了。
func n9eEnvs(db *sql.DB) ([]string, error) {
	// ⚠️ ORDER BY 的列必须出现在 SELECT 列表里 —— 这里选的是 COALESCE(env,'')
	//	而不是裸 env，所以 `ORDER BY env` 在 DISTINCT 下非法：
	//	    Error 3065: Expression #1 of ORDER BY clause is not in SELECT list
	//	（生产实测到的，由 envs_error 这条三态如实报了出来 ——
	//	 如果当初把失败也返回成空数组，这个 bug 会表现成"环境选择器不显示"，
	//	 而那看起来完全正常。）
	rows, err := db.Query(`SELECT DISTINCT COALESCE(env,'') AS e FROM obs_endpoints
	                        WHERE type='n9e' AND enabled=1 AND COALESCE(env,'')<>'' ORDER BY e`)
	if err != nil {
		logx.Line("alerts", fmt.Sprintf("WARN 查夜莺接入环境失败: %v", err))
		// ⚠️ 返回错误而不是空数组：空数组的含义是"确实没有绑环境的接入点"，
		//	把失败也说成那个，等于告诉前端"这里没什么可选的" —— 而事实是我们不知道。
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil && e != "" {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
