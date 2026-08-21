// 关口网关（数据面）。
//
// 它做四件事，且只做这四件：
//
//  1. 按 Host 找到应用，把请求代理到上游
//  2. 校验会话；没有会话就跳登录
//  3. 两级判定：先「他能不能用这个应用」，再「他能不能调这个接口」
//  4. 注入身份请求头 + 签名，并把每次判定写成一条审计
//
// # 为什么不放在控制面里
//
// 数据面在每个请求的关键路径上，控制面不是。放一起会让「改个策略重启一下」
// 变成全公司登录中断。分开之后控制面可以随便重启，网关只在配置变更时热加载。
//
// # 应用怎么信任这些头
//
// 应用只需读 X-Gate-User，并用共享密钥校验 X-Gate-Signature。
// 网关会**剥掉**客户端自带的所有 X-Gate-* 头 —— 不剥的话，任何人伪造一个头就是任意越权。
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/auth"
	"ops-sso-backend/internal/domain/mfa"
	"ops-sso-backend/internal/domain/pathpolicy"
	"ops-sso-backend/internal/health"
	"ops-sso-backend/internal/secrets"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

var (
	version = "dev"
	gitsha  = "unknown"
	gwName  = "gw-a1"
)

// route 一条 Host → 上游的映射。
type route struct {
	AppID      int64
	AppCode    string
	TenantID   int64
	Upstream   *url.URL
	InjectMode string
}

type gateway struct {
	db      *sql.DB
	st      *store.Store
	authSvc *auth.Service
	access  *access.Repo
	path    *pathpolicy.Repo
	mfa     *mfa.Repo
	box     *secrets.Box
	signKey []byte
	portal  string

	mu     sync.RWMutex
	routes map[string]route
}

func main() {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
			envd("DB_USER", "access"), os.Getenv("DB_PASSWORD"),
			envd("DB_HOST", "127.0.0.1"), envd("DB_PORT", "3306"),
			envd("DB_NAME", "ops_access_plane"))
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil || db.Ping() != nil {
		logx.Line("gateway", "连接数据库失败")
		os.Exit(1)
	}
	// 网关也要能解 MFA 密钥（校验票据时不需要解密钥，但 Repo 构造要它）
	box, err := secrets.New(os.Getenv("OAP_SECRET_KEY"))
	if err != nil {
		logx.Line("gateway", "密钥未就绪: "+err.Error())
		os.Exit(1)
	}

	signKey := os.Getenv("OAP_GATEWAY_SIGN_KEY")
	if len(signKey) < 32 {
		// 没有签名密钥 = 下游无法分辨请求头是不是网关注入的。
		// 这种情况下起服务，等于把「零改造接入」做成了「零防护接入」。
		logx.Line("gateway", "OAP_GATEWAY_SIGN_KEY 未配置或过短（至少 32 位），拒绝启动")
		os.Exit(1)
	}

	// 与控制面同一个理由：中间网络设备会丢掉空闲 TCP，池子要自己先回收
	db.SetMaxOpenConns(64)
	db.SetMaxIdleConns(16)
	db.SetConnMaxIdleTime(30 * time.Second)

	st := store.New(db)
	g := &gateway{
		db: db, st: st,
		authSvc: auth.New(st),
		access:  access.NewRepo(st),
		path:    pathpolicy.NewRepo(st),
		mfa:     mfa.NewRepo(st, box),
		box:     box,
		signKey: []byte(signKey),
		portal:  envd("PORTAL_URL", "http://localhost:5173/login"),
		routes:  map[string]route{},
	}
	if name := os.Getenv("GATEWAY_NAME"); name != "" {
		gwName = name
	}

	if err := g.reload(); err != nil {
		logx.Line("gateway", "加载路由失败: "+err.Error())
		os.Exit(1)
	}
	// 路由热加载：改配置不需要重启网关，重启网关 = 全公司瞬断
	go func() {
		for range time.Tick(30 * time.Second) {
			if err := g.reload(); err != nil {
				logx.Line("gateway", "重载路由失败（沿用旧配置）: "+err.Error())
			}
		}
	}()

	// 健康端口，契约与后端/CMDB 一致。网关是数据面，探针挂了等于全公司登录中断，
	// 所以它比谁都需要"业务端口卡死时仍能被探到"。
	go health.Start(envd("HEALTH_PORT", ":8089"), db)

	addr := ":" + envd("PORT", "8081")
	logx.Line("gateway", fmt.Sprintf("关口网关 %s (%s) %s 监听 %s", version, gitsha, gwName, addr))
	srv := &http.Server{Addr: addr, Handler: g, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		logx.Line("gateway", "退出: "+err.Error())
		os.Exit(1)
	}
}

