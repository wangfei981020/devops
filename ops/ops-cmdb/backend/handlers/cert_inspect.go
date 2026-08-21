package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"

	"ops-cmdb-backend/logx"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"
)

// CertInspectHandler 证书巡检：跨所有域名把线上检测证书 + ACME 签发证书拉平成一张表。
type CertInspectHandler struct {
	Store *store.Store
	DB    *sql.DB
}

func NewCertInspectHandler(st *store.Store, db *sql.DB) *CertInspectHandler {
	return &CertInspectHandler{Store: st, DB: db}
}

func (h *CertInspectHandler) Register(r *gin.RouterGroup) {
	r.GET("/cert-inspect", h.List)
	r.PUT("/records/:id/cert-ignore", h.Ignore)
}

type certInspectItem struct {
	Kind         string `json:"kind"` // online=线上检测 / acme=签发
	RecordID     int64  `json:"record_id"`
	DomainCIID   int64  `json:"domain_ci_id"`
	FQDN         string `json:"fqdn"`
	Domain       string `json:"domain"`
	ExpiryAt     string `json:"expiry_at"`
	CheckMsg     string `json:"check_msg"`
	Ignored      bool   `json:"ignored"`
	IgnoreReason string `json:"ignore_reason"`
	DomainStatus string `json:"domain_status"` // 所属主域名生命周期状态（已下线/未使用的证书可停续期）
	OriginIP     string `json:"origin_ip"`     // 解析目标地址，内网判定的依据，界面上也要能看见
	// 失败归类（见 tls_error.go）。check_msg 非空时才有值。
	//
	//	scope=internal 的**不是失败**：内网地址被公网巡检器探测，连不上是必然的。
	//	前端据此把这类单列，不再混进"检测失败 N"里。
	ReasonKey   string `json:"reason_key"`
	ReasonLabel string `json:"reason_label"`
	Scope       string `json:"scope"`
	// ProbeState 这条**有没有被探过**，只对 kind=online 有意义。
	//
	//	never  = 从没探过 → 到期日为空是"不知道"，不是「没有到期日」
	//	failed = 探了但失败 → 看 reason_label
	//	ok     = 探到了
	//
	//	⚠️ 三态缺了 never 这一档，就没法区分"没探过"和"探了没结果"。
	//	实测 700 条全部是 never（探测任务 inspect 停用且从没跑过），
	//	而界面把它们渲染成 700 行「—」——
	//	看的人只能理解成"这些证书没有到期日"，那是不可能的事，
	//	于是要么怀疑数据坏了，要么根本不再看这一页（OPSCMDB-031 P0-4）。
	ProbeState string `json:"probe_state,omitempty"`
}

// certProbeOut 巡检响应。
//
//	⚠️ 这里从**裸数组改成了包装对象**。
//
//	理由是必须有地方放"整批数据能不能信"这个结论：
//	探测任务从没跑过时，700 行空白本身传达不了任何信息，
//	而顶层一句「探测任务从没执行过，下面的到期日全部不可用」能直接指向下一步。
//
//	⚠️ 改形状同时改了前端类型和 MCP 说明。
//	本项目在"包装对象 vs 裸数组"上崩过两次页面（{items,...} 被当成数组），
//	所以这个字段名刻意用 items，与其它列表接口一致。
type certProbeOut struct {
	Items []certInspectItem `json:"items"`
	Total int               `json:"total"`
	// ProbeState 整批的探测状态：never / partial / ok
	ProbeState string `json:"probe_state"`
	// ProbeNoteKey / ProbeNoteParams 前端语言包的 key 和插值参数。
	//
	// ⚠️ 这里**不发拼好的句子**：那句话（"你的证书临期提醒根本不工作"）
	//	既是最不能丢的一句，也是英文界面上永远看不懂的一句。
	//	判定仍在后端，前端只负责把它说成人话。
	ProbeNoteKey    string         `json:"probe_note_key,omitempty"`
	ProbeNoteParams map[string]any `json:"probe_note_params,omitempty"`
	// NeverProbed / ProbeFailed 各有多少条，供前端做筛选和标红
	NeverProbed int `json:"never_probed"`
	ProbeFailed int `json:"probe_failed"`
	// InternalTargets 解析到内网地址的条数。⚠️ 这些**不是失败**：
	// 内网地址被公网巡检器探测，连不上是必然的
	InternalTargets int `json:"internal_targets"`
}

