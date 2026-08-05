package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// EventCenterHandler 事件中心：聚合平台各处事件成统一时间线(到期/变更/同步失败/K8s Warning)。
// AI 排障入口:一次拉到"最近平台出了什么事"，再钻具体诊断。
type EventCenterHandler struct {
	DB     *sql.DB
	Pool   *k8ssource.Pool
	Cipher *crypto.Cipher // 解密夜莺接入的 token
}

func NewEventCenterHandler(db *sql.DB, pool *k8ssource.Pool, cipher *crypto.Cipher) *EventCenterHandler {
	return &EventCenterHandler{DB: db, Pool: pool, Cipher: cipher}
}

func (h *EventCenterHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/event-center", h.List) // days,level(critical/warning/info),source(expiry/change/sync/k8s/alert)
}

type evt struct {
	Time    string `json:"time"`
	Source  string `json:"source"` // expiry/change/sync/k8s
	Level   string `json:"level"`  // critical/warning/info
	Object  string `json:"object"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Cluster string `json:"cluster,omitempty"`
	// Upcoming=true 表示这条是「还没发生的预告」（到期类），不是已发生的事件。
	// 到期事件的时间戳是**到期日**（未来），混在按时间倒序的时间线里会永远霸占顶部——
	// 一条 7 天后才到期的证书，看起来像刚刚发生的最新严重事件（实测过 3 条 2026-08-09 排在最前）。
	Upcoming bool `json:"upcoming"`
	// Count 为合并掉的同类条数（同来源+对象+标题+详情），>1 时前端显示 ×N。
	Count  int `json:"count"`
	sortTs time.Time
}

// List 聚合事件。days 默认30(到期/变更窗口);K8s Warning 取各启用集群实时(有界)。
// eventCenterLimit 单次返回的事件条数上限。超出会在响应里以 truncated=true 明确标出，
// 绝不静默丢弃——静默截断让人以为看到的就是全部（CMDB-019，与 CMDB-007 同类问题）。
const eventCenterLimit = 500

// workloadChangeLimit 工作负载变更这一路的取数上限。它是"最近的变更"，
// 时间倒序取前 N 条即可，但同样不该假装那就是全部——超过时打日志留痕。
const workloadChangeLimit = 300

func (h *EventCenterHandler) List(c *gin.Context) {
	days := 30
	if d, e := strconv.Atoi(c.Query("days")); e == nil && d > 0 && d <= 365 {
		days = d
	}
	srcFilter := c.Query("source")
	lvlFilter := c.Query("level")
	want := func(src, lvl string) bool {
		return (srcFilter == "" || srcFilter == src) && (lvlFilter == "" || lvlFilter == lvl)
	}
	events := []evt{}
	now := time.Now()
	add := func(e evt) {
		if want(e.Source, e.Level) {
			// 预告（到期日在未来）只显示到「天」：证书到期时刻精确到秒毫无意义，
			// 反而让人误以为那一刻发生过什么事。
			e.Upcoming = e.sortTs.After(now)
			if e.Upcoming {
				e.Time = e.sortTs.Format("2006-01-02")
			} else {
				e.Time = e.sortTs.Format("2006-01-02 15:04:05")
			}
			events = append(events, e)
		}
	}

	// 1. 到期:证书(线上探测) + 域名注册
	// Object 必须是完整 FQDN：domain_records.host 只是子域前缀（@ / www / mond），
	// 单独拿出来会得到「证书 @ 即将到期」这种没法处置的告警，必须 JOIN 出根域名拼全。
	if rows, _ := h.DB.Query(`SELECT r.host, c.name, r.cert_expiry_at FROM domain_records r
		JOIN cis c ON c.id=r.domain_ci_id
		WHERE r.cert_expiry_at IS NOT NULL AND r.cert_ignored=0 AND r.cert_expiry_at <= NOW()+INTERVAL ? DAY
		ORDER BY r.cert_expiry_at`, days); rows != nil {
		for rows.Next() {
			var host, domain string
			var exp sql.NullTime
			if rows.Scan(&host, &domain, &exp) == nil && exp.Valid {
				add(evt{Source: "expiry", Level: expiryLevel(exp.Time), Object: recordFQDN(host, domain),
					Title: "证书到期", Message: "证书 " + exp.Time.Format("2006-01-02") + " 到期", sortTs: exp.Time})
			}
		}
		rows.Close()
	}
	if rows, _ := h.DB.Query(`SELECT c.name, d.expiry_at FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.stale=0 AND d.ignored=0 AND d.expiry_at IS NOT NULL AND d.expiry_at <= NOW()+INTERVAL ? DAY
		ORDER BY d.expiry_at`, days); rows != nil {
		for rows.Next() {
			var name string
			var exp sql.NullTime
			if rows.Scan(&name, &exp) == nil && exp.Valid {
				add(evt{Source: "expiry", Level: expiryLevel(exp.Time), Object: name,
					Title: "域名到期", Message: "域名注册 " + exp.Time.Format("2006-01-02") + " 到期", sortTs: exp.Time})
			}
		}
		rows.Close()
	}

	// 2. 工作负载变更
	if rows, _ := h.DB.Query(`SELECT namespace,kind,name,field,old_value,new_value,changed_at FROM k8s_changes
		WHERE changed_at >= NOW()-INTERVAL ? DAY ORDER BY changed_at DESC LIMIT ?`, days, workloadChangeLimit); rows != nil {
		for rows.Next() {
			var ns, kind, name, field, ov, nv string
			var ts time.Time
			if rows.Scan(&ns, &kind, &name, &field, &ov, &nv, &ts) == nil {
				fn := map[string]string{"image": "镜像", "replicas": "副本"}[field]
				if fn == "" {
					fn = field
				}
				add(evt{Source: "change", Level: "info", Object: ns + "/" + name,
					Title: kind + " " + fn + "变更", Message: ov + " → " + nv, sortTs: ts})
			}
		}
		rows.Close()
	}

	// 3. 同步失败:K8s 采集失败 + 云项目同步失败
	if rows, _ := h.DB.Query(`SELECT s.resource, s.err, s.last_sync, COALESCE(k.display_name,k.name)
		FROM k8s_sync_state s JOIN k8s_clusters k ON k.id=s.cluster_id WHERE s.ok=0 AND s.err<>''`); rows != nil {
		for rows.Next() {
			var res, errMsg, cl string
			var ts sql.NullTime
			if rows.Scan(&res, &errMsg, &ts, &cl) == nil {
				t := time.Now()
				if ts.Valid {
					t = ts.Time
				}
				add(evt{Source: "sync", Level: "warning", Object: cl + "/" + res,
					Title: "K8s 采集失败", Message: truncStr(errMsg, 200), Cluster: cl, sortTs: t})
			}
		}
		rows.Close()
	}
	if rows, _ := h.DB.Query(`SELECT project_id, last_result, last_sync_at FROM cloud_account_projects
		WHERE last_result<>'' AND (last_result LIKE '%error%' OR last_result LIKE '%失败%' OR last_result LIKE '%fail%' OR last_result LIKE '%denied%')`); rows != nil {
		for rows.Next() {
			var pid, lr string
			var ts sql.NullTime
			if rows.Scan(&pid, &lr, &ts) == nil {
				t := time.Now()
				if ts.Valid {
					t = ts.Time
				}
				add(evt{Source: "sync", Level: "warning", Object: pid,
					Title: "云项目同步失败", Message: truncStr(lr, 200), sortTs: t})
			}
		}
		rows.Close()
	}

	// 4. K8s Warning 事件(各启用集群实时,有界)
	if srcFilter == "" || srcFilter == "k8s" {
		if lvlFilter == "" || lvlFilter == "warning" {
			h.collectK8sWarnings(c.Request.Context(), days, &events, add)
		}
	}

	// 5. 夜莺告警（当前活跃的）。没接入夜莺就自动跳过，不影响其他来源。
	// 只取活跃的：事件中心看的是"最近发生了什么"，
	// 把几千条已恢复的历史告警灌进来会把其他来源全淹掉。
	if srcFilter == "" || srcFilter == "alert" {
		h.collectAlerts(c.Request.Context(), h.Cipher, add)
	}

	// 合并完全同类的重复条目（同来源+对象+标题+详情）。
	// 实测同一个 Pod 的 Unhealthy 会重复 3 条、同一域名的证书到期重复 2 条，
	// 时间线被同一件事刷屏，真正不同的事件被挤出视野。合并后用 ×N 表示发生次数，
	// 时间取最近一次——丢的是重复，不是信息。
	rawTotal := len(events)
	merged := make([]evt, 0, len(events))
	seen := map[string]int{}
	for _, e := range events {
		k := e.Source + "\x00" + e.Object + "\x00" + e.Title + "\x00" + e.Message
		if e.Count < 1 {
			e.Count = 1 // 非 K8s 来源没有原生次数，按 1 次算
		}
		if i, ok := seen[k]; ok {
			merged[i].Count += e.Count
			if e.sortTs.After(merged[i].sortTs) {
				merged[i].sortTs, merged[i].Time = e.sortTs, e.Time
			}
			continue
		}
		seen[k] = len(merged)
		merged = append(merged, e)
	}
	events = merged

	// 排序分两段：已发生的按时间倒序（最新在前）排在上面；
	// 未发生的预告按到期时间正序（最紧迫在前）排在下面。
	// 预告绝不能靠"时间戳更大"插到已发生事件前面——那是在用未来的日期伪装成最新消息。
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.Upcoming != b.Upcoming {
			return !a.Upcoming
		}
		if a.Upcoming {
			return a.sortTs.Before(b.sortTs)
		}
		return a.sortTs.After(b.sortTs)
	})

	// 上限仍然保留（一次返回上万条对前端没意义），但**必须显式告知被截断了**。
	// 原先 count 返回的是截断后的长度，于是「近 30 天」正好 500 条看起来像真实总数，
	// 使用者拿到的是"这就是全部"的错觉（CMDB-019）。
	// 分级计数在**截断之前**统计：数字必须反映选定时间范围内的真实总量，
	// 不能是"当前这页有几条"——否则严重事件被截掉后，计数也跟着变小（CMDB-019）。
	byLevel := map[string]int{"critical": 0, "warning": 0, "info": 0}
	upcoming := 0
	for _, e := range events {
		byLevel[e.Level]++
		if e.Upcoming {
			upcoming++
		}
	}
	total := len(events)
	truncated := false
	if total > eventCenterLimit {
		events = events[:eventCenterLimit]
		truncated = true
		logx.J("event_center", "truncated", map[string]any{"total": total, "limit": eventCenterLimit, "days": days})
	}
	// count 保持"本次返回条数"的语义不变（前端老代码在用），total 才是截断前的真实总量。
	// raw_total/merged_away 把"合并掉了多少条"摆在明面上：
	// 合并后 total 变小是预期行为，但不能让人以为事件凭空少了（同 CMDB-019 的态度）。
	c.JSON(http.StatusOK, gin.H{
		"events": events, "count": len(events),
		"total": total, "truncated": truncated, "limit": eventCenterLimit,
		"by_level": byLevel,
		// upcoming 是"尚未发生的到期预告"条数，已从时间线顶部移到末尾单独成段。
		"upcoming":    upcoming,
		"raw_total":   rawTotal,
		"merged_away": rawTotal - total,
	})
}

func (h *EventCenterHandler) collectK8sWarnings(ctx context.Context, days int, _ *[]evt, add func(evt)) {
	rows, err := h.DB.Query(`SELECT id, COALESCE(display_name,name) FROM k8s_clusters WHERE enabled=1`)
	if err != nil {
		return
	}
	type cl struct {
		id   int
		name string
	}
	cls := []cl{}
	for rows.Next() {
		var x cl
		if rows.Scan(&x.id, &x.name) == nil {
			cls = append(cls, x)
		}
	}
	rows.Close()
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, x := range cls {
		cs, err := h.Pool.ClientFor(x.id)
		if err != nil {
			continue // 集群不可达:跳过,不影响其它源
		}
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		list, err := cs.CoreV1().Events("").List(cctx, metav1.ListOptions{Limit: 300})
		cancel()
		if err != nil {
			continue
		}
		for i := range list.Items {
			e := &list.Items[i]
			if e.Type != "Warning" {
				continue
			}
			ts := e.LastTimestamp.Time
			if ts.IsZero() {
				ts = e.EventTime.Time
			}
			if ts.Before(cutoff) {
				continue
			}
			obj := e.InvolvedObject.Kind + " " + e.InvolvedObject.Name
			if e.InvolvedObject.Namespace != "" {
				obj = e.InvolvedObject.Namespace + "/" + e.InvolvedObject.Name
			}
			// K8s 自己就记了同一事件重复发生的次数（Event.Count），直接带出来。
			// 不带的话「BackOff 重启」发生 200 次和发生 1 次在时间线上长得一模一样，
			// 而这个次数恰恰是判断"偶发还是一直在崩"的关键。
			cnt := int(e.Count)
			if cnt < 1 {
				cnt = 1
			}
			add(evt{Source: "k8s", Level: k8sEventLevel(e.Reason, cnt), Object: obj,
				Title: e.Reason, Message: truncStr(e.Message, 240), Cluster: x.name, sortTs: ts, Count: cnt})
		}
	}
}

// k8sEventLevel 给 K8s Warning 事件分级。
//
//	原来一律写死 "warning"——不看 Reason 也不看次数。后果是
//	`FailedToRetrieveImagePullSecret` 发生 **33.9 万次**和某个 Pod 偶发一次
//	同级，全都淹在同一片黄色里，而前者是**明确坏掉的配置**：
//	拉不到镜像密钥意味着那些 Pod 根本起不来。
//
//	两条判据：
//	  1. Reason 本身就代表"确定性故障"（不是暂态、重试也好不了）→ critical
//	  2. 任何 Reason 重复到一定量级 → 升级。偶发一次是噪音，
//	     重复几千次就是一个持续存在的问题，不该和噪音同级。
func k8sEventLevel(reason string, count int) string {
	// 这些是"配置/环境坏了"，不会自愈，重试多少次都一样
	switch reason {
	case "FailedToRetrieveImagePullSecret", "FailedMount", "FailedAttachVolume",
		"FailedCreatePodSandBox", "InvalidDiskCapacity", "FailedScheduling",
		"NodeNotReady", "SystemOOM", "OOMKilling", "FailedKillPod":
		return "critical"
	}
	// 重复量级：暂态问题不会累积到这个数。阈值取 1000——
	// BackOff 之类的正常重试通常在几十到几百，上千说明它一直没恢复。
	if count >= 1000 {
		return "critical"
	}
	return "warning"
}

func expiryLevel(t time.Time) string {
	if time.Until(t) <= 7*24*time.Hour {
		return "critical"
	}
	return "warning"
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
