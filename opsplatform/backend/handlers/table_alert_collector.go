package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"opsplatform/database"
	"opsplatform/services"

	"github.com/google/uuid"
)

// ============================================================================
// 桌台维护告警 —— 采集器 + 告警引擎
//
// 采集：按环境各自的 interval_sec 定时拉 /gameRoom/list，原样记录响应，
//       解析出每张桌台的「启停状态」和「是否维护中」。
// 判定：gameRoomMaintainList 非空数组 = 维护中（status 是启停，与维护无关）。
// 告警：维护超过阈值后按间隔重复发 Lark，可发多个群、可艾特指定人，
//       人工确认后静默，恢复正常时发一条恢复通知。
//
// 日志：全部走 DEBUG 级别打到控制台（前缀 [table-alert]），
//       便于直接 kubectl logs / Loki 里排查，不需要进数据库翻。
// ============================================================================

// ⚠️ 所有业务时间戳一律由 Go 侧生成后作为参数传入，绝不用 SQL 里的 NOW()。
// 原因：数据库容器和应用容器的时区可能不一致（本地实测 MySQL 是 UTC、应用是 CST），
// 一边用 NOW() 写、一边用 time.Now() 比，维护时长会凭空多出 8 小时。
// driver 带 loc=Local，Go 传参时会正确编码，跟数据库服务器时区无关。

const taLogPrefix = "[table-alert]"

// taDebugEnabled 调试日志开关。默认开，调稳之后可通过环境变量 TABLE_ALERT_DEBUG=0 关掉。
var taDebugEnabled = true

func taDebugf(format string, args ...interface{}) {
	if !taDebugEnabled {
		return
	}
	log.Printf(taLogPrefix+"[DEBUG] "+format, args...)
}

func taInfof(format string, args ...interface{}) {
	log.Printf(taLogPrefix+"[INFO] "+format, args...)
}

func taErrorf(format string, args ...interface{}) {
	log.Printf(taLogPrefix+"[ERROR] "+format, args...)
}

// ---------------------------------------------------------------------------
// 类型
// ---------------------------------------------------------------------------

// TAEnv 环境配置
type TAEnv struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort_order"`

	URL           string `json:"url"`
	Method        string `json:"method"`
	HostHeader    string `json:"host_header"`
	RequestBody   string `json:"request_body"`
	ExtraHeaders  string `json:"extra_headers"`
	Token         string `json:"token"`
	TokenPlace    string `json:"token_place"`
	SkipTLSVerify bool   `json:"skip_tls_verify"`
	TimeoutSec    int    `json:"timeout_sec"`
	CurPage       int    `json:"cur_page"`
	PageSize      int    `json:"page_size"`

	DataPath     string `json:"data_path"`
	TotalPath    string `json:"total_path"`
	FRoomID      string `json:"f_room_id"`
	FTableNo     string `json:"f_table_no"`
	FRoomNo      string `json:"f_room_no"`
	FPlatformID  string `json:"f_platform_id"`
	FStatus      string `json:"f_status"`
	FMaintain    string `json:"f_maintain"`
	FOperator    string `json:"f_operator"`
	FUpdateTime  string `json:"f_update_time"`
	FOnlineTotal string `json:"f_online_total"`

	MaintainRule        string `json:"maintain_rule"`
	MaintainStatusValue string `json:"maintain_status_value"`

	IntervalSec    int  `json:"interval_sec"`
	LogRawResponse bool `json:"log_raw_response"`

	LastCollectAt    *string `json:"last_collect_at"`
	LastCollectOK    bool    `json:"last_collect_ok"`
	LastCollectError string  `json:"last_collect_error"`
	LastCollectCount int     `json:"last_collect_count"`
	LastDurationMs   int     `json:"last_duration_ms"`
	LastHTTPStatus   int     `json:"last_http_status"`
}

// taRoomSnapshot 一次采集解析出来的单张桌台
type taRoomSnapshot struct {
	RoomID      string
	TableNo     string
	RoomNo      string
	PlatformID  string
	Status      string
	Maintaining bool
	SiteCount   int
	OnlineTotal int
	Operator    string
	UpdateTime  string
}

