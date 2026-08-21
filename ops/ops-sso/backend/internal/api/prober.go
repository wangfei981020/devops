package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

// ══════════════════════════════════════════════════════════════════
// 拨测执行器
// ══════════════════════════════════════════════════════════════════
//
// # 它做什么，不做什么（先说清楚）
//
// 做：**可达性拨测** —— 定时请求应用的上游地址，记录通不通、多久、什么错。
// 不做：**登录级拨测** —— 用探针凭据真走一遍目标系统的登录。
//
// 为什么先做前者：多数故障是"上游挂了"，不是"登录表单变了"。
// 可达性已经能回答服务状态页那个问题（"是不是只有我进不去"），
// 而登录级需要为每种目标系统写一套登录逻辑，没有通用解 ——
// 那正是这块一直空着的原因。
//
// ⚠️ 因此 probe_credentials 里的 username/secret **目前没有被使用**。
// 字段留着给登录级拨测，但界面必须说清"现在没在用"——
// 让人以为凭据在被使用，比不做还糟：他会以为登录链路被覆盖了。
//
// # 为什么这里不拦内网地址，而 IdP discover 那里拦
//
// 两者的目标性质相反：discover 拉的是**外部** IdP 的 discovery，
// 指向内网就是 SSRF；而拨测的目标**本来就是内网上游**（应用的回源地址），
// 拦掉内网等于把这个功能关掉。
// 二者的安全边界靠"目标从哪来"区分：拨测目标来自 app_routes/apps 的
// 既有配置，不接受请求参数指定 —— 没有"让调用方指定目标"这条路。

const (
	// probeTick 执行器多久醒一次。真正的拨测频率由每条探针的 interval_sec 决定，
	// 这里只是检查"谁到点了"。
	probeTick = 30 * time.Second
	// probeTimeout 单次拨测超时。拨测把被测系统拖垮是这个功能最讽刺的失败模式，
	// 所以宁可判失败也不长等。
	probeTimeout = 5 * time.Second
)

// runProbes 把到点的探针跑一遍。返回这一轮实际拨测了几个。
func runProbes(ctx context.Context, d Deps) (int, error) {
	tctx := store.WithTenant(ctx, store.TenantID(1))

	// 只取到点的：now >= last_probe_at + interval，或者从没测过。
	// 在 SQL 里筛而不是取回来再判，是因为探针可能几百条而到点的通常只有几条。
	rows, err := d.Store.Raw().QueryContext(tctx, `
		SELECT p.id, p.app_id, p.quiet_hours,
		       COALESCE(r.upstream, ''), COALESCE(a.base_url, '')
		FROM probe_credentials p
		JOIN apps a ON a.id = p.app_id AND a.tenant_id = p.tenant_id
		LEFT JOIN app_routes r ON r.app_id = p.app_id AND r.tenant_id = p.tenant_id
		                      AND r.deleted_at IS NULL
		WHERE p.tenant_id = 1
		  AND p.revoked_at IS NULL
		  AND a.status = 'active' AND a.deleted_at IS NULL
		  AND (p.last_probe_at IS NULL
		       OR p.last_probe_at <= NOW() - INTERVAL p.interval_sec SECOND)`)
	if err != nil {
		return 0, err
	}
	type target struct {
		id, appID int64
		quiet     string
		url       string
	}
	var todo []target
	for rows.Next() {
		var t target
		var upstream, baseURL string
		if err := rows.Scan(&t.id, &t.appID, &t.quiet, &upstream, &baseURL); err != nil {
			rows.Close()
			return 0, err
		}
		// 优先回源地址：那才是真正提供服务的那台。base_url 可能是个
		// 走 CDN/网关的对外地址，测它测不出上游死没死。
		t.url = upstream
		if t.url == "" {
			t.url = baseURL
		}
		todo = append(todo, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	done := 0
	for _, t := range todo {
		if inQuietHours(t.quiet, time.Now()) {
			continue
		}
		if t.url == "" {
			// 没有可测的地址：这不是"测失败"，是"没配地址"。
			// 混成 fail 会让人去查上游，而上游根本没被填过。
			saveProbeResult(tctx, d, t.id, "fail", "没有配置上游地址或访问地址，无法拨测")
			done++
			continue
		}
		result, detail := probeOnce(ctx, t.url)
		saveProbeResult(tctx, d, t.id, result, detail)
		done++
	}
	return done, nil
}

// probeOnce 拨一次。返回 ok/fail 与给人看的原因。
func probeOnce(ctx context.Context, raw string) (string, string) {
	url := raw
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	c, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(c, http.MethodGet, url, nil)
	if err != nil {
		return "fail", "地址不合法：" + raw
	}
	// 标明身份：被测系统的日志里要能看出这是拨测，而不是真实用户。
	// 不标的话，客户会把我们每 5 分钟一次的请求当成异常流量来查。
	req.Header.Set("User-Agent", "ops-access-plane-probe/1.0")

	start := time.Now()
	// 不跟跳转：302 到登录页本身就是"活着"的证据，跟过去反而可能
	// 落到一个完全不同的服务上，测的就不是这台了。
	cl := &http.Client{
		Timeout:       probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cl.Do(req)
	took := time.Since(start).Milliseconds()
	if err != nil {
		return "fail", fmt.Sprintf("连不上（%dms）：%s", took, shortErr(err))
	}
	defer resp.Body.Close()

	// 5xx 才算失败。4xx（含 401/403）说明**服务活着**，只是这个请求没权限 ——
	// 把它算成故障，会让每一个需要登录的系统永远显示红色。
	if resp.StatusCode >= 500 {
		return "fail", fmt.Sprintf("HTTP %d（%dms）", resp.StatusCode, took)
	}
	return "ok", fmt.Sprintf("HTTP %d，%dms", resp.StatusCode, took)
}

func saveProbeResult(ctx context.Context, d Deps, id int64, result, detail string) {
	if len([]rune(detail)) > 160 {
		detail = string([]rune(detail)[:160])
	}
	if _, err := d.Store.Raw().ExecContext(ctx,
		`UPDATE probe_credentials SET last_probe_at = NOW(), last_result = ?, last_detail = ?
		 WHERE id = ?`, result, detail, id); err != nil {
		logx.Line("probe", "写拨测结果失败 id="+itoa(id)+": "+err.Error())
	}
}

// inQuietHours 静默窗口，形如 "02:00-04:00"。
//
// 存在的理由是老系统的夜间批处理：那段时间它本来就慢，
// 拨测在这时候报红是噪音，而噪音会让人开始忽略这个指标。
//
// 跨零点（如 "22:00-02:00"）也要支持 —— 静默窗口最常见的就是深夜那段。
func inQuietHours(spec string, now time.Time) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return false
	}
	from, ok1 := parseHM(parts[0])
	to, ok2 := parseHM(parts[1])
	if !ok1 || !ok2 {
		// 格式不对就当没配。**不能当成"全天静默"** ——
		// 那会让一个填错格式的窗口把拨测彻底关掉，而界面上什么都看不出来。
		logx.Line("probe", "静默窗口格式不对，已忽略: "+spec)
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	if from <= to {
		return cur >= from && cur < to
	}
	// 跨零点
	return cur >= from || cur < to
}

func parseHM(s string) (int, bool) {
	s = strings.TrimSpace(s)
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func shortErr(err error) string {
	s := err.Error()
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:120])
	}
	return s
}