func (g *gateway) reload() error {
	rows, err := g.db.Query(`SELECT r.host, r.upstream, r.inject_mode, r.app_id, r.tenant_id, a.code
		FROM app_routes r JOIN apps a ON a.id = r.app_id AND a.tenant_id = r.tenant_id
		WHERE r.deleted_at IS NULL AND a.deleted_at IS NULL AND a.status = 'active'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := map[string]route{}
	for rows.Next() {
		var host, upstream, mode, code string
		var appID, tenantID int64
		if err := rows.Scan(&host, &upstream, &mode, &appID, &tenantID, &code); err != nil {
			return err
		}
		u, err := url.Parse(upstream)
		if err != nil {
			logx.Line("gateway", "跳过非法上游 "+upstream+"（host="+host+"）")
			continue
		}
		next[strings.ToLower(host)] = route{AppID: appID, AppCode: code, TenantID: tenantID, Upstream: u, InjectMode: mode}
	}
	g.mu.Lock()
	g.routes = next
	g.mu.Unlock()
	return nil
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := newReqID()

	if r.URL.Path == "/__gate/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"gateway":"` + gwName + `"}`))
		return
	}

	host := strings.ToLower(strings.Split(r.Host, ":")[0])
	g.mu.RLock()
	rt, ok := g.routes[host]
	g.mu.RUnlock()
	if !ok {
		// 未知 Host 一律拒绝，不做默认后端 —— 默认后端会让配错域名变成"莫名其妙能访问"
		http.Error(w, "unknown host", http.StatusNotFound)
		return
	}

	ctx := store.WithTenant(r.Context(), store.TenantID(rt.TenantID))

	// ── 1. 会话 ──
	tok := sessionToken(r)
	id, err := g.authSvc.Authenticate(ctx, tok)
	if err != nil {
		g.record(ctx, rt, r, reqID, 0, "", "deny", "no_session", 0, "", start)
		// 浏览器访问跳登录，接口调用返回 401 —— 给 XHR 跳 302 会让前端拿到一坨 HTML
		if wantsHTML(r) {
			http.Redirect(w, r, g.portal+"?next="+url.QueryEscape("https://"+r.Host+r.URL.RequestURI()), http.StatusFound)
		} else {
			http.Error(w, `{"code":"auth.unauthorized"}`, http.StatusUnauthorized)
		}
		return
	}

	// ── 2. 应用级授权 ──
	sub, err := g.access.LoadSubject(ctx, id.UserID)
	if err != nil {
		g.fail(w, ctx, rt, r, reqID, id, "subject_load_failed", start)
		return
	}
	appRules, err := g.access.LoadRulesForApp(ctx, rt.AppID)
	if err != nil {
		g.fail(w, ctx, rt, r, reqID, id, "policy_load_failed", start)
		return
	}
	groupIDs, err := g.appGroups(ctx, rt.AppID)
	if err != nil {
		g.fail(w, ctx, rt, r, reqID, id, "policy_load_failed", start)
		return
	}
	appDec := access.Evaluate(sub, access.App{ID: rt.AppID, GroupIDs: groupIDs}, appRules)
	if !appDec.Allowed() {
		ruleID := int64(0)
		if appDec.Rule != nil {
			ruleID = appDec.Rule.ID
		}
		g.record(ctx, rt, r, reqID, id.UserID, id.DisplayName, "deny", string(appDec.Reason), ruleID, "", start)
		http.Error(w, `{"code":"access.denied"}`, http.StatusForbidden)
		return
	}

	// ── 3. 接口级判定 ──
	pathRules, err := g.path.LoadForApp(ctx, rt.AppID)
	if err != nil {
		g.fail(w, ctx, rt, r, reqID, id, "policy_load_failed", start)
		return
	}
	ver, _ := g.path.Version(ctx, rt.AppID)
	res := pathpolicy.Evaluate(pathpolicy.Request{
		AppID: rt.AppID, Method: r.Method, Path: r.URL.Path,
		DeviceState: deviceState(r), SourceKind: sourceKind(r), At: time.Now(),
		UserID: sub.UserID, RoleIDs: sub.RoleIDs, GroupIDs: sub.GroupIDs, DeptDepth: sub.DeptDepth,
	}, pathRules)

	ruleID := int64(0)
	if res.Rule != nil {
		ruleID = res.Rule.ID
	}
	switch res.Decision {
	case pathpolicy.Deny:
		g.record(ctx, rt, r, reqID, id.UserID, id.DisplayName, "deny", string(res.Reason), ruleID, ver, start)
		http.Error(w, `{"code":"access.denied"}`, http.StatusForbidden)
		return
	case pathpolicy.Challenge:
		// ★ 先看他手里有没有刚验过的票据。
		//
		// 没有这一步的话，"二次验证"就变成"每个写请求都弹一次验证码"，
		// 人会立刻想办法绕过 —— 比如把 TTL 调到 8 小时，那才是真正的风险。
		if tk, err := g.mfa.FindValidTicket(ctx, id.UserID, rt.AppID, r.URL.Path, time.Now()); err == nil && tk != nil {
			g.mfa.TouchTicket(ctx, tk.ID)
			g.record(ctx, rt, r, reqID, id.UserID, id.DisplayName, "allow", "step_up_ticket", ruleID, ver, start)
			g.proxy(w, r, rt, id, sub, reqID)
			return
		}
		g.record(ctx, rt, r, reqID, id.UserID, id.DisplayName, "challenge", string(res.Reason), ruleID, ver, start)
		// 428 而不是 403：告诉客户端「还差一步」，而不是「不行」。
		// 界面据此弹二次验证，而不是显示一个死胡同。
		w.Header().Set("X-Gate-Challenge", string(res.Reason))
		// 把该带的上下文一并给出去，前端不用自己猜要验什么
		w.Header().Set("X-Gate-App-Id", strconv.FormatInt(rt.AppID, 10))
		http.Error(w, `{"code":"access.challenge_required","reason":"`+string(res.Reason)+`"}`, http.StatusPreconditionRequired)
		return
	}

	// ── 4. 注入身份并转发 ──
	g.record(ctx, rt, r, reqID, id.UserID, id.DisplayName, "allow", string(res.Reason), ruleID, ver, start)
	g.proxy(w, r, rt, id, sub, reqID)
}