// taChange 状态变化（只记关心的字段，onlineUserTotal 这类高频波动一律忽略）
type taChange struct {
	TableNo string `json:"table_no"`
	RoomNo  string `json:"room_no"`
	Field   string `json:"field"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// TACollectResult 采集结果
type TACollectResult struct {
	EnvName      string     `json:"env_name"`
	OK           bool       `json:"ok"`
	HTTPStatus   int        `json:"http_status"`
	DurationMs   int        `json:"duration_ms"`
	RequestURL   string     `json:"request_url"`
	RecordCount  int        `json:"record_count"`
	TotalCount   int        `json:"total_count"`
	EnableCount  int        `json:"enable_count"`
	DisableCount int        `json:"disable_count"`
	MaintainCnt  int        `json:"maintain_count"`
	Changes      []taChange `json:"changes"`
	Error        string     `json:"error"`
	RawSize      int        `json:"raw_size"`
}

// ---------------------------------------------------------------------------
// 调度器
// ---------------------------------------------------------------------------

var (
	taSchedulerOnce sync.Once
	taCollectMu     sync.Mutex // 同一时刻只允许一个环境在采集，避免并发打爆中台
)

// StartTableAlertScheduler 启动采集调度器。
// 每 10 秒扫一次环境表，到点的环境就采集一次 —— 这样页面上改了 interval_sec 立刻生效，
// 不需要重启服务。
func StartTableAlertScheduler() {
	taSchedulerOnce.Do(func() {
		go taSchedulerLoop()
		go taAlertLoop()
	})
}

func taSchedulerLoop() {
	taInfof("采集调度器已启动（每 10s 检查一次到期环境）")
	// 启动后等一会儿再跑，避开服务刚起来时的初始化
	time.Sleep(15 * time.Second)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 多副本下只让一个实例真正去采集，否则中台会被打两遍
		if !taAcquireLeader("collector", 30) {
			continue
		}
		envs, err := taListEnvs(true)
		if err != nil {
			taErrorf("读取环境列表失败: %v", err)
			continue
		}
		if len(envs) == 0 {
			continue
		}
		for _, env := range envs {
			if taShouldCollect(env) {
				e := env
				func() {
					taCollectMu.Lock()
					defer taCollectMu.Unlock()
					if _, err := TACollectOnce(&e); err != nil {
						taErrorf("env=%s 采集失败: %v", e.Name, err)
					}
				}()
			}
		}
	}
}

// taShouldCollect 距上次采集是否已超过该环境配置的间隔
func taShouldCollect(env TAEnv) bool {
	if !env.Enabled || strings.TrimSpace(env.URL) == "" {
		return false
	}
	if env.LastCollectAt == nil || *env.LastCollectAt == "" {
		return true
	}
	last, err := time.ParseInLocation("2006-01-02 15:04:05", *env.LastCollectAt, time.Local)
	if err != nil {
		// 解析不了就当作该采了，总比一直不采强
		return true
	}
	iv := env.IntervalSec
	if iv < 10 {
		iv = 10
	}
	return time.Since(last) >= time.Duration(iv)*time.Second
}

// ---------------------------------------------------------------------------
// 采集主流程
// ---------------------------------------------------------------------------

// TACollectOnce 采集一个环境：发请求 → 原样落日志 → 解析 → 比对 → 更新快照 → 维护事件
func TACollectOnce(env *TAEnv) (*TACollectResult, error) {
	started := time.Now()
	res := &TACollectResult{EnvName: env.Name}

	reqURL, req, err := taBuildRequest(env)
	if err != nil {
		res.Error = err.Error()
		taErrorf("env=%s 构造请求失败: %v", env.Name, err)
		taFinishCollect(env, res, started, "")
		return res, err
	}
	res.RequestURL = reqURL

	taDebugf("env=%s 开始采集 ----------------------------------------", env.Name)
	taDebugf("env=%s 请求 %s %s", env.Name, env.Method, reqURL)
	if env.HostHeader != "" {
		taDebugf("env=%s 请求头 Host: %s", env.Name, env.HostHeader)
	}
	if env.TokenPlace != "" && env.TokenPlace != "none" && env.Token != "" {
		// 只说明 token 放哪了，绝不打印 token 本身
		taDebugf("env=%s 已附带 token（位置 %s，值已隐藏）", env.Name, env.TokenPlace)
	}
	if env.Method == "POST" && env.RequestBody != "" {
		taDebugf("env=%s 请求体 %s", env.Name, env.RequestBody)
	}

	client := &http.Client{
		Timeout: time.Duration(taDefaultInt(env.TimeoutSec, 10)) * time.Second,
	}
	if env.SkipTLSVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 内网网关证书过期时使用，由使用者显式打开
		}
		taDebugf("env=%s 已跳过 TLS 证书校验", env.Name)
	}

	resp, err := client.Do(req)
	if err != nil {
		res.DurationMs = int(time.Since(started).Milliseconds())
		res.Error = err.Error()
		taErrorf("env=%s 请求失败（耗时 %dms）: %v", env.Name, res.DurationMs, err)
		taFinishCollect(env, res, started, "")
		return res, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	res.DurationMs = int(time.Since(started).Milliseconds())
	res.HTTPStatus = resp.StatusCode
	res.RawSize = len(raw)
	if err != nil {
		res.Error = "读取响应体失败: " + err.Error()
		taErrorf("env=%s %s", env.Name, res.Error)
		taFinishCollect(env, res, started, "")
		return res, err
	}

	taDebugf("env=%s HTTP %d 耗时 %dms 响应 %d 字节", env.Name, resp.StatusCode, res.DurationMs, len(raw))

	// 调试期把完整响应原样打到控制台。稳定后把 log_raw_response 关掉即可。
	if env.LogRawResponse {
		taDebugf("env=%s RAW_RESPONSE >>>\n%s\n<<< RAW_RESPONSE END", env.Name, string(raw))
	}

	if resp.StatusCode >= 400 {
		res.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, taTruncate(string(raw), 500))
		taErrorf("env=%s %s", env.Name, res.Error)
		// 非 2xx 一律把响应体留全量，方便事后查
		taFinishCollect(env, res, started, string(raw))
		return res, fmt.Errorf(res.Error)
	}

	snaps, total, err := taParseRooms(env, raw)
	if err != nil {
		res.Error = "解析失败: " + err.Error()
		taErrorf("env=%s %s", env.Name, res.Error)
		taFinishCollect(env, res, started, string(raw)) // 解析失败留全量
		return res, err
	}

	res.OK = true
	res.RecordCount = len(snaps)
	res.TotalCount = total
	for _, s := range snaps {
		switch {
		case s.Maintaining:
			res.MaintainCnt++
		}
		if strings.EqualFold(s.Status, "Enable") {
			res.EnableCount++
		} else if strings.EqualFold(s.Status, "Disable") {
			res.DisableCount++
		}
	}

	taDebugf("env=%s 解析成功 records=%d total=%d", env.Name, res.RecordCount, res.TotalCount)
	taInfof("env=%s 维护中 %d 台 / Enable %d / Disable %d",
		env.Name, res.MaintainCnt, res.EnableCount, res.DisableCount)

	// 比对 + 落库
	changes, err := taApplySnapshots(env, snaps)
	if err != nil {
		taErrorf("env=%s 写入快照失败: %v", env.Name, err)
	}
	res.Changes = changes
	if len(changes) > 0 {
		taInfof("env=%s 状态变化 %d 条:", env.Name, len(changes))
		for _, c := range changes {
			taInfof("env=%s   %s(%s) %s: %s → %s", env.Name, c.TableNo, c.RoomNo, c.Field, c.From, c.To)
		}
	} else {
		taDebugf("env=%s 无状态变化", env.Name)
	}

	rawToStore := ""
	if env.LogRawResponse {
		rawToStore = string(raw)
	}
	taFinishCollect(env, res, started, rawToStore)
	taDebugf("env=%s 采集结束 ----------------------------------------", env.Name)
	return res, nil
}

// taBuildRequest 按环境配置拼出请求。地址、token 全部来自配置，代码里不含任何硬编码地址。
func taBuildRequest(env *TAEnv) (string, *http.Request, error) {
	base := strings.TrimSpace(env.URL)
	if base == "" {
		return "", nil, fmt.Errorf("未配置请求地址")
	}

	// 分页参数：curPage 不传接口会 NPE 返回 9999，必须带上
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	reqURL := fmt.Sprintf("%s%scurPage=%d&pageSize=%d",
		base, sep, taDefaultInt(env.CurPage, 1), taDefaultInt(env.PageSize, 500))

	method := strings.ToUpper(strings.TrimSpace(env.Method))
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if method == "POST" && env.RequestBody != "" {
		bodyReader = bytes.NewReader([]byte(env.RequestBody))
	}

	// token 放 query 的情况，先拼进 URL
	if env.TokenPlace == "query" && env.Token != "" {
		reqURL += "&token=" + env.Token
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return reqURL, nil, err
	}

	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Host 头：按 IP 直连网关时必填，否则 Istio 匹配不到 VirtualService 直接 404
	if h := strings.TrimSpace(env.HostHeader); h != "" {
		req.Host = h
		req.Header.Set("Host", h)
	}

	// 额外请求头
	if env.ExtraHeaders != "" {
		var extra map[string]string
		if err := json.Unmarshal([]byte(env.ExtraHeaders), &extra); err == nil {
			for k, v := range extra {
				req.Header.Set(k, v)
			}
		} else {
			taErrorf("env=%s extra_headers 不是合法 JSON，已忽略: %v", env.Name, err)
		}
	}

	// token
	if env.Token != "" {
		switch env.TokenPlace {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+env.Token)
		case "raw":
			req.Header.Set("Authorization", env.Token)
		case "token_header":
			req.Header.Set("token", env.Token)
		}
	}

	return reqURL, req, nil
}

// taParseRooms 按配置的路径和字段名解析响应
func taParseRooms(env *TAEnv, raw []byte) ([]taRoomSnapshot, int, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, 0, fmt.Errorf("响应不是合法 JSON: %w", err)
	}

	// 业务错误码：中台成功是 "0000"
	if code, ok := root["code"]; ok {
		cs := fmt.Sprintf("%v", code)
		if cs != "0000" && cs != "0" && cs != "200" {
			msg := ""
			if m, ok := root["msg"]; ok {
				msg = fmt.Sprintf("%v", m)
			}
			return nil, 0, fmt.Errorf("接口返回业务错误 code=%s msg=%s", cs, msg)
		}
	}

	node := taDigPath(root, taDefaultStr(env.DataPath, "data.records"))
	arr, ok := node.([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("路径 %s 下不是数组（实际 %T），检查「数据路径」配置",
			taDefaultStr(env.DataPath, "data.records"), node)
	}

	total := 0
	if tn := taDigPath(root, taDefaultStr(env.TotalPath, "data.total")); tn != nil {
		total = taToInt(tn)
	}

	out := make([]taRoomSnapshot, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		s := taRoomSnapshot{
			RoomID:      taToStr(m[taDefaultStr(env.FRoomID, "id")]),
			TableNo:     taToStr(m[taDefaultStr(env.FTableNo, "tableNo")]),
			RoomNo:      taToStr(m[taDefaultStr(env.FRoomNo, "roomNo")]),
			PlatformID:  taToStr(m[taDefaultStr(env.FPlatformID, "gamePlatformId")]),
			Status:      taToStr(m[taDefaultStr(env.FStatus, "status")]),
			OnlineTotal: taToInt(m[taDefaultStr(env.FOnlineTotal, "onlineUserTotal")]),
			Operator:    taToStr(m[taDefaultStr(env.FOperator, "operator")]),
			UpdateTime:  taToStr(m[taDefaultStr(env.FUpdateTime, "updateTime")]),
		}
		if s.RoomID == "" {
			// 没有主键就用桌台号兜底，否则没法去重
			s.RoomID = s.TableNo
		}

		// ===== 维护判定 =====
		// 实测：维护中的桌台 status 依然是 Enable，真正变化的是 gameRoomMaintainList
		//       从 null 变成 [{siteId, source}...]，所以默认按「该字段非空数组」判定。
		switch env.MaintainRule {
		case "status_equals":
			s.Maintaining = env.MaintainStatusValue != "" &&
				strings.EqualFold(s.Status, env.MaintainStatusValue)
		default: // list_not_empty
			mv := m[taDefaultStr(env.FMaintain, "gameRoomMaintainList")]
			if lst, ok := mv.([]interface{}); ok && len(lst) > 0 {
				s.Maintaining = true
				s.SiteCount = len(lst)
			}
		}

		out = append(out, s)
	}
	return out, total, nil
}

// taApplySnapshots 写快照并比对出变化。
// 只比对「维护状态 / 启停状态 / 操作人」三个字段 —— onlineUserTotal 这类每分钟都在抖的
// 字段一律不比，否则日志会被噪音淹没（实测一次采集有 20+ 张桌台的在线人数 ±1）。
func taApplySnapshots(env *TAEnv, snaps []taRoomSnapshot) ([]taChange, error) {
	changes := []taChange{}
	now := time.Now()

	for _, s := range snaps {
		var (
			oldStatus      string
			oldMaintaining bool
			oldOperator    string
			maintainSince  sql.NullTime
			oldEstimated   bool
			exists         bool
		)
		err := database.DB.QueryRow(`
			SELECT status, maintaining, operator, maintain_since, since_estimated
			FROM table_alert_rooms WHERE env_id = ? AND room_id = ?`,
			env.ID, s.RoomID).Scan(&oldStatus, &oldMaintaining, &oldOperator, &maintainSince, &oldEstimated)
		switch {
		case err == sql.ErrNoRows:
			exists = false
		case err != nil:
			taErrorf("env=%s 读取桌台 %s 旧状态失败: %v", env.Name, s.TableNo, err)
			continue
		default:
			exists = true
		}

		// 计算 maintain_since —— 维护时长以它为准，分两种来源：
		//
		//   精确：本地观测到「正常 → 维护中」这个跃迁，跃迁时刻就是维护开始。
		//   估算：首次采集到这张桌台时它**已经在维护**（系统刚上线、或新加的环境），
		//         这时没有跃迁可观测，用接口的 updateTime 回溯。
		//
		// 早先这里一律用 now，结果是：系统上线时已经维护了 5 小时的桌台显示「0 分钟」，
		// 还要再等一个阈值才告警，严重低估。updateTime 确实会被任何编辑操作刷新、
		// 不够可靠，但在「首次发现」这个场景下，一个可能偏晚的估算值远好过确定错误的 0。
		// 估算出来的用 since_estimated 标记，页面上要让人一眼看出哪些时长不精确。
		var newSince interface{}
		newEstimated := false
		switch {
		case s.Maintaining && !exists:
			// 首次见到就是维护中 —— 回溯
			if t := taParseRemoteTime(s.UpdateTime); !t.IsZero() && t.Before(now) {
				newSince = t
				newEstimated = true
				taDebugf("env=%s 桌台 %s 首次采集即处于维护，按接口 updateTime 回溯到 %s（估算）",
					env.Name, s.TableNo, t.Format("2006-01-02 15:04:05"))
			} else {
				newSince = now
			}
		case s.Maintaining && !oldMaintaining:
			// 观测到跃迁 —— 精确，且覆盖掉之前可能的估算标记
			newSince = now
		case s.Maintaining && maintainSince.Valid:
			newSince = maintainSince.Time
			newEstimated = oldEstimated
		case s.Maintaining:
			newSince = now
		default:
			newSince = nil
		}

		if exists {
			if oldMaintaining != s.Maintaining {
				changes = append(changes, taChange{
					TableNo: s.TableNo, RoomNo: s.RoomNo, Field: "维护状态",
					From: taMaintainLabel(oldMaintaining), To: taMaintainLabel(s.Maintaining),
				})
			}
			if oldStatus != s.Status {
				changes = append(changes, taChange{
					TableNo: s.TableNo, RoomNo: s.RoomNo, Field: "启停状态",
					From: oldStatus, To: s.Status,
				})
			}
			if oldOperator != s.Operator && s.Operator != "" {
				changes = append(changes, taChange{
					TableNo: s.TableNo, RoomNo: s.RoomNo, Field: "操作人",
					From: oldOperator, To: s.Operator,
				})
			}
			_, err = database.DB.Exec(`
				UPDATE table_alert_rooms
				SET table_no=?, room_no=?, platform_id=?, status=?, maintaining=?, maintain_site_count=?,
				    online_user_total=?, operator=?, remote_update_time=?, maintain_since=?,
				    since_estimated=?, last_seen_at=?
				WHERE env_id=? AND room_id=?`,
				s.TableNo, s.RoomNo, s.PlatformID, s.Status, s.Maintaining, s.SiteCount,
				s.OnlineTotal, s.Operator, s.UpdateTime, newSince, newEstimated, now, env.ID, s.RoomID)
		} else {
			if s.Maintaining {
				changes = append(changes, taChange{
					TableNo: s.TableNo, RoomNo: s.RoomNo, Field: "维护状态",
					From: "(首次发现)", To: "维护中",
				})
			}
			_, err = database.DB.Exec(`
				INSERT INTO table_alert_rooms
				  (id, env_id, room_id, table_no, room_no, platform_id, status, maintaining,
				   maintain_site_count, online_user_total, operator, remote_update_time, maintain_since,
				   since_estimated, first_seen_at, last_seen_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				uuid.New().String(), env.ID, s.RoomID, s.TableNo, s.RoomNo, s.PlatformID,
				s.Status, s.Maintaining, s.SiteCount, s.OnlineTotal, s.Operator, s.UpdateTime, newSince,
				newEstimated, now, now)
		}
		if err != nil {
			taErrorf("env=%s 写入桌台 %s 失败: %v", env.Name, s.TableNo, err)
			continue
		}

		// 维护事件：开始 / 结束
		taSyncEvent(env, s, exists && oldMaintaining, newSince, newEstimated)
	}

	return changes, nil
}

