package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/store"
)

// Prometheus 指标导出。
//
// # 为什么全部从库里现算，而不用进程内计数器
//
// 后端跑多副本。进程内计数器意味着每个副本只知道**自己**执行过的那部分规则，
// 而 Service 会把抓取请求轮询到随机一个副本 —— 同一个指标在相邻两次抓取里
// 会在"副本 A 的一半"和"副本 B 的另一半"之间跳，看板上是一条锯齿，
// 而且怎么加副本都对不上真值。
//
// 从库里算的代价是每次抓取几条 SQL（规则数量级，很小），
// 换来的是"哪个副本被抓到都返回同一个值"。这也让它不需要选主。
//
// # 端点鉴权
//
// Prometheus 抓取端用不了 JWT。用 METRICS_TOKEN 做 Bearer 校验；
// 没配就允许匿名并在启动时 WARN —— 指标里含规则名和标签，
// 属于内部信息，公网暴露前必须配上。

// Prometheus 的保留标签。static_labels 里出现这些名字，抓取端会把它们
// 重命名成 exported_xxx，于是你在 PromQL 里怎么查都查不到自己填的那个值。
// 这个坑在 ops-version 上栽过一次（见 project_ops_version_pitfalls）。
var reservedLabels = map[string]bool{
	"job": true, "instance": true, "__name__": true,
}

var labelNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateMetricLabels 校验用户填的静态标签。
//
// 🔴 必须在**保存时**拦，不能等到导出时跳过。
// Prometheus 解析 /metrics 是全有全无的：一条非法标签名会让整个响应
// 解析失败，于是**一条坏规则会让所有规则的指标一起消失**，
// 而界面上那条规则看起来一切正常。
func ValidateMetricLabels(labels map[string]string) error {
	for k := range labels {
		if !labelNameRe.MatchString(k) {
			return fmt.Errorf("标签名 %q 不合法：只能是字母、数字、下划线，且不能以数字开头", k)
		}
		if reservedLabels[k] {
			return fmt.Errorf("标签名 %q 是 Prometheus 保留标签，抓取端会把它重命名成 exported_%s，"+
				"你在 PromQL 里将查不到这个值。请换一个名字", k, k)
		}
		if strings.HasPrefix(k, "__") {
			return fmt.Errorf("标签名 %q 以双下划线开头，那是 Prometheus 的内部命名空间", k)
		}
	}
	return nil
}

// escapeLabelValue 按 Prometheus 文本格式转义标签值。
// 值里的反斜杠、双引号、换行不转义会直接破坏整份响应的解析。
func escapeLabelValue(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

type metricSample struct {
	name   string
	labels map[string]string
	value  float64
}

func (s metricSample) render() string {
	if len(s.labels) == 0 {
		return fmt.Sprintf("%s %g", s.name, s.value)
	}
	keys := make([]string, 0, len(s.labels))
	for k := range s.labels {
		keys = append(keys, k)
	}
	// 排序让输出稳定：不排的话 map 迭代顺序每次都变，
	// diff 两次抓取结果时满屏都是"变化"，实际什么都没变
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, escapeLabelValue(s.labels[k])))
	}
	return fmt.Sprintf("%s{%s} %g", s.name, strings.Join(parts, ","), s.value)
}