// formFillSecret 取表单填充要用的下游凭据。
//
// # 表单填充是给"只有账号密码登录页"的老系统用的
//
// 关口在网关侧代它提交那张表单，凭据**存在关口、永不下发到浏览器** ——
// 这是与"浏览器扩展填充"路线的本质区别：扩展方案必须把密码送到浏览器里，
// 而浏览器是最不该放凭据的地方。
//
// 凭据按 (应用, 用户) 取：支持每人一套，也支持整组共用一套。
func (g *gateway) formFillSecret(ctx context.Context, appID, userID int64) (string, string, bool) {
	q, err := g.st.Tenant(ctx)
	if err != nil {
		return "", "", false
	}
	var user string
	var enc []byte
	// 先找这个人专属的，没有再找共用的（user_id = 0）
	err = q.QueryRow(`SELECT username, secret_enc FROM formfill_credentials
		WHERE tenant_id = ? AND app_id = ? AND (user_id = ? OR user_id = 0) AND revoked_at IS NULL
		ORDER BY user_id DESC LIMIT 1`, appID, userID).Scan(&user, &enc)
	if err != nil {
		return "", "", false
	}
	plain, err := g.box.Open(enc)
	if err != nil {
		logx.Line("gateway", "表单凭据无法解开（换过 OAP_SECRET_KEY？）app="+strconv.FormatInt(appID, 10))
		return "", "", false
	}
	return user, string(plain), true
}