// taSyncEvent 维护开始时开事件单，维护结束时收单
func taSyncEvent(env *TAEnv, s taRoomSnapshot, wasMaintaining bool, since interface{}, estimated bool) {
	switch {
	case s.Maintaining && !wasMaintaining:
		// 新开一单（先查有没有未结束的，防止重复开）
		var cnt int
		database.DB.QueryRow(`
			SELECT COUNT(*) FROM table_alert_events
			WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL`, env.ID, s.RoomID).Scan(&cnt)
		if cnt > 0 {
			return
		}
		_, err := database.DB.Exec(`
			INSERT INTO table_alert_events
			  (id, env_id, env_name, room_id, table_no, room_no, platform_id,
			   maintain_start_at, start_estimated, site_count, operator, state, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?, 'pending',?,?)`,
			uuid.New().String(), env.ID, env.Name, s.RoomID, s.TableNo, s.RoomNo,
			s.PlatformID, taEventStart(since), estimated, s.SiteCount, s.Operator, time.Now(), time.Now())
		if err != nil {
			taErrorf("env=%s 桌台 %s 开事件单失败: %v", env.Name, s.TableNo, err)
			return
		}
		startLabel := "实测跃迁"
		if estimated {
			startLabel = "按 updateTime 回溯（估算）"
		}
		taInfof("env=%s 桌台 %s(%s) 进入维护，已开事件单，开始时间 %s [%s]，影响站点 %d 个，操作人 %s",
			env.Name, s.TableNo, s.RoomNo,
			taEventStart(since).Format("2006-01-02 15:04:05"), startLabel, s.SiteCount, s.Operator)

	case !s.Maintaining && wasMaintaining:
		// 收单 + 恢复通知
		var evID string
		var state string
		err := database.DB.QueryRow(`
			SELECT id, state FROM table_alert_events
			WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL
			ORDER BY maintain_start_at DESC LIMIT 1`, env.ID, s.RoomID).Scan(&evID, &state)
		if err != nil {
			return
		}
		database.DB.Exec(`
			UPDATE table_alert_events SET maintain_end_at=?, state='recovered' WHERE id=?`, time.Now(), evID)
		taInfof("env=%s 桌台 %s(%s) 维护结束，事件单已关闭", env.Name, s.TableNo, s.RoomNo)
		// 只有真的告过警才发恢复通知，免得没人知道的维护结束了还去打扰群
		if state == "alerting" || state == "acked" {
			go taSendRecoverNotify(env, evID, s)
		}

	case s.Maintaining && wasMaintaining:
		// 维护中，刷新站点数（部分站点解除时数组会变短，只记录不改变告警状态）
		database.DB.Exec(`
			UPDATE table_alert_events SET site_count=?, operator=?
			WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL`,
			s.SiteCount, s.Operator, env.ID, s.RoomID)
	}
}