// List 返回全量巡检项（前端做排序/筛选/分页）。
//
// ⚠️ 三段来源（线上检测 / 域名注册 / ACME 签发）缺一不可：少了任何一段，
// 前端算出来的"快到期 N / 已过期 N / 检测失败 N"就是偏小的，
// 而偏小的告警数比报错更危险（CMDB-013）。所以任何一段查失败都返回 500。
func (h *CertInspectHandler) List(c *gin.Context) {
	out := []certInspectItem{}
	// 主域名生命周期状态查表：domain ci_id → status
	statusMap := map[int64]string{}
	if srows, e := h.DB.Query(`SELECT ci_id, status FROM domains WHERE status<>''`); e != nil {
		logx.J("cert_inspect", "query_fail", map[string]any{"item": "domain_status", "err": e.Error()})
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("巡检查询失败(域名状态): %w", e), nil)
		return
	} else {
		for srows.Next() {
			var id int64
			var st string
			if srows.Scan(&id, &st) == nil {
				statusMap[id] = st
			}
		}
		srows.Close()
	}

	// 线上检测证书（来自解析记录）
	rows, err := h.DB.Query(`
		SELECT r.id, r.domain_ci_id, r.host, c.name, r.cert_expiry_at, r.cert_check_msg, r.cert_ignored, r.cert_ignore_reason,
		       COALESCE(r.origin_ip,'')
		FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id
		WHERE r.ignored=0`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	internalN, neverProbed, probeFailed := 0, 0, 0
	for rows.Next() {
		var it certInspectItem
		var host, domain string
		var exp sql.NullTime
		var ignored int
		if rows.Scan(&it.RecordID, &it.DomainCIID, &host, &domain, &exp, &it.CheckMsg, &ignored, &it.IgnoreReason,
			&it.OriginIP) != nil {
			continue
		}
		it.Kind = "online"
		it.Domain = domain
		it.FQDN = recordFQDN(host, domain)
		if exp.Valid {
			it.ExpiryAt = exp.Time.Format("2006-01-02")
		}
		it.Ignored = ignored == 1
		it.DomainStatus = statusMap[it.DomainCIID]
		if r := classifyTLSError(it.CheckMsg, it.OriginIP); r.Key != "" {
			it.ReasonKey, it.ReasonLabel, it.Scope = r.Key, r.Label, r.Scope
			if r.Scope == scopeInternal {
				internalN++
			}
		}
		// 三态：有到期日 = 探到了；没到期日但有错误信息 = 探了失败；两者都没有 = 从没探过
		switch {
		case it.ExpiryAt != "":
			it.ProbeState = "ok"
		case it.CheckMsg != "":
			it.ProbeState = "failed"
			probeFailed++
		default:
			it.ProbeState = "never"
			neverProbed++
		}
		out = append(out, it)
	}
	rows.Close()
	if internalN > 0 {
		// 这个数字直接决定"检测失败 N"要不要信，必须留痕
		logx.J("cert_inspect", "internal_targets", map[string]any{
			"count": internalN, "note": "解析到内网地址，公网探测不适用，已从失败计数中分出",
		})
	}

	// 域名注册到期（WHOIS）——已忽略的主域名不纳入巡检
	drows, err := h.DB.Query(`SELECT c.id, c.name, d.expiry_at FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND d.stale=0 AND d.ignored=0`)
	if err != nil {
		logx.J("cert_inspect", "query_fail", map[string]any{"item": "domain_expiry", "err": err.Error()})
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("巡检查询失败(域名到期): %w", err), nil)
		return
	} else {
		for drows.Next() {
			var it certInspectItem
			var name string
			var exp sql.NullTime
			if drows.Scan(&it.DomainCIID, &name, &exp) != nil {
				continue
			}
			it.Kind = "domain"
			it.FQDN = name
			it.Domain = name
			if exp.Valid {
				it.ExpiryAt = exp.Time.Format("2006-01-02")
			}
			it.DomainStatus = statusMap[it.DomainCIID]
			out = append(out, it)
		}
		drows.Close()
	}

	// ACME 签发证书
	arows, err := h.DB.Query(`SELECT cert.ci_id, c.name, cert.cn, cert.expiry_at
		FROM certificates cert JOIN cis c ON c.id=cert.ci_id WHERE cert.status='active'`)
	if err != nil {
		logx.J("cert_inspect", "query_fail", map[string]any{"item": "acme_certs", "err": err.Error()})
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("巡检查询失败(ACME 证书): %w", err), nil)
		return
	} else {
		for arows.Next() {
			var it certInspectItem
			var name, cn string
			var exp sql.NullTime
			if arows.Scan(&it.DomainCIID, &name, &cn, &exp) != nil {
				continue
			}
			it.Kind = "acme"
			it.FQDN = cn
			it.Domain = name
			if exp.Valid {
				it.ExpiryAt = exp.Time.Format("2006-01-02")
			}
			out = append(out, it)
		}
		arows.Close()
	}

	res := certProbeOut{
		Items: out, Total: len(out),
		NeverProbed: neverProbed, ProbeFailed: probeFailed, InternalTargets: internalN,
	}
	res.ProbeState, res.ProbeNoteKey, res.ProbeNoteParams = h.probeVerdict(neverProbed, probeFailed)
	c.JSON(http.StatusOK, res)
}