// MetricsHandler 输出 Prometheus 文本格式。由 main 挂到健康端口上。
func (s *Server) MetricsHandler(c *gin.Context) {
	if s.metricsToken != "" {
		got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if got != s.metricsToken {
			c.Header("WWW-Authenticate", `Bearer realm="metrics"`)
			c.String(http.StatusUnauthorized, "# 需要 METRICS_TOKEN\n")
			return
		}
	}
	var out strings.Builder
	emit := func(name, help, typ string, samples []metricSample) {
		if len(samples) == 0 {
			return
		}
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
		for _, sm := range samples {
			out.WriteString(sm.render())
			out.WriteByte('\n')
		}
	}

	fmt.Fprintf(&out, "# HELP opsalert_build_info 构建信息\n# TYPE opsalert_build_info gauge\n")
	fmt.Fprintf(&out, "opsalert_build_info{version=\"%s\"} 1\n", escapeLabelValue(s.version))

	// /metrics 没有登录态，所以拿不到租户上下文。
	//
	// ⚠️ 不能"就当是 1 号租户"：多租户部署里会**静默只导出一个租户**，
	// 其余租户的看板全空，而端点返回 200、up 是 1 —— 没有任何迹象说明少了东西。
	// 用共享的 ForEachTenant，与探测/升级/清理保持同一个"哪些租户算数"的口径。
	//
	// ⚠️ tenant 标签用**租户 ID 而不是名字**：名字是可以改的，
	// 改一次 Prometheus 就认成一条全新的时间序列，所有看板和告警的历史断在那一刻。
	var hits, runs, events, lastRun, enabled, failing, incidents, dsUp []metricSample
	var firstErr error
	failed, err := store.ForEachTenant(c.Request.Context(), s.st, "api/metrics.go",
		func(sc *store.Scoped, tid store.TenantID) error {
			tl := map[string]string{"tenant": fmt.Sprint(int64(tid))}
			h, r, e, lr, en, f, inc, ds, err := s.tenantSamples(sc, tl)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return err
			}
			hits, runs, events = append(hits, h...), append(runs, r...), append(events, e...)
			lastRun, enabled, failing = append(lastRun, lr...), append(enabled, en...), append(failing, f...)
			incidents, dsUp = append(incidents, inc...), append(dsUp, ds...)
			return nil
		})
	if err != nil {
		c.String(http.StatusInternalServerError, "# 取租户列表失败: %s\n", err.Error())
		return
	}
	// ⚠️ 任何一个租户失败就整体 5xx，**不能把剩下的导出去**。
	// 部分导出在抓取端表现为"那个租户的指标消失了"，而 up 仍是 1 ——
	// 看板变空但监控说一切正常，正是这个产品要根治的那类误读。
	if failed > 0 {
		c.String(http.StatusInternalServerError, "# %d 个租户的指标查询失败: %v\n", failed, firstErr)
		return
	}

	emit("opsalert_rule_hits_total", "规则累计命中的日志条数", "counter", hits)
	emit("opsalert_rule_events_total", "规则累计产生的事件数", "counter", events)
	emit("opsalert_rule_runs_total", "规则累计执行次数，按结果分", "counter", runs)
	emit("opsalert_rule_last_run_timestamp_seconds", "规则上次执行的时刻。停止推进即代表这条规则不再检测", "gauge", lastRun)
	emit("opsalert_rule_enabled", "规则是否启用", "gauge", enabled)
	emit("opsalert_rule_consecutive_failures", "规则连续执行失败次数。大于 0 时这条规则已经不产生告警了", "gauge", failing)
	emit("opsalert_incidents", "当前未恢复的事件数", "gauge", incidents)
	emit("opsalert_datasource_up", "数据源是否可达。为 0 时依赖它的规则全部静默失效", "gauge", dsUp)

	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.String(http.StatusOK, out.String())
}