// taFinishCollect 回写环境运行态 + 写一条采集日志
func taFinishCollect(env *TAEnv, res *TACollectResult, started time.Time, rawToStore string) {
	if res.DurationMs == 0 {
		res.DurationMs = int(time.Since(started).Milliseconds())
	}
	_, err := database.DB.Exec(`
		UPDATE table_alert_envs
		SET last_collect_at=?, last_collect_ok=?, last_collect_error=?,
		    last_collect_count=?, last_duration_ms=?, last_http_status=?
		WHERE id=?`,
		started, res.OK, res.Error,
		res.RecordCount, res.DurationMs, res.HTTPStatus, env.ID)
	if err != nil {
		taErrorf("env=%s 回写采集状态失败: %v", env.Name, err)
	}

	changesJSON := "[]"
	if len(res.Changes) > 0 {
		if b, err := json.Marshal(res.Changes); err == nil {
			changesJSON = string(b)
		}
	}

	_, err = database.DB.Exec(`
		INSERT INTO table_alert_collect_logs
		  (id, env_id, env_name, started_at, duration_ms, http_status, ok, error_msg, request_url,
		   record_count, total_count, enable_count, disable_count, maintain_count,
		   change_count, changes, raw_response, raw_size, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.New().String(), env.ID, env.Name, started,
		res.DurationMs, res.HTTPStatus, res.OK, res.Error, res.RequestURL,
		res.RecordCount, res.TotalCount, res.EnableCount, res.DisableCount, res.MaintainCnt,
		len(res.Changes), changesJSON, rawToStore, res.RawSize, time.Now())
	if err != nil {
		taErrorf("env=%s 写采集日志失败: %v", env.Name, err)
	}
}

// ---------------------------------------------------------------------------
// 告警引擎
// ---------------------------------------------------------------------------

// taAlertLoop 每 30 秒扫一次维护中的事件，按规则决定要不要发告警
func taAlertLoop() {
	taInfof("告警引擎已启动（每 30s 扫一次）")
	time.Sleep(20 * time.Second)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 没抢到租约的副本不发告警，避免同一条告警被重复 @ 到群里
		if !taAcquireLeader("alerter", 60) {
			continue
		}
		if err := taScanAndAlert(); err != nil {
			taErrorf("告警扫描失败: %v", err)
		}
	}
}

func taScanAndAlert() error {
	rows, err := database.DB.Query(`
		SELECT e.id, e.env_id, e.env_name, e.room_id, e.table_no, e.room_no,
		       e.maintain_start_at, e.site_count, e.operator, e.alert_count,
		       e.next_alert_at, e.state, e.escalated, e.silence_until
		FROM table_alert_events e
		WHERE e.maintain_end_at IS NULL AND e.state IN ('pending','alerting','acked')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pending struct {
		ID, EnvID, EnvName, RoomID, TableNo, RoomNo, Operator, State string
		StartAt                                                      time.Time
		SiteCount, AlertCount                                        int
		NextAlertAt, SilenceUntil                                    sql.NullTime
		Escalated                                                    bool
	}
	list := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.ID, &p.EnvID, &p.EnvName, &p.RoomID, &p.TableNo, &p.RoomNo,
			&p.StartAt, &p.SiteCount, &p.Operator, &p.AlertCount,
			&p.NextAlertAt, &p.State, &p.Escalated, &p.SilenceUntil); err != nil {
			taErrorf("扫描事件失败: %v", err)
			continue
		}
		list = append(list, p)
	}

	now := time.Now()
	for _, p := range list {
		rule, err := taGetRule(p.EnvID)
		if err != nil || !rule.Enabled {
			continue
		}

		// 已确认且还在静默期内 —— 跳过
		if p.State == "acked" {
			if p.SilenceUntil.Valid && now.Before(p.SilenceUntil.Time) {
				continue
			}
			// 静默到期，重新进入告警
			database.DB.Exec(`UPDATE table_alert_events SET state='alerting' WHERE id=?`, p.ID)
			p.State = "alerting"
		}

		// 维护时长还没到阈值
		dur := now.Sub(p.StartAt)
		if dur < time.Duration(rule.ThresholdMin)*time.Minute {
			continue
		}

		// 告警次数用完了
		if p.AlertCount >= rule.MaxTimes && !rule.Escalate {
			if p.State != "stopped" {
				database.DB.Exec(`UPDATE table_alert_events SET state='stopped' WHERE id=?`, p.ID)
				taInfof("env=%s 桌台 %s 告警次数已达上限 %d，停止告警", p.EnvName, p.TableNo, rule.MaxTimes)
			}
			continue
		}

		// 还没到下次告警时间
		if p.NextAlertAt.Valid && now.Before(p.NextAlertAt.Time) {
			continue
		}

		// 免打扰时段
		if rule.QuietEnabled && taInQuietHours(now, rule.QuietStart, rule.QuietEnd) {
			taDebugf("env=%s 桌台 %s 处于免打扰时段（%s-%s），跳过本次告警",
				p.EnvName, p.TableNo, rule.QuietStart, rule.QuietEnd)
			continue
		}

		escalating := p.AlertCount >= rule.MaxTimes && rule.Escalate
		seq := p.AlertCount + 1

		// 下次告警时间
		nextIv := rule.IntervalMin
		if escalating {
			nextIv = rule.EscalateIntervalMin
		}
		next := now.Add(time.Duration(nextIv) * time.Minute)

		// CAS：只有把 alert_count 从旧值推进到新值的那一个实例才真的发送。
		// 租约之外的第二道保险——租约交接的瞬间仍可能有两个实例同时在跑。
		cas, err := database.DB.Exec(`
			UPDATE table_alert_events
			SET alert_count=?, last_alert_at=?, next_alert_at=?, state='alerting', escalated=?, site_count=?
			WHERE id=? AND alert_count=?`, seq, now, next, escalating, p.SiteCount, p.ID, p.AlertCount)
		if err != nil {
			taErrorf("env=%s 桌台 %s 更新告警计数失败: %v", p.EnvName, p.TableNo, err)
			continue
		}
		if n, _ := cas.RowsAffected(); n == 0 {
			taDebugf("env=%s 桌台 %s 第 %d 次告警已被其他实例抢先发送，跳过", p.EnvName, p.TableNo, seq)
			continue
		}

		taInfof("env=%s 桌台 %s(%s) 触发第 %d 次告警（已维护 %s，升级=%v），下次 %s",
			p.EnvName, p.TableNo, p.RoomNo, seq, taHumanDur(dur), escalating, next.Format("15:04:05"))

		go taSendAlert(taAlertPayload{
			EventID:     p.ID,
			EnvName:     p.EnvName,
			TableNo:     p.TableNo,
			RoomNo:      p.RoomNo,
			SiteCount:   p.SiteCount,
			Operator:    p.Operator,
			StartAt:     p.StartAt,
			Duration:    dur,
			Seq:         seq,
			MaxTimes:    rule.MaxTimes,
			Escalating:  escalating,
			NextAt:      next,
			IntervalMin: nextIv,
		}, rule)
	}
	return nil
}

