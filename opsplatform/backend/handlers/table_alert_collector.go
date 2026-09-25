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
	SiteIDs     []string // 受影响的 siteId，顺序保持接口返回的样子
	OnlineTotal int
	Operator    string
	UpdateTime  string
}

// taUnavailable 在用桌台是否处于不可用状态，以及原因。
//
// 「维护中」和「被停用」对业务的后果是一样的：这张桌台现在不能用。
// 分两套告警只会让人配两遍规则、收两种措辞不一的消息，所以统一成一个概念，
// 用 reason 区分成因即可。
func taUnavailable(status string, maintaining bool) (bool, string) {
	disabled := status != "" && !strings.EqualFold(status, "Enable")
	switch {
	case disabled && maintaining:
		return true, "both"
	case disabled:
		return true, "disabled"
	case maintaining:
		return true, "maintain"
	}
	return false, ""
}

// taReasonLabel 不可用原因的中文措辞，日志与卡片共用
func taReasonLabel(reason string) string {
	switch reason {
	case "disabled":
		return "已被停用"
	case "both":
		return "已停用且维护中"
	default:
		return "维护中"
	}
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

// taSchedulerTick 是调度器扫描环境表的节奏，同时决定了采集间隔的精度。
//
// 这个值直接决定「填进去的间隔」和「实际跑出来的间隔」差多少：扫描是离散的，
// 只能在 tick 上开采，所以配 45s、tick 10s 时会被顶到 50s —— 页面上写 45、
// 日志里跑 50，对不上。取 5s 再配合下面的半拍容差，常用值（5 的倍数）都能落准。
const taSchedulerTick = 5 * time.Second

// StartTableAlertScheduler 启动采集调度器。
// 每 taSchedulerTick 扫一次环境表，到点的环境就采集一次 —— 这样页面上改了
// interval_sec 立刻生效，不需要重启服务。
func StartTableAlertScheduler() {
	taSchedulerOnce.Do(func() {
		go taSchedulerLoop()
		go taAlertLoop()
	})
}

func taSchedulerLoop() {
	taInfof("采集调度器已启动（每 %s 检查一次到期环境）", taSchedulerTick)
	// 启动后等一会儿再跑，避开服务刚起来时的初始化
	time.Sleep(15 * time.Second)

	ticker := time.NewTicker(taSchedulerTick)
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
	// 留半拍容差：只能在 tick 上开采，用「够了才采」会让所有不是 tick 整数倍的间隔
	// 一律往后顶一整拍（配 45s 实际跑 50s）。提前半拍判定，误差就从「最多晚一拍」
	// 变成「前后半拍」，填什么值都不至于系统性偏慢。
	due := time.Duration(iv)*time.Second - taSchedulerTick/2
	return time.Since(last) >= due
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

	// 站点自动发现：先于快照落库，保证事件单算关注数时字典已是最新
	taDiscoverSites(env.ID, snaps)

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
			// 没有主键时的兜底顺序：房间号优先于桌台号。
			// 一个桌台可能有多个房间（N13 下有 N013 和 N013-2），
			// 用桌台号兜底会把它们撞成同一条记录，房间号才是真正唯一的那个。
			if s.RoomNo != "" {
				s.RoomID = s.RoomNo
			} else {
				s.RoomID = s.TableNo
			}
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
				// 元素形如 {"siteId":"...","source":"Central"}，把 siteId 抽出来。
				// 兼容元素直接就是字符串的情况。
				for _, it := range lst {
					switch e := it.(type) {
					case map[string]interface{}:
						if v := taToStr(e["siteId"]); v != "" {
							s.SiteIDs = append(s.SiteIDs, v)
						}
					case string:
						if e != "" {
							s.SiteIDs = append(s.SiteIDs, e)
						}
					}
				}
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
			oldInService   bool
			inServiceSet   bool
			exists         bool
		)
		err := database.DB.QueryRow(`
			SELECT status, maintaining, operator, maintain_since, since_estimated,
			       in_service, in_service_manual
			FROM table_alert_rooms WHERE env_id = ? AND room_id = ?`,
			env.ID, s.RoomID).Scan(&oldStatus, &oldMaintaining, &oldOperator, &maintainSince,
			&oldEstimated, &oldInService, &inServiceSet)
		switch {
		case err == sql.ErrNoRows:
			exists = false
		case err != nil:
			taErrorf("env=%s 读取桌台 %s 旧状态失败: %v", env.Name, s.TableNo, err)
			continue
		default:
			exists = true
		}

		// in_service：系统分不清一张停用的桌台是「刚被误停」还是「压根没上线」，
		// 这个信息只有人知道。自动推断遵循**只升不降**：
		//
		//   首次见到          → 按 status 给初值（启用→在用），省得几十台一个个标
		//   之后变成启用      → 自动标为在用（它上线了）
		//   之后变成停用      → **保持原值不动**
		//   人工设过          → 永远以人工为准
		//
		// 最关键的是第三条。早先写成「没人工设过就每轮按 status 重算」，
		// 结果桌台一被停用就自动降级成非在用，于是不再告警 —— 恰恰把
		// 「在用桌台被误停」这个最该发现的场景给静默了。自动逻辑可以把桌台
		// 标成在用，但降级只能由人来判断。
		newInService := oldInService
		switch {
		case !exists:
			newInService = strings.EqualFold(s.Status, "Enable")
		case !inServiceSet && strings.EqualFold(s.Status, "Enable"):
			newInService = true
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
		// 不可用 = 维护中 或 被停用（仅对在用桌台有意义）
		unavailNow, reasonNow := taUnavailable(s.Status, s.Maintaining)
		unavailBefore := false
		if exists {
			unavailBefore, _ = taUnavailable(oldStatus, oldMaintaining)
		}

		var newSince interface{}
		newEstimated := false
		switch {
		case unavailNow && !exists:
			// 首次见到就是维护中 —— 回溯
			if t := taParseRemoteTime(s.UpdateTime); !t.IsZero() && t.Before(now) {
				newSince = t
				newEstimated = true
				taDebugf("env=%s 桌台 %s 首次采集即处于维护，按接口 updateTime 回溯到 %s（估算）",
					env.Name, s.TableNo, t.Format("2006-01-02 15:04:05"))
			} else {
				newSince = now
			}
		case unavailNow && !unavailBefore:
			// 观测到跃迁 —— 精确，且覆盖掉之前可能的估算标记
			newSince = now
		case unavailNow && maintainSince.Valid:
			newSince = maintainSince.Time
			newEstimated = oldEstimated
			// 存量数据一次性纠正：升级前这里一律用采集时刻，导致冷启动时已在维护的
			// 桌台开始时间被记成「系统发现它的那一刻」。光改新逻辑救不了这些记录 ——
			// 它们已经有 maintain_since，会一直走这个分支把错值留着。
			//
			// 判据用差值而不是"是否相等"：实测跃迁记下的 since 天然就比 updateTime 晚
			// 一点（最多晚一个采集周期），而冷启动记的能晚上好几个小时。所以只有差距
			// 明显超出正常采集延迟时才认定是存量错值。
			// 纠正后打上 since_estimated，下次就不会再进来，只会发生一次。
			if !oldEstimated {
				if t := taParseRemoteTime(s.UpdateTime); !t.IsZero() {
					tolerance := time.Duration(taDefaultInt(env.IntervalSec, 60)) * time.Second * 3
					if tolerance < 5*time.Minute {
						tolerance = 5 * time.Minute
					}
					if maintainSince.Time.Sub(t) > tolerance {
						newSince = t
						newEstimated = true
						taInfof("env=%s 桌台 %s 维护开始时间回填：%s → %s（原值是升级前按采集时刻记的，现按接口 updateTime 回溯，只纠正这一次）",
							env.Name, s.TableNo,
							maintainSince.Time.Format("2006-01-02 15:04:05"),
							t.Format("2006-01-02 15:04:05"))
					}
				}
			}
		case unavailNow:
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
				    maintain_site_ids=?, online_user_total=?, operator=?, remote_update_time=?,
				    maintain_since=?, since_estimated=?, in_service=?, last_seen_at=?
				WHERE env_id=? AND room_id=?`,
				s.TableNo, s.RoomNo, s.PlatformID, s.Status, s.Maintaining, s.SiteCount,
				strings.Join(s.SiteIDs, ","), s.OnlineTotal, s.Operator, s.UpdateTime,
				newSince, newEstimated, newInService, now, env.ID, s.RoomID)
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
				   maintain_site_count, maintain_site_ids, online_user_total, operator,
				   remote_update_time, maintain_since, since_estimated, in_service,
				   first_seen_at, last_seen_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				uuid.New().String(), env.ID, s.RoomID, s.TableNo, s.RoomNo, s.PlatformID,
				s.Status, s.Maintaining, s.SiteCount, strings.Join(s.SiteIDs, ","),
				s.OnlineTotal, s.Operator, s.UpdateTime, newSince, newEstimated, newInService,
				now, now)
		}
		if err != nil {
			taErrorf("env=%s 写入桌台 %s 失败: %v", env.Name, s.TableNo, err)
			continue
		}

		// 维护事件：开始 / 结束
		taSyncEvent(env, s, unavailNow, reasonNow, newInService, newSince, newEstimated)
	}

	return changes, nil
}

// taSyncEvent 按当前状态对账事件单：该开的开、该收的收。
func taSyncEvent(env *TAEnv, s taRoomSnapshot, unavailNow bool, reason string,
	inService bool, since interface{}, estimated bool) {
	// 非在用的桌台不开单：它没在对外服务，维护也好停用也罢都不构成问题。
	// 人工把它标成在用之后，下一轮就会正常开单。
	if !inService {
		return
	}

	// 开单 / 收单一律按「当前是否不可用 + 有没有未结单」对账，**不依赖本轮是否观测到跃迁**。
	//
	// 早先开单条件写的是 unavailNow && !wasUnavail，也就是必须本轮亲眼看到
	// 「可用 → 不可用」这一下。问题出在人工标记这条路上：一张早就停用或维护中的桌台
	// 被标成在用时，跃迁已经是过去的事了，于是永远等不到开单 —— 页面上标记明明生效了，
	// 一条告警都不会来，得等上游状态再抖一次才补上。
	// 重复开单由「有没有未结单」拦住，跃迁条件本来就是多余的。
	var evID, evState string
	err := database.DB.QueryRow(`
		SELECT id, state FROM table_alert_events
		WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL
		ORDER BY maintain_start_at DESC LIMIT 1`, env.ID, s.RoomID).Scan(&evID, &evState)
	hasOpen := err == nil
	if err != nil && err != sql.ErrNoRows {
		taErrorf("env=%s 桌台 %s 查未结事件单失败: %v", env.Name, s.TableNo, err)
		return
	}

	switch {
	case unavailNow && !hasOpen:
		start := taEventStart(since)
		// 归属判定放在开单时做一次：之后即使维护拖到窗口之外，也还知道它本来属于哪次例行保养
		var winID, winName string
		var winEnd interface{}
		if hit := taMatchWindow(env.ID, s.RoomNo, s.TableNo, start, time.Now()); hit != nil {
			winID, winName, winEnd = hit.Window.ID, hit.Window.Name, hit.PlanEnd
		}
		watchedNames := taPickWatched(s.SiteIDs, taWatchedSites(env.ID))
		_, err := database.DB.Exec(`
			INSERT INTO table_alert_events
			  (id, env_id, env_name, room_id, table_no, room_no, platform_id,
			   maintain_start_at, start_estimated, site_count, site_ids, watched_site_count,
			   operator, state, reason, window_id, window_name, window_end_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?, 'pending',?,?,?,?,?,?)`,
			uuid.New().String(), env.ID, env.Name, s.RoomID, s.TableNo, s.RoomNo,
			s.PlatformID, start, estimated, s.SiteCount, strings.Join(s.SiteIDs, ","),
			len(watchedNames), s.Operator, reason,
			winID, winName, winEnd, time.Now(), time.Now())
		if err != nil {
			taErrorf("env=%s 桌台 %s 开事件单失败: %v", env.Name, s.TableNo, err)
			return
		}
		startLabel := "实测跃迁"
		if estimated {
			startLabel = "按 updateTime 回溯（估算）"
		}
		planLabel := "计划外"
		if winName != "" {
			planLabel = "例行维护「" + winName + "」"
		}
		taInfof("env=%s 桌台 %s(%s) 不可用（%s），已开事件单，开始时间 %s [%s]，%s，影响站点 %d 个，操作人 %s",
			env.Name, s.TableNo, s.RoomNo, taReasonLabel(reason),
			start.Format("2006-01-02 15:04:05"), startLabel, planLabel, s.SiteCount, s.Operator)

	case !unavailNow && hasOpen:
		// 收单 + 恢复通知
		database.DB.Exec(`
			UPDATE table_alert_events SET maintain_end_at=?, state='recovered' WHERE id=?`, time.Now(), evID)
		taInfof("env=%s 桌台 %s(%s) 已恢复可用，事件单已关闭", env.Name, s.TableNo, s.RoomNo)
		// 只有真的告过警才发恢复通知，免得没人知道的维护结束了还去打扰群
		if evState == "alerting" || evState == "acked" {
			go taSendRecoverNotify(env, evID, s)
		}

	case unavailNow && hasOpen:
		// 仍不可用，刷新站点数与原因（维护中被停用时 reason 会从 maintain 变成 both）
		database.DB.Exec(`
			UPDATE table_alert_events SET reason=?
			WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL`,
			reason, env.ID, s.RoomID)
		// 部分站点解除维护时数组会变短，关注站点数要跟着重算 ——
		// 关注的那几个都恢复了，就不该再继续吵人
		watchedNow := taPickWatched(s.SiteIDs, taWatchedSites(env.ID))
		database.DB.Exec(`
			UPDATE table_alert_events SET site_count=?, site_ids=?, watched_site_count=?, operator=?
			WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL`,
			s.SiteCount, strings.Join(s.SiteIDs, ","), len(watchedNow), s.Operator, env.ID, s.RoomID)

		// 快照侧回填了开始时间的话，事件单也要跟着改 —— 告警时长判定读的是
		// events.maintain_start_at，只改 rooms 的话页面对了、告警阈值还是错的。
		if estimated {
			start := taEventStart(since)
			res, err := database.DB.Exec(`
				UPDATE table_alert_events
				SET maintain_start_at=?, start_estimated=1
				WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL
				  AND start_estimated=0 AND maintain_start_at > ?`,
				start, env.ID, s.RoomID, start)
			if err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					taInfof("env=%s 桌台 %s 事件单开始时间同步回填为 %s",
						env.Name, s.TableNo, start.Format("2006-01-02 15:04:05"))
				}
			}
		}
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
		       e.next_alert_at, e.state, e.escalated, e.silence_until,
		       e.window_id, e.window_name, e.window_end_at, e.overrun_notified,
		       COALESCE(e.site_ids,''), e.watched_site_count,
		       COALESCE(r.status,''), COALESCE(r.in_service,0), e.reason
		FROM table_alert_events e
		LEFT JOIN table_alert_rooms r ON r.env_id = e.env_id AND r.room_id = e.room_id
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
		WindowID, WindowName                                         string
		WindowEndAt                                                  sql.NullTime
		OverrunNotified                                              bool
		SiteIDs                                                      string
		WatchedSiteCount                                             int
		RoomStatus                                                   string
		InService                                                    bool
		Reason                                                       string
	}
	list := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.ID, &p.EnvID, &p.EnvName, &p.RoomID, &p.TableNo, &p.RoomNo,
			&p.StartAt, &p.SiteCount, &p.Operator, &p.AlertCount,
			&p.NextAlertAt, &p.State, &p.Escalated, &p.SilenceUntil,
			&p.WindowID, &p.WindowName, &p.WindowEndAt, &p.OverrunNotified,
			&p.SiteIDs, &p.WatchedSiteCount, &p.RoomStatus, &p.InService, &p.Reason); err != nil {
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

		// ===== 是否在用 =====
		// 「在用」是人工标记的：系统分不清一张停用的桌台是刚被误停还是压根没上线。
		// 标了在用，不论维护还是停用都算不可用、都要告警；没标的怎么折腾都不打扰。
		// 事件本来就只对在用桌台开单，这里再挡一道，是为了覆盖「开单后被人改成非在用」的情况。
		if rule.AlertTableScope != "all" && !p.InService {
			taDebugf("env=%s 桌台 %s(%s) 未标记为在用，跳过告警",
				p.EnvName, p.TableNo, p.RoomNo)
			continue
		}

		// ===== 告警范围：只告警关注站点 =====
		// 一张桌台可能对几十个站点维护，但只有少数几个是真正关心的。
		// 选了 watched 之后，没碰到关注站点的维护在页面上照样看得到，
		// 只是不发 Lark、不计入告警中 —— 信息不丢，只是不吵人。
		watchedNames := []string{}
		if rule.AlertScope == "watched" {
			watchedNames = taPickWatched(strings.Split(p.SiteIDs, ","), taWatchedSites(p.EnvID))
			if len(watchedNames) == 0 {
				taDebugf("env=%s 桌台 %s 维护未涉及关注站点，按「仅关注站点」策略跳过告警",
					p.EnvName, p.TableNo)
				continue
			}
		} else if rule.ListWatchedSites {
			watchedNames = taPickWatched(strings.Split(p.SiteIDs, ","), taWatchedSites(p.EnvID))
		}

		// ===== 例行维护窗口 =====
		// 计划内的保养不该和故障用一样的措辞，否则告警会被当成噪音；
		// 但「例行维护拖过了窗口还没恢复」恰恰是最该有人去看的情况。
		inWindow, overrun := false, time.Duration(0)
		var planEnd time.Time
		if p.WindowID != "" && p.WindowEndAt.Valid {
			planEnd = p.WindowEndAt.Time
			if now.Before(planEnd) {
				inWindow = true
			} else {
				overrun = now.Sub(planEnd)
			}
		}

		if inWindow {
			win := taFindWindow(p.EnvID, p.WindowID)
			if win != nil && win.Action == "suppress" {
				taDebugf("env=%s 桌台 %s 处于例行维护「%s」窗口内（计划 %s 结束），按配置静默",
					p.EnvName, p.TableNo, p.WindowName, planEnd.Format("15:04"))
				continue
			}
		}

		// 超窗后第一次扫到：无论之前告警到第几次，都立刻补一条「已超时」，
		// 不必等下一个告警间隔 —— 这是状态性质的变化，值得马上说一声。
		forceOverrun := false
		if overrun > 0 && !p.OverrunNotified {
			forceOverrun = true
			database.DB.Exec(`UPDATE table_alert_events SET overrun_notified=1 WHERE id=?`, p.ID)
		}

		// 告警次数用完了
		if p.AlertCount >= rule.MaxTimes && !rule.Escalate {
			if p.State != "stopped" {
				database.DB.Exec(`UPDATE table_alert_events SET state='stopped' WHERE id=?`, p.ID)
				taInfof("env=%s 桌台 %s 告警次数已达上限 %d，停止告警", p.EnvName, p.TableNo, rule.MaxTimes)
			}
			continue
		}

		// 还没到下次告警时间（超窗首次提醒例外）
		if !forceOverrun && p.NextAlertAt.Valid && now.Before(p.NextAlertAt.Time) {
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
			Reason:       p.Reason,
			WatchedSites: watchedNames,
			TotalSites:   p.SiteCount,
			ListSites:    rule.ListWatchedSites,
			MaxListSites: rule.MaxListSites,
			WindowName:   p.WindowName,
			InWindow:     inWindow,
			Overrun:      overrun,
			PlanEnd:      planEnd,
			EventID:      p.ID,
			EnvName:      p.EnvName,
			TableNo:      p.TableNo,
			RoomNo:       p.RoomNo,
			SiteCount:    p.SiteCount,
			Operator:     p.Operator,
			StartAt:      p.StartAt,
			Duration:     dur,
			Seq:          seq,
			MaxTimes:     rule.MaxTimes,
			Escalating:   escalating,
			NextAt:       next,
			IntervalMin:  nextIv,
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
	AlertScope          string   `json:"alert_scope"`
	AlertTableScope     string   `json:"alert_table_scope"`
	AlertOnDisable      bool     `json:"alert_on_disable"`
	ListWatchedSites    bool     `json:"list_watched_sites"`
	MaxListSites        int      `json:"max_list_sites"`
	BotIDs              []string `json:"bot_ids"`
}

func taGetRule(envID string) (*TARule, error) {
	r := &TARule{}
	err := database.DB.QueryRow(`
		SELECT id, env_id, enabled, threshold_min, interval_min, max_times, escalate,
		       escalate_interval_min, notify_on_recover, at_lark_ids, escalate_at_lark_ids,
		       reat_every_time, silence_after_ack_min, quiet_enabled, quiet_start, quiet_end,
		       alert_scope, list_watched_sites, max_list_sites,
		       alert_table_scope, alert_on_disable
		FROM table_alert_rules WHERE env_id=?`, envID).Scan(
		&r.ID, &r.EnvID, &r.Enabled, &r.ThresholdMin, &r.IntervalMin, &r.MaxTimes, &r.Escalate,
		&r.EscalateIntervalMin, &r.NotifyOnRecover, &r.AtLarkIDs, &r.EscalateAtLarkIDs,
		&r.ReatEveryTime, &r.SilenceAfterAckMin, &r.QuietEnabled, &r.QuietStart, &r.QuietEnd,
		&r.AlertScope, &r.ListWatchedSites, &r.MaxListSites,
		&r.AlertTableScope, &r.AlertOnDisable)
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
	Reason       string   // maintain / disabled / both
	WatchedSites []string // 受影响的关注站点名
	TotalSites   int      // 受影响站点总数
	ListSites    bool     // 是否在卡片里列出站点名
	MaxListSites int
	WindowName   string        // 命中的例行维护窗口名，空=计划外维护
	InWindow     bool          // 当前仍在例行窗口内
	Overrun      time.Duration // 已超出窗口多久
	PlanEnd      time.Time     // 例行窗口的计划结束时间
	EventID      string
	EnvName      string
	TableNo      string
	RoomNo       string
	SiteCount    int
	Operator     string
	StartAt      time.Time
	Duration     time.Duration
	Seq          int
	MaxTimes     int
	Escalating   bool
	NextAt       time.Time
	IntervalMin  int
}

// taSendAlert 发 Lark 告警，支持多个群
func taSendAlert(p taAlertPayload, rule *TARule) {
	atIDs := taSplitIDs(rule.AtLarkIDs)
	if p.Escalating {
		atIDs = append(atIDs, taSplitIDs(rule.EscalateAtLarkIDs)...)
	}
	atIDs = taUniq(atIDs)

	// 三种口径，措辞要让人一眼分清该不该紧张：
	//   例行维护窗口内   → 蓝色，只是知会
	//   例行维护已超时   → 红色，计划内的事拖过了点，最该有人看
	//   计划外维护       → 橙色/红色，常规告警
	var title, color, planLine string
	switch {
	case p.WindowName != "" && p.InWindow:
		title = fmt.Sprintf("🗓 【%s】例行维护中 · 第 %d 次", p.EnvName, p.Seq)
		color = "blue"
		planLine = fmt.Sprintf("**例行维护**：%s（计划 %s 结束）\n", p.WindowName, p.PlanEnd.Format("01-02 15:04"))
	case p.WindowName != "" && p.Overrun > 0:
		title = fmt.Sprintf("⏰ 【%s】例行维护已超时 · 第 %d 次", p.EnvName, p.Seq)
		color = "red"
		planLine = fmt.Sprintf("**例行维护**：%s\n**计划结束**：%s，**已超时 %s**\n",
			p.WindowName, p.PlanEnd.Format("01-02 15:04"), taHumanDur(p.Overrun))
	case p.Escalating:
		title = fmt.Sprintf("🚨 【%s】桌台不可用告警升级 · 第 %d 次", p.EnvName, p.Seq)
		color = "red"
		planLine = "**类型**：计划外\n"
	case p.Reason == "disabled":
		// 在用的桌台被停用，往往是误操作（想点维护点成了停用），比维护更值得立刻看
		title = fmt.Sprintf("⛔ 【%s】在用桌台已被停用 · 第 %d 次", p.EnvName, p.Seq)
		color = "red"
		planLine = "**类型**：计划外 —— 该桌台标记为在用，却处于停用状态\n"
	case p.Reason == "both":
		title = fmt.Sprintf("⛔ 【%s】在用桌台已停用且维护中 · 第 %d 次", p.EnvName, p.Seq)
		color = "red"
		planLine = "**类型**：计划外 —— 停用与维护同时存在\n"
	default:
		title = fmt.Sprintf("🔧 【%s】桌台维护告警 · 第 %d 次", p.EnvName, p.Seq)
		color = "orange"
		planLine = "**类型**：计划外维护\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**桌台**：%s（房间号 %s）\n", p.TableNo, p.RoomNo)
	fmt.Fprintf(&b, "**状态**：%s，已持续 **%s**\n", taReasonLabel(p.Reason), taHumanDur(p.Duration))
	b.WriteString(planLine)
	fmt.Fprintf(&b, "**开始时间**：%s\n", p.StartAt.Format("2006-01-02 15:04:05"))
	// 站点：优先说清楚「哪些关注的站点受影响」，总数放在后面作参考
	if p.ListSites && len(p.WatchedSites) > 0 {
		fmt.Fprintf(&b, "**影响关注站点**：%s\n", taJoinSites(p.WatchedSites, p.MaxListSites))
		if p.TotalSites > 0 {
			fmt.Fprintf(&b, "**影响站点总数**：%d 个（其中关注 %d 个）\n", p.TotalSites, len(p.WatchedSites))
		}
	} else if p.TotalSites > 0 {
		fmt.Fprintf(&b, "**影响站点**：%d 个\n", p.TotalSites)
	}
	if p.Operator != "" {
		fmt.Fprintf(&b, "**最后操作人**：%s\n", p.Operator)
	}
	fmt.Fprintf(&b, "**下次告警**：%s（每 %d 分钟）\n", p.NextAt.Format("15:04"), p.IntervalMin)
	switch {
	case p.WindowName != "" && p.InWindow:
		b.WriteString("\n计划内的例行保养，正常情况无需处理；若提前完成可直接确认。")
	case p.WindowName != "" && p.Overrun > 0:
		b.WriteString("\n**例行维护已超过计划结束时间仍未恢复，请确认现场情况。**")
	case p.Reason == "disabled" || p.Reason == "both":
		b.WriteString("\n**该桌台被标记为在用，却处于停用状态 —— 请确认是否为误操作。**")
	default:
		b.WriteString("\n请确认该桌台是否需要恢复。")
	}

	kind := "alert"
	switch {
	case p.WindowName != "" && p.InWindow:
		kind = "routine"
	case p.WindowName != "" && p.Overrun > 0:
		kind = "overrun"
	case p.Reason == "disabled" || p.Reason == "both":
		kind = "disabled"
	case p.Escalating:
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
	title := fmt.Sprintf("✅ 【%s】桌台已恢复可用", env.Name)
	body := fmt.Sprintf("**桌台**：%s（房间号 %s）\n**状态**：已恢复正常（启用中、无维护）\n**恢复时间**：%s",
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

// ---------------------------------------------------------------------------
// 例行维护窗口
//
// 桌台有计划内的例行保养。这类维护是预期的，跟故障用同样的措辞报出去，
// 时间一长就没人认真看告警了。所以要把两者分开：
//
//   窗口内     → 标注「例行维护」，或按配置完全静默
//   超出窗口   → 说明「例行维护已超时」，这才是真正要人去看的情况
//   没命中窗口 → 计划外维护，照常告警
//
// 归属判定用「维护开始时间」落在哪个窗口，而不是当前时间 —— 这样维护拖到
// 窗口之外时，仍然知道它本来属于哪次例行保养，能报出「超时多久」。
// ---------------------------------------------------------------------------

// TAMaintWindow 例行维护窗口
type TAMaintWindow struct {
	ID           string `json:"id"`
	EnvID        string `json:"env_id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	RepeatType   string `json:"repeat_type"`
	Weekdays     string `json:"weekdays"`
	MonthDays    string `json:"month_days"`
	OnceDate     string `json:"once_date"`
	StartTime    string `json:"start_time"`
	EndTime      string `json:"end_time"`
	TableNos     string `json:"table_nos"`
	Action       string `json:"action"`
	OverrunAlert bool   `json:"overrun_alert"`
	Remark       string `json:"remark"`
}

// taWindowHit 一次维护对例行窗口的命中结果
type taWindowHit struct {
	Window   *TAMaintWindow
	PlanEnd  time.Time // 本次窗口实例的计划结束时间
	InWindow bool      // 当前时刻仍在窗口内
	Overrun  time.Duration
}

// taListWindows 取某环境下启用的全部窗口
func taListWindows(envID string) []TAMaintWindow {
	out := []TAMaintWindow{}
	rows, err := database.DB.Query(`
		SELECT id, env_id, name, enabled, repeat_type, weekdays, month_days, once_date,
		       start_time, end_time, COALESCE(table_nos,''), action, overrun_alert, remark
		FROM table_alert_maint_windows
		WHERE env_id = ? AND enabled = 1`, envID)
	if err != nil {
		taErrorf("读取例行维护窗口失败: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var w TAMaintWindow
		if rows.Scan(&w.ID, &w.EnvID, &w.Name, &w.Enabled, &w.RepeatType, &w.Weekdays,
			&w.MonthDays, &w.OnceDate, &w.StartTime, &w.EndTime, &w.TableNos,
			&w.Action, &w.OverrunAlert, &w.Remark) == nil {
			out = append(out, w)
		}
	}
	return out
}

// taWindowCoversRoom 判断这个窗口管不管这个房间。
//
// 以**房间号**为准：一个桌台可能有多个房间（N13 下有 N013 和 N013-2），
// 它们各自独立维护，配置要能精确到房间，否则维护 N013-2 会把 N013 也算成例行。
//
// 同时兼容填桌台号：填 N13 表示该桌台下的所有房间，省得一个个列。
// `*` 表示全部。
func taWindowCoversRoom(w *TAMaintWindow, roomNo, tableNo string) bool {
	list := strings.TrimSpace(w.TableNos)
	if list == "" {
		return false
	}
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" {
			return true
		}
		// 房间号精确匹配优先；桌台号匹配作为「该桌台全部房间」的批量写法
		if roomNo != "" && strings.EqualFold(item, roomNo) {
			return true
		}
		if tableNo != "" && strings.EqualFold(item, tableNo) {
			return true
		}
	}
	return false
}

// taWindowInstance 计算某个自然日上这个窗口的 [开始, 结束]。
// end <= start 视为跨零点，结束时间落到次日。日期不匹配重复规则时返回零值。
func taWindowInstance(w *TAMaintWindow, day time.Time) (time.Time, time.Time, bool) {
	sh, sm, ok1 := taParseHM(w.StartTime)
	eh, em, ok2 := taParseHM(w.EndTime)
	if !ok1 || !ok2 {
		return time.Time{}, time.Time{}, false
	}

	switch w.RepeatType {
	case "weekly":
		// time.Weekday: 周日=0，这里按 1=周一 … 7=周日 的习惯来配
		wd := int(day.Weekday())
		if wd == 0 {
			wd = 7
		}
		if !taCSVHasInt(w.Weekdays, wd) {
			return time.Time{}, time.Time{}, false
		}
	case "monthly":
		if !taCSVHasInt(w.MonthDays, day.Day()) {
			return time.Time{}, time.Time{}, false
		}
	case "once":
		if day.Format("2006-01-02") != strings.TrimSpace(w.OnceDate) {
			return time.Time{}, time.Time{}, false
		}
	}
	// daily 不额外判断

	start := time.Date(day.Year(), day.Month(), day.Day(), sh, sm, 0, 0, time.Local)
	end := time.Date(day.Year(), day.Month(), day.Day(), eh, em, 0, 0, time.Local)
	if !end.After(start) {
		end = end.AddDate(0, 0, 1) // 跨零点
	}
	return start, end, true
}

// taMatchWindow 判断这次维护属于哪个例行窗口。
// 以 maintainStart 落在窗口实例内为准；检查前后各一天，覆盖跨零点的情况。
func taMatchWindow(envID, roomNo, tableNo string, maintainStart, now time.Time) *taWindowHit {
	if maintainStart.IsZero() {
		return nil
	}
	for _, w := range taListWindows(envID) {
		win := w
		if !taWindowCoversRoom(&win, roomNo, tableNo) {
			continue
		}
		for _, offset := range []int{-1, 0, 1} {
			day := maintainStart.AddDate(0, 0, offset)
			start, end, ok := taWindowInstance(&win, day)
			if !ok {
				continue
			}
			if maintainStart.Before(start) || maintainStart.After(end) {
				continue
			}
			hit := &taWindowHit{Window: &win, PlanEnd: end}
			if now.Before(end) {
				hit.InWindow = true
			} else {
				hit.Overrun = now.Sub(end)
			}
			return hit
		}
	}
	return nil
}

func taParseHM(v string) (int, int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d:%d", &h, &m); err != nil {
		return 0, 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func taCSVHasInt(csv string, n int) bool {
	for _, p := range strings.Split(csv, ",") {
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err == nil && v == n {
			return true
		}
	}
	return false
}

// taFindWindow 按 ID 取窗口（判定 action 用）
func taFindWindow(envID, winID string) *TAMaintWindow {
	for _, w := range taListWindows(envID) {
		if w.ID == winID {
			win := w
			return &win
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 站点字典
//
// 接口只给 siteId，不给名称。要在页面上显示「泰坦体育」而不是一串雪花 ID，
// 就得有个对应关系。手工导入几十上百个 ID 不现实，所以：
//   采集时遇到没见过的 siteId 自动入库（名称留空、默认不关注），
//   使用者只需要给关心的那几个起名 + 打星，其余一直躺着也不碍事。
// ---------------------------------------------------------------------------

// taDiscoverSites 把这一轮采集见到的 siteId 落库，并刷新出现次数。
// 用 INSERT IGNORE + 批量，避免 140 张桌台 × 几十个站点打出上千条单发 SQL。
func taDiscoverSites(envID string, snaps []taRoomSnapshot) {
	// siteID -> 本轮涉及的桌台数
	counter := map[string]int{}
	for _, s := range snaps {
		if !s.Maintaining {
			continue
		}
		seen := map[string]bool{}
		for _, id := range s.SiteIDs {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			counter[id]++
		}
	}
	if len(counter) == 0 {
		return
	}

	now := time.Now()
	newCount := 0
	for siteID, cnt := range counter {
		// INSERT IGNORE：已存在就跳过，绝不覆盖名称和关注状态 ——
		// 人工录入的名字优先，不能被每轮采集冲掉
		res, err := database.DB.Exec(`
			INSERT IGNORE INTO table_alert_sites
			  (id, env_id, site_id, site_name, watched, source, table_count, first_seen_at, last_seen_at)
			VALUES (?,?,?,'',0,'auto',?,?,?)`,
			uuid.New().String(), envID, siteID, cnt, now, now)
		if err != nil {
			taErrorf("站点 %s 入库失败: %v", siteID, err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			newCount++
			continue
		}
		// 已存在：只刷新出现次数和最近时间，不动名称和关注标记
		database.DB.Exec(`
			UPDATE table_alert_sites SET table_count = ?, last_seen_at = ?
			WHERE env_id = ? AND site_id = ?`, cnt, now, envID, siteID)
	}
	if newCount > 0 {
		taInfof("env=%s 新发现 %d 个站点（待命名），本轮共涉及 %d 个站点",
			envID, newCount, len(counter))
	}
}

// taWatchedSites 取某环境下关注的站点，返回 siteID -> 名称
func taWatchedSites(envID string) map[string]string {
	out := map[string]string{}
	rows, err := database.DB.Query(`
		SELECT site_id, site_name FROM table_alert_sites
		WHERE env_id = ? AND watched = 1`, envID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil {
			if name == "" {
				name = id // 关注了却没起名，退而显示 ID，总比空白强
			}
			out[id] = name
		}
	}
	return out
}

// taPickWatched 从受影响站点里挑出关注的，返回名称列表（保持接口返回的顺序）
func taPickWatched(siteIDs []string, watched map[string]string) []string {
	if len(watched) == 0 {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, id := range siteIDs {
		if name, ok := watched[id]; ok && !seen[id] {
			seen[id] = true
			out = append(out, name)
		}
	}
	return out
}

// taJoinSites 把站点名拼成一行，超过 max 个就收尾成「等 N 个」，
// 免得卡片被几十个站点名刷屏。
func taJoinSites(names []string, max int) string {
	if len(names) == 0 {
		return ""
	}
	if max <= 0 {
		max = 5
	}
	if len(names) <= max {
		return strings.Join(names, "、")
	}
	return strings.Join(names[:max], "、") + fmt.Sprintf(" 等 %d 个", len(names))
}