// probeVerdict 给整批数据一个"能不能信"的结论 + 下一步。
//
//	# ⚠️ 为什么要查任务状态
//
//	光说「700 条没有探测结果」还不够 —— 人下一个问题必然是"为什么"。
//	而答案往前一步就能拿到：证书探测由 `inspect` 定时任务负责，
//	实测它是**停用且从没跑过**的状态。也就是说这不是探测失败，
//	是探测从来没有发生过（OPSCMDB-031 P0-4 / P1-42 是同一件事的两个面）。
//
//	把这句话说出来，才能把"一片空白"变成"去把那个任务打开"。
//	查不到任务状态时**不要编**：那时只说数据缺失，不说原因。
func (h *CertInspectHandler) probeVerdict(never, failed int) (state, key string, params map[string]any) {
	return probeVerdictDB(h.DB, never, failed)
}

// probeVerdictDB 是上面那个方法的实现，抽成包级是为了让**证书列表页**也能用。
//
// ⚠️ 两处必须共用同一套判词。同一件事两处说法不同，
//
//	人会以为是两个问题 —— 而这里说的恰恰是"你的证书临期提醒不工作"这种要命的事。
//
// ⚠️ 返回值现在是 (state, key, params)。
//
//	原来直接返回拼好的中文长句 —— 那句话恰恰是最不能丢的一句
//	（"你的证书临期提醒根本不工作"），也恰恰是英文界面上永远看不懂的一句。
//	拆成 key + 参数之后，两种语言各写一遍，而**判定逻辑仍然在后端**：
//	前端只负责把它说成人话，不负责判断状况。
//
//	⚠️ 原文里的 `**xxx**` 是 Markdown —— 界面是纯文本渲染，星号会原样显示。
//	强调靠句子本身，不靠标记。
func probeVerdictDB(db *sql.DB, never, failed int) (state, key string, params map[string]any) {
	if never == 0 && failed == 0 {
		return "ok", "", nil
	}
	if never == 0 {
		return "partial", "certs:probe.someFailed", map[string]any{"failed": failed}
	}

	var enabled int
	var lastRun sql.NullTime
	err := db.QueryRow(`SELECT enabled, last_run_at FROM scheduled_tasks WHERE task_key='inspect'`).
		Scan(&enabled, &lastRun)
	params = map[string]any{"never": never}
	switch {
	case err != nil:
		// 查不到任务就只陈述事实，不猜原因
		state, key = "never", "certs:probe.neverUnknownTask"
	case !lastRun.Valid && enabled == 0:
		state, key = "never", "certs:probe.neverTaskDisabled"
	case !lastRun.Valid:
		state, key = "never", "certs:probe.neverTaskNotRun"
	default:
		state, key = "partial", "certs:probe.partialSinceLastRun"
		params["lastRun"] = lastRun.Time.Format("2006-01-02 15:04:05")
	}
	// ⚠️ "另有 N 条探测失败"原来是拼在句尾的。拼接在多语言下会出错
	//	（英文的从句位置和中文不同），所以改成参数：由语言包决定这半句放哪儿、怎么说。
	if failed > 0 {
		params["failed"] = failed
	}
	return state, key, params
}

type certIgnoreIn struct {
	Ignored bool   `json:"ignored"`
	Reason  string `json:"reason"`
}

// Ignore 标记/取消某条解析的证书忽略。
func (h *CertInspectHandler) Ignore(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in certIgnoreIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	v := 0
	reason := ""
	if in.Ignored {
		v = 1
		reason = in.Reason
	}
	res, err := sc.Exec(`UPDATE domain_records SET cert_ignored=?, cert_ignore_reason=? WHERE tenant_id = ? AND id=?`,
		v, reason, c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "record")
		return
	}
	SetAuditTarget(c, c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