// TARule 告警规则
type TARule struct {
	ID                  string   `json:"id"`
	EnvID               string   `json:"env_id"`
	Enabled             bool     `json:"enabled"`
	ThresholdMin        int      `json:"threshold_min"`
	IntervalMin         int      `json:"interval_min"`
	MaxTimes            int      `json:"max_times"`
	Escalate            bool     `json:"escalate"`
	EscalateIntervalMin int      `json:"escalate_interval_min"`
	NotifyOnRecover     bool     `json:"notify_on_recover"`
	AtLarkIDs           string   `json:"at_lark_ids"`
	EscalateAtLarkIDs   string   `json:"escalate_at_lark_ids"`
	ReatEveryTime       bool     `json:"reat_every_time"`
	SilenceAfterAckMin  int      `json:"silence_after_ack_min"`
	QuietEnabled        bool     `json:"quiet_enabled"`
	QuietStart          string   `json:"quiet_start"`
	QuietEnd            string   `json:"quiet_end"`
	BotIDs              []string `json:"bot_ids"`
}

func taGetRule(envID string) (*TARule, error) {
	r := &TARule{}
	err := database.DB.QueryRow(`
		SELECT id, env_id, enabled, threshold_min, interval_min, max_times, escalate,
		       escalate_interval_min, notify_on_recover, at_lark_ids, escalate_at_lark_ids,
		       reat_every_time, silence_after_ack_min, quiet_enabled, quiet_start, quiet_end
		FROM table_alert_rules WHERE env_id=?`, envID).Scan(
		&r.ID, &r.EnvID, &r.Enabled, &r.ThresholdMin, &r.IntervalMin, &r.MaxTimes, &r.Escalate,
		&r.EscalateIntervalMin, &r.NotifyOnRecover, &r.AtLarkIDs, &r.EscalateAtLarkIDs,
		&r.ReatEveryTime, &r.SilenceAfterAckMin, &r.QuietEnabled, &r.QuietStart, &r.QuietEnd)
	if err != nil {
		return nil, err
	}
	r.BotIDs = taGetRuleBots(r.ID)
	return r, nil
}