// proxy 转发到上游，注入身份头。
func (g *gateway) proxy(w http.ResponseWriter, r *http.Request, rt route, id auth.Identity, sub access.Subject, reqID string) {
	// 表单填充模式：把凭据以请求头交给上游前置（或由网关直接代提交表单）。
	// 这里给出的是"注入到约定头"的形态 —— 下游只需一个 20 行的前置脚本读它，
	// 比让每个老系统去接 OIDC 现实得多。
	var ffUser, ffPass string
	if rt.InjectMode == "formfill" {
		if u, p, ok := g.formFillSecret(r.Context(), rt.AppID, id.UserID); ok {
			ffUser, ffPass = u, p
		} else {
			// 没配凭据就**不要静默转发**：老系统会弹自己的登录页，
			// 用户看到的是"单点登录没生效"，而我们这边一点痕迹都没有
			logx.Line("gateway", "表单填充未配置凭据，按拒绝处理 app="+rt.AppCode)
			http.Error(w, `{"code":"gateway.formfill_not_configured"}`, http.StatusServiceUnavailable)
			return
		}
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = rt.Upstream.Scheme
			req.URL.Host = rt.Upstream.Host
			req.Host = rt.Upstream.Host

			// ★ 先剥掉客户端自带的所有 X-Gate-*。
			// 不剥的话，任何人加一个 X-Gate-User: admin 就是任意越权 ——
			// 这是身份注入类网关最经典、也最致命的一个漏洞。
			for h := range req.Header {
				if strings.HasPrefix(strings.ToLower(h), "x-gate-") {
					req.Header.Del(h)
				}
			}

			groups := make([]string, 0, len(sub.GroupIDs))
			for gid := range sub.GroupIDs {
				groups = append(groups, strconv.FormatInt(gid, 10))
			}
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			req.Header.Set("X-Gate-User", id.Username)
			// 显示名可能是中文。HTTP 头按 RFC 只保证 latin-1，直接塞进去下游会解成乱码
			// （端到端实测：张伟 → å¼ ä¼Ÿ）。百分号编码后下游 unescape 一次即可。
			req.Header.Set("X-Gate-User-Display", url.QueryEscape(id.DisplayName))
			req.Header.Set("X-Gate-Groups", strings.Join(groups, ","))
			req.Header.Set("X-Gate-Request-Id", reqID)
			req.Header.Set("X-Gate-Timestamp", ts)
			// 签名覆盖 用户+时间戳：下游校验签名即可确认这些头确实来自网关，
			// 时间戳让重放有窗口限制（下游按自己的容忍度判断）
			req.Header.Set("X-Gate-Signature", g.sign(id.Username, ts))
			// 不下发手机号、邮箱以外的个人信息：应用要不到就泄不了
			if strings.Contains(id.Username, "@") {
				req.Header.Set("X-Gate-Email", id.Username)
			}
			// 表单填充：凭据只在这一跳出现，且只发给上游，绝不回到浏览器
			if ffUser != "" {
				req.Header.Set("X-Gate-FormFill-User", ffUser)
				req.Header.Set("X-Gate-FormFill-Pass", ffPass)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// 上游挂了要说清是上游挂了，不能让人以为是权限问题 ——
			// 「502 上游不可达」和「403 你没权限」会引向完全不同的排障方向
			logx.Line("gateway", "上游不可达 "+rt.Upstream.String()+": "+err.Error())
			http.Error(w, `{"code":"gateway.upstream_unreachable"}`, http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

func (g *gateway) sign(user, ts string) string {
	m := hmac.New(sha256.New, g.signKey)
	m.Write([]byte(user + "|" + ts))
	return hex.EncodeToString(m.Sum(nil))
}

func (g *gateway) appGroups(ctx context.Context, appID int64) ([]int64, error) {
	q, err := g.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT group_id FROM app_group_members WHERE tenant_id = ? AND app_id = ?`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// record 写一条访问事件。
//
// 三条不能省的字段：命中规则、**策略版本号**、请求 ID。
// 少了策略版本号，三个月后没人能复现「当时为什么放行」——策略早就改过了。
func (g *gateway) record(ctx context.Context, rt route, r *http.Request, reqID string, uid int64, label, decision, reason string, ruleID int64, ver string, start time.Time) {
	_, err := g.db.ExecContext(ctx, `INSERT INTO access_events
		(tenant_id, request_id, user_id, user_label, app_id, app_code, method, path,
		 decision, reason, matched_rule, policy_version, client_ip, device_state, gateway, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rt.TenantID, reqID, uid, label, rt.AppID, rt.AppCode, r.Method, trunc(r.URL.Path, 512),
		decision, reason, ruleID, ver, clientIP(r), deviceState(r), gwName,
		time.Since(start).Milliseconds())
	if err != nil {
		// 审计写失败不能阻断访问，但必须留痕 —— 静默丢审计比丢访问更糟
		logx.Line("gateway", "写访问事件失败: "+err.Error())
	}
}

func (g *gateway) fail(w http.ResponseWriter, ctx context.Context, rt route, r *http.Request, reqID string, id auth.Identity, reason string, start time.Time) {
	g.record(ctx, rt, r, reqID, id.UserID, id.DisplayName, "deny", reason, 0, "", start)
	// 判定链路本身故障时**拒绝**，不是放行。
	// 「策略读不出来就先放过去」这种设计，等于数据库一抖动全公司门户大开。
	http.Error(w, `{"code":"gateway.policy_unavailable"}`, http.StatusServiceUnavailable)
}

// ── 工具 ──

func sessionToken(r *http.Request) string {
	if ck, err := r.Cookie("oag_session"); err == nil {
		return ck.Value
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func wantsHTML(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// deviceState 设备状态。
//
// 现在靠受管设备证书注入的头判断；没有这个头就是空串 ——
// **空串不等于 unmanaged**。服务账号本来就没有设备，把两者混为一谈
// 会在策略里误杀所有跑批任务（原型预演里抓到过一次）。
func deviceState(r *http.Request) string {
	switch r.Header.Get("X-Device-State") {
	case "managed":
		return "managed"
	case "unmanaged":
		return "unmanaged"
	}
	return ""
}

func sourceKind(r *http.Request) string {
	ip := clientIP(r)
	if strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "172.") {
		return "office"
	}
	return "internet"
}

func clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(strings.Split(f, ",")[0])
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func newReqID() string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (i * 8))
	}
	return "req_" + hex.EncodeToString(b)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func envd(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