// tenantSamples 取一个租户的全部样本。tl 是要贴到每条样本上的租户标签。
func (s *Server) tenantSamples(sc *store.Scoped, tl map[string]string) (
	hits, runs, events, lastRun, enabled, failing, incidents, dsUp []metricSample, err error,
) {
	// ── 规则维度 ────────────────────────────────────────────────
	// 只导出显式开了开关的规则。全导出的话，一个装了几百条规则的租户
	// 会把抓取端灌满，而其中绝大多数没人看。
	rows, err := sc.Query(`SELECT r.id, r.name, r.severity, r.enabled, r.metrics_labels,
		COALESCE(m.hits_total,0), COALESCE(m.runs_ok,0), COALESCE(m.runs_nodata,0),
		COALESCE(m.runs_error,0), COALESCE(m.events_total,0),
		-- ⚠️ UNIX_TIMESTAMP 作用在 DATETIME(3) 上返回的是**小数**（"1755573600.123"），
		-- 直接扫进 int64 会报 "converting driver.Value type []uint8 to a int64"。
		-- 用 float64 接而不是 CAST 成整数：这个 gauge 表达"上次执行时刻"，
		-- 毫秒精度对判断"规则是不是停了"没坏处，而 CAST 会引入一次无谓的截断。
		COALESCE(UNIX_TIMESTAMP(m.last_run_at),0), r.consecutive_failures
		FROM rules r LEFT JOIN rule_metrics m ON m.rule_id = r.id AND m.tenant_id = r.tenant_id
		WHERE r.tenant_id = ? AND r.deleted_at IS NULL AND r.metrics_enabled = 1`)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		var name, severity string
		var isEnabled int
		var rawLabels []byte
		var hitsN, okN, ndN, errN, evN, consecFail int64
		var lastTS float64
		if err = rows.Scan(&id, &name, &severity, &isEnabled, &rawLabels,
			&hitsN, &okN, &ndN, &errN, &evN, &lastTS, &consecFail); err != nil {
			rows.Close()
			return
		}
		base := clone(tl)
		base["rule_id"] = fmt.Sprint(id)
		base["rule"] = name
		var extra map[string]string
		_ = json.Unmarshal(rawLabels, &extra)
		for k, v := range extra {
			// 保存时已经校验过，这里再挡一次：老数据是在校验上线之前写进去的，
			// 一条脏数据能让整份 /metrics 解析失败
			if labelNameRe.MatchString(k) && !reservedLabels[k] && !strings.HasPrefix(k, "__") {
				base[k] = v
			}
		}
		withSev := clone(base)
		withSev["severity"] = severity

		hits = append(hits, metricSample{"opsalert_rule_hits_total", withSev, float64(hitsN)})
		events = append(events, metricSample{"opsalert_rule_events_total", withSev, float64(evN)})
		// 顺序固定：map 迭代顺序随机，不排的话每次抓取的行序都不同
		for _, oc := range []struct {
			name string
			n    int64
		}{{"ok", okN}, {"no_data", ndN}, {"error", errN}} {
			l := clone(base)
			l["outcome"] = oc.name
			runs = append(runs, metricSample{"opsalert_rule_runs_total", l, float64(oc.n)})
		}
		lastRun = append(lastRun, metricSample{"opsalert_rule_last_run_timestamp_seconds", base, lastTS})
		enabled = append(enabled, metricSample{"opsalert_rule_enabled", base, float64(isEnabled)})
		failing = append(failing, metricSample{"opsalert_rule_consecutive_failures", base, float64(consecFail)})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return
	}

	// ── 事件维度 ────────────────────────────────────────────────
	r2, err := sc.Query(`SELECT severity, status, COUNT(*) FROM incidents
		WHERE tenant_id = ? AND status <> 'resolved' GROUP BY severity, status`)
	if err != nil {
		return
	}
	for r2.Next() {
		var sev, st string
		var n int64
		if err = r2.Scan(&sev, &st, &n); err != nil {
			r2.Close()
			return
		}
		l := clone(tl)
		l["severity"], l["status"] = sev, st
		incidents = append(incidents, metricSample{"opsalert_incidents", l, float64(n)})
	}
	r2.Close()
	if err = r2.Err(); err != nil {
		return
	}

	// ── 数据源可达性 ────────────────────────────────────────────
	//
	// 这条是整套指标里最该配告警的一个：数据源不可达时，
	// 所有依赖它的规则都会静默地不再告警，而**事件数会归零**——
	// 看板上和"一切正常"长得完全一样。为它配一条
	// `opsalert_datasource_up == 0` 的告警，比盯着事件数有用得多。
	r3, err := sc.Query(`SELECT id, name, type, status FROM datasources
		WHERE tenant_id = ? AND deleted_at IS NULL`)
	if err != nil {
		return
	}
	for r3.Next() {
		var id int64
		var name, typ, status string
		if err = r3.Scan(&id, &name, &typ, &status); err != nil {
			r3.Close()
			return
		}
		up := 0.0
		if status == "ok" {
			up = 1
		}
		l := clone(tl)
		l["datasource_id"], l["name"], l["type"] = fmt.Sprint(id), name, typ
		dsUp = append(dsUp, metricSample{"opsalert_datasource_up", l, up})
	}
	r3.Close()
	err = r3.Err()
	return
}

func clone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