func taGetRuleBots(ruleID string) []string {
	out := []string{}
	rows, err := database.DB.Query(`SELECT bot_id FROM table_alert_rule_bots WHERE rule_id=?`, ruleID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// taAlertPayload 一次告警所需的全部信息
type taAlertPayload struct {
	EventID     string
	EnvName     string
	TableNo     string
	RoomNo      string
	SiteCount   int
	Operator    string
	StartAt     time.Time
	Duration    time.Duration
	Seq         int
	MaxTimes    int
	Escalating  bool
	NextAt      time.Time
	IntervalMin int
}

// taSendAlert 发 Lark 告警，支持多个群
func taSendAlert(p taAlertPayload, rule *TARule) {
	atIDs := taSplitIDs(rule.AtLarkIDs)
	if p.Escalating {
		atIDs = append(atIDs, taSplitIDs(rule.EscalateAtLarkIDs)...)
	}
	atIDs = taUniq(atIDs)

	title := fmt.Sprintf("🔧 【%s】桌台维护告警 · 第 %d 次", p.EnvName, p.Seq)
	color := "orange"
	if p.Escalating {
		title = fmt.Sprintf("🚨 【%s】桌台维护告警升级 · 第 %d 次", p.EnvName, p.Seq)
		color = "red"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**桌台**：%s（房间号 %s）\n", p.TableNo, p.RoomNo)
	fmt.Fprintf(&b, "**状态**：维护中，已持续 **%s**\n", taHumanDur(p.Duration))
	fmt.Fprintf(&b, "**开始时间**：%s\n", p.StartAt.Format("2006-01-02 15:04:05"))
	if p.SiteCount > 0 {
		fmt.Fprintf(&b, "**影响站点**：%d 个\n", p.SiteCount)
	}
	if p.Operator != "" {
		fmt.Fprintf(&b, "**最后操作人**：%s\n", p.Operator)
	}
	fmt.Fprintf(&b, "**下次告警**：%s（每 %d 分钟）\n", p.NextAt.Format("15:04"), p.IntervalMin)
	b.WriteString("\n请确认该桌台是否需要恢复。")

	kind := "alert"
	if p.Escalating {
		kind = "escalate"
	}
	taBroadcast(rule, kind, p.EventID, p.EnvName, p.TableNo, p.Seq, title, b.String(), color, atIDs)
}

// taSendRecoverNotify 恢复通知
func taSendRecoverNotify(env *TAEnv, eventID string, s taRoomSnapshot) {
	rule, err := taGetRule(env.ID)
	if err != nil || !rule.Enabled || !rule.NotifyOnRecover {
		return
	}
	title := fmt.Sprintf("✅ 【%s】桌台维护已恢复", env.Name)
	body := fmt.Sprintf("**桌台**：%s（房间号 %s）\n**状态**：已退出维护，恢复正常\n**恢复时间**：%s",
		s.TableNo, s.RoomNo, time.Now().Format("2006-01-02 15:04:05"))
	taBroadcast(rule, "recover", eventID, env.Name, s.TableNo, 0, title, body, "green", nil)
}

// taBroadcast 把同一条通知发到规则绑定的所有群，逐个记录发送结果
func taBroadcast(rule *TARule, kind, eventID, envName, tableNo string, seq int,
	title, body, color string, atIDs []string) {

	if len(rule.BotIDs) == 0 {
		taErrorf("env=%s 桌台 %s 告警未发送：规则没有绑定任何 Lark 机器人", envName, tableNo)
		return
	}

	linkURL := taPortalBaseURL()
	if linkURL != "" {
		linkURL += "/table-alert"
	}

	for _, botID := range rule.BotIDs {
		var name, webhook, secret string
		var enabled bool
		err := database.DB.QueryRow(`
			SELECT name, webhook, secret, enabled FROM table_alert_lark_bots WHERE id=?`,
			botID).Scan(&name, &webhook, &secret, &enabled)
		if err != nil {
			taErrorf("读取机器人 %s 失败: %v", botID, err)
			continue
		}
		if !enabled || webhook == "" {
			taDebugf("机器人 %s 未启用或未配置 webhook，跳过", name)
			continue
		}

		taDebugf("env=%s 向群「%s」发送 %s 通知（桌台 %s，第 %d 次，艾特 %d 人）",
			envName, name, kind, tableNo, seq, len(atIDs))

		err = services.SendLarkCardWithRetry(context.Background(),
			webhook, secret, title, body, color, "查看详情", linkURL, atIDs...)

		ok := err == nil
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
			taErrorf("env=%s 群「%s」发送失败: %v", envName, name, err)
		} else {
			taInfof("env=%s 群「%s」发送成功（桌台 %s，第 %d 次）", envName, name, tableNo, seq)
		}

		database.DB.Exec(`
			INSERT INTO table_alert_notify_logs
			  (id, event_id, env_name, table_no, bot_id, bot_name, kind, seq,
			   at_lark_ids, title, body, ok, error_msg, sent_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			uuid.New().String(), eventID, envName, tableNo, botID, name, kind, seq,
			strings.Join(atIDs, ","), title, body, ok, errMsg, time.Now())
	}
}

// taPortalBaseURL 通知卡片里「查看详情」按钮的跳转地址。
// 不写死域名：从系统参数 table_alert_portal_url 读，没配就不显示按钮。
func taPortalBaseURL() string {
	var v string
	err := database.DB.QueryRow(
		`SELECT setting_value FROM system_settings WHERE setting_key = 'table_alert_portal_url' LIMIT 1`).Scan(&v)
	if err != nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(v), "/")
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func taListEnvs(onlyEnabled bool) ([]TAEnv, error) {
	q := `
		SELECT id, name, enabled, sort_order, COALESCE(url,''), method, host_header,
		       COALESCE(request_body,''), COALESCE(extra_headers,''), COALESCE(token,''), token_place,
		       skip_tls_verify, timeout_sec, cur_page, page_size,
		       data_path, total_path, f_room_id, f_table_no, f_room_no, f_platform_id,
		       f_status, f_maintain, f_operator, f_update_time, f_online_total,
		       maintain_rule, maintain_status_value, interval_sec, log_raw_response,
		       DATE_FORMAT(last_collect_at, '%Y-%m-%d %H:%i:%s'), last_collect_ok,
		       COALESCE(last_collect_error,''), last_collect_count, last_duration_ms, last_http_status
		FROM table_alert_envs`
	if onlyEnabled {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY sort_order, name`

	rows, err := database.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TAEnv{}
	for rows.Next() {
		var e TAEnv
		var lastAt sql.NullString
		if err := rows.Scan(&e.ID, &e.Name, &e.Enabled, &e.Sort, &e.URL, &e.Method, &e.HostHeader,
			&e.RequestBody, &e.ExtraHeaders, &e.Token, &e.TokenPlace,
			&e.SkipTLSVerify, &e.TimeoutSec, &e.CurPage, &e.PageSize,
			&e.DataPath, &e.TotalPath, &e.FRoomID, &e.FTableNo, &e.FRoomNo, &e.FPlatformID,
			&e.FStatus, &e.FMaintain, &e.FOperator, &e.FUpdateTime, &e.FOnlineTotal,
			&e.MaintainRule, &e.MaintainStatusValue, &e.IntervalSec, &e.LogRawResponse,
			&lastAt, &e.LastCollectOK, &e.LastCollectError, &e.LastCollectCount,
			&e.LastDurationMs, &e.LastHTTPStatus); err != nil {
			taErrorf("读取环境行失败: %v", err)
			continue
		}
		if lastAt.Valid {
			s := lastAt.String
			e.LastCollectAt = &s
		}
		out = append(out, e)
	}
	return out, nil
}

func taGetEnv(id string) (*TAEnv, error) {
	envs, err := taListEnvs(false)
	if err != nil {
		return nil, err
	}
	for i := range envs {
		if envs[i].ID == id {
			return &envs[i], nil
		}
	}
	return nil, fmt.Errorf("环境不存在: %s", id)
}

// taDigPath 按点分路径取值，如 "data.records"
func taDigPath(root map[string]interface{}, path string) interface{} {
	if path == "" {
		return root
	}
	var cur interface{} = root
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return cur
}

func taToStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func taToInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		return n
	}
	return 0
}

func taDefaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func taDefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func taTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(截断)"
}

// taInsecureTransport 跳过证书校验的 transport。
// 内网网关证书过期时用（如 *.inner.* 那张），由使用者在环境配置里显式打开。
func taInsecureTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 由使用者显式开启
	}
}

func taMaintainLabel(b bool) string {
	if b {
		return "维护中"
	}
	return "正常"
}

func taHumanDur(d time.Duration) string {
	total := int(d.Minutes())
	if total < 60 {
		return fmt.Sprintf("%d分钟", total)
	}
	h := total / 60
	m := total % 60
	if h < 24 {
		return fmt.Sprintf("%d小时%d分钟", h, m)
	}
	return fmt.Sprintf("%d天%d小时%d分钟", h/24, h%24, m)
}

func taSplitIDs(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func taUniq(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// taInQuietHours 判断当前是否在免打扰时段，支持跨零点（如 23:00-07:00）
func taInQuietHours(now time.Time, start, end string) bool {
	toMin := func(s string) int {
		var h, m int
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil {
			return -1
		}
		return h*60 + m
	}
	s, e := toMin(start), toMin(end)
	if s < 0 || e < 0 {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	if s <= e {
		return cur >= s && cur < e
	}
	return cur >= s || cur < e
}

// ---------------------------------------------------------------------------
// 多副本选主
//
// 后端是多副本部署，每个副本都跑着采集器和告警引擎。没有协调的话：
//   - 中台会被重复请求（副本数倍的压力）
//   - 同一条告警被重复 @ 到群里（最难受的那种）
// 这里用数据库里的一行租约做选主：谁更新成功谁在 ttl 内负责干活，
// 持有者挂了租约自然过期，其他副本接管，不需要额外组件。
// ---------------------------------------------------------------------------

var taInstanceIDOnce sync.Once
var taInstanceIDVal string

// taInstanceID 实例标识：主机名 + 进程号，k8s 下就是 pod 名
func taInstanceID() string {
	taInstanceIDOnce.Do(func() {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown"
		}
		taInstanceIDVal = fmt.Sprintf("%s-%d", host, os.Getpid())
	})
	return taInstanceIDVal
}

// taAcquireLeader 抢/续租约，成功返回 true。
// 条件 (expires_at < NOW() OR holder = 自己) 保证：租约没过期时别人抢不走，
// 自己则可以无限续租，不会出现频繁易主导致的采集抖动。
func taAcquireLeader(role string, ttlSec int) bool {
	me := taInstanceID()
	_, err := database.DB.Exec(`
		UPDATE table_alert_leader
		SET holder = ?, expires_at = DATE_ADD(NOW(), INTERVAL ? SECOND)
		WHERE role = ? AND (expires_at < NOW() OR holder = ?)`,
		me, ttlSec, role, me)
	if err != nil {
		taErrorf("抢占 %s 租约失败: %v", role, err)
		return false
	}
	// 不能只看 RowsAffected：同一秒内重复执行时值没变化，MySQL 会返回 0 行，
	// 会把「我本来就是 leader」误判成「没抢到」。回查一次 holder 才可靠。
	var holder string
	if err := database.DB.QueryRow(`
		SELECT holder FROM table_alert_leader WHERE role = ? AND expires_at > NOW()`,
		role).Scan(&holder); err != nil {
		return false
	}
	return holder == me
}

// taEventStart 把 maintain_since 归一成 time.Time，取不到就用当前时间兜底
func taEventStart(since interface{}) time.Time {
	if t, ok := since.(time.Time); ok && !t.IsZero() {
		return t
	}
	return time.Now()
}

// taParseRemoteTime 解析接口返回的时间字符串。
// 中台给的是 "2026-09-24T04:53:29.000+00:00" 这种带时区的格式，
// 解析后统一转到本地时区，避免和 time.Now() 比较时又踩一次时区的坑。
func taParseRemoteTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
	} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.In(time.Local)
		}
	}
	return time.Time{}
}
