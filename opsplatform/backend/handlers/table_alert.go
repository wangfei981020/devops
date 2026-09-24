package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/services"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ============================================================================
// 桌台维护告警 —— HTTP 接口
//
// 路由前缀 /api/table-alert，前端页面 /table-alert，权限码 table_alert。
// 与「桌台维护记录 / 桌台配置 / 桌台管理」三个老菜单互不影响。
// ============================================================================

// ---------------------------------------------------------------------------
// 环境配置
// ---------------------------------------------------------------------------

// HandleTAListEnvs GET /api/table-alert/envs
func HandleTAListEnvs(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	envs, err := taListEnvs(false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "读取环境失败: "+err.Error())
		return
	}
	// token 不回传明文，只告诉前端配没配
	out := make([]map[string]interface{}, 0, len(envs))
	for _, e := range envs {
		m := taEnvToMap(e)
		out = append(out, m)
	}
	respondJSON(w, http.StatusOK, out)
}

func taEnvToMap(e TAEnv) map[string]interface{} {
	b, _ := json.Marshal(e)
	var m map[string]interface{}
	json.Unmarshal(b, &m)
	m["token"] = ""
	m["has_token"] = strings.TrimSpace(e.Token) != ""
	return m
}

// HandleTACreateEnv POST /api/table-alert/envs
func HandleTACreateEnv(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermEnvCreate) {
		return
	}
	var e TAEnv
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if strings.TrimSpace(e.Name) == "" {
		respondError(w, http.StatusBadRequest, "环境名称不能为空")
		return
	}
	taFillEnvDefaults(&e)

	id := uuid.New().String()
	_, err := database.DB.Exec(`
		INSERT INTO table_alert_envs
		  (id, name, enabled, sort_order, url, method, host_header, request_body, extra_headers,
		   token, token_place, skip_tls_verify, timeout_sec, cur_page, page_size,
		   data_path, total_path, f_room_id, f_table_no, f_room_no, f_platform_id,
		   f_status, f_maintain, f_operator, f_update_time, f_online_total,
		   maintain_rule, maintain_status_value, interval_sec, log_raw_response, created_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, e.Name, e.Enabled, e.Sort, e.URL, e.Method, e.HostHeader, e.RequestBody, e.ExtraHeaders,
		e.Token, e.TokenPlace, e.SkipTLSVerify, e.TimeoutSec, e.CurPage, e.PageSize,
		e.DataPath, e.TotalPath, e.FRoomID, e.FTableNo, e.FRoomNo, e.FPlatformID,
		e.FStatus, e.FMaintain, e.FOperator, e.FUpdateTime, e.FOnlineTotal,
		e.MaintainRule, e.MaintainStatusValue, e.IntervalSec, e.LogRawResponse, taOperator(r))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	taInfof("环境 %s 已创建（操作人 %s）", e.Name, taOperator(r))
	respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

// HandleTAUpdateEnv PUT /api/table-alert/envs/{id}
func HandleTAUpdateEnv(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermEnvUpdate) {
		return
	}
	id := mux.Vars(r)["id"]
	var e TAEnv
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	taFillEnvDefaults(&e)

	// token 留空表示不修改，避免前端拿不到明文回填时把已配置的 token 清掉
	tokenClause := ""
	args := []interface{}{
		e.Name, e.Enabled, e.Sort, e.URL, e.Method, e.HostHeader, e.RequestBody, e.ExtraHeaders,
		e.TokenPlace, e.SkipTLSVerify, e.TimeoutSec, e.CurPage, e.PageSize,
		e.DataPath, e.TotalPath, e.FRoomID, e.FTableNo, e.FRoomNo, e.FPlatformID,
		e.FStatus, e.FMaintain, e.FOperator, e.FUpdateTime, e.FOnlineTotal,
		e.MaintainRule, e.MaintainStatusValue, e.IntervalSec, e.LogRawResponse,
	}
	if strings.TrimSpace(e.Token) != "" {
		tokenClause = ", token=?"
		args = append(args, e.Token)
	}
	args = append(args, id)

	_, err := database.DB.Exec(`
		UPDATE table_alert_envs SET
		  name=?, enabled=?, sort_order=?, url=?, method=?, host_header=?, request_body=?, extra_headers=?,
		  token_place=?, skip_tls_verify=?, timeout_sec=?, cur_page=?, page_size=?,
		  data_path=?, total_path=?, f_room_id=?, f_table_no=?, f_room_no=?, f_platform_id=?,
		  f_status=?, f_maintain=?, f_operator=?, f_update_time=?, f_online_total=?,
		  maintain_rule=?, maintain_status_value=?, interval_sec=?, log_raw_response=?`+tokenClause+`
		WHERE id=?`, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	taInfof("环境 %s 已更新（操作人 %s）", e.Name, taOperator(r))
	respondJSON(w, http.StatusOK, map[string]string{"message": "已保存"})
}

// HandleTADeleteEnv DELETE /api/table-alert/envs/{id}
func HandleTADeleteEnv(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermEnvDelete) {
		return
	}
	id := mux.Vars(r)["id"]
	for _, t := range []string{
		"table_alert_rooms", "table_alert_events", "table_alert_collect_logs",
	} {
		database.DB.Exec("DELETE FROM "+t+" WHERE env_id=?", id)
	}
	var ruleID string
	if database.DB.QueryRow(`SELECT id FROM table_alert_rules WHERE env_id=?`, id).Scan(&ruleID) == nil {
		database.DB.Exec(`DELETE FROM table_alert_rule_bots WHERE rule_id=?`, ruleID)
		database.DB.Exec(`DELETE FROM table_alert_rules WHERE id=?`, ruleID)
	}
	if _, err := database.DB.Exec(`DELETE FROM table_alert_envs WHERE id=?`, id); err != nil {
		respondError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已删除"})
}

// HandleTATestEnv POST /api/table-alert/envs/{id}/test
// 用当前（可能未保存的）配置试一次，不写快照、不产生事件，只回显结果给页面。
func HandleTATestEnv(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermCollect) {
		return
	}
	id := mux.Vars(r)["id"]
	env, err := taGetEnv(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// 允许前端传临时覆盖，方便「先测再存」
	var override TAEnv
	if json.NewDecoder(r.Body).Decode(&override) == nil {
		if strings.TrimSpace(override.URL) != "" {
			env.URL = override.URL
		}
		if strings.TrimSpace(override.HostHeader) != "" {
			env.HostHeader = override.HostHeader
		}
		if strings.TrimSpace(override.Method) != "" {
			env.Method = override.Method
		}
		if strings.TrimSpace(override.Token) != "" {
			env.Token = override.Token
			env.TokenPlace = override.TokenPlace
		}
		if override.SkipTLSVerify {
			env.SkipTLSVerify = true
		}
	}

	taInfof("环境 %s 手动测试连接（操作人 %s）", env.Name, taOperator(r))
	res, sample, err := taTestFetch(env)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok":     false,
			"error":  err.Error(),
			"result": res,
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":     true,
		"result": res,
		"sample": sample,
	})
}

// HandleTACollectNow POST /api/table-alert/envs/{id}/collect  立即采集一次（真正落库）
func HandleTACollectNow(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermCollect) {
		return
	}
	id := mux.Vars(r)["id"]
	env, err := taGetEnv(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	taInfof("环境 %s 手动触发采集（操作人 %s）", env.Name, taOperator(r))

	taCollectMu.Lock()
	res, err := TACollectOnce(env)
	taCollectMu.Unlock()

	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error(), "result": res})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "result": res})
}

func taFillEnvDefaults(e *TAEnv) {
	e.Method = strings.ToUpper(taDefaultStr(e.Method, "GET"))
	e.TokenPlace = taDefaultStr(e.TokenPlace, "none")
	e.TimeoutSec = taDefaultInt(e.TimeoutSec, 10)
	e.CurPage = taDefaultInt(e.CurPage, 1)
	e.PageSize = taDefaultInt(e.PageSize, 500)
	e.DataPath = taDefaultStr(e.DataPath, "data.records")
	e.TotalPath = taDefaultStr(e.TotalPath, "data.total")
	e.FRoomID = taDefaultStr(e.FRoomID, "id")
	e.FTableNo = taDefaultStr(e.FTableNo, "tableNo")
	e.FRoomNo = taDefaultStr(e.FRoomNo, "roomNo")
	e.FPlatformID = taDefaultStr(e.FPlatformID, "gamePlatformId")
	e.FStatus = taDefaultStr(e.FStatus, "status")
	e.FMaintain = taDefaultStr(e.FMaintain, "gameRoomMaintainList")
	e.FOperator = taDefaultStr(e.FOperator, "operator")
	e.FUpdateTime = taDefaultStr(e.FUpdateTime, "updateTime")
	e.FOnlineTotal = taDefaultStr(e.FOnlineTotal, "onlineUserTotal")
	e.MaintainRule = taDefaultStr(e.MaintainRule, "list_not_empty")
	// 采集间隔下限 10 秒，防止误填 1 秒把中台打爆
	if e.IntervalSec < 10 {
		e.IntervalSec = 60
	}
}

// ---------------------------------------------------------------------------
// 桌台列表 / 统计
// ---------------------------------------------------------------------------

// HandleTAListRooms GET /api/table-alert/rooms
func HandleTAListRooms(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	q := r.URL.Query()
	envID := q.Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}

	where := []string{"env_id = ?"}
	args := []interface{}{envID}

	if s := q.Get("status"); s != "" {
		where = append(where, "status = ?")
		args = append(args, s)
	}
	switch q.Get("maintaining") {
	case "1":
		where = append(where, "maintaining = 1")
	case "0":
		where = append(where, "maintaining = 0")
	}
	if kw := strings.TrimSpace(q.Get("q")); kw != "" {
		where = append(where, "(table_no LIKE ? OR room_no LIKE ? OR platform_id LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	if p := q.Get("platform_id"); p != "" {
		where = append(where, "platform_id = ?")
		args = append(args, p)
	}

	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int
	database.DB.QueryRow("SELECT COUNT(*) FROM table_alert_rooms"+whereSQL, args...).Scan(&total)

	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 500 {
		size = 20
	}

	rows, err := database.DB.Query(`
		SELECT room_id, table_no, room_no, platform_id, status, maintaining, maintain_site_count,
		       online_user_total, operator, remote_update_time,
		       DATE_FORMAT(maintain_since, '%Y-%m-%d %H:%i:%s'),
		       DATE_FORMAT(last_seen_at, '%Y-%m-%d %H:%i:%s'), since_estimated,
		       COALESCE(maintain_site_ids,'')
		FROM table_alert_rooms`+whereSQL+`
		ORDER BY maintaining DESC, table_no
		LIMIT ? OFFSET ?`, append(args, size, (page-1)*size)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	items := []map[string]interface{}{}
	now := time.Now()
	watchedMap := taWatchedSites(envID) // 循环外取一次，别每行都查库
	for rows.Next() {
		var (
			roomID, tableNo, roomNo, platformID, status, operator, remoteUpd string
			maintaining, sinceEstimated                                      bool
			siteCount, online                                                int
			since, lastSeen                                                  sql.NullString
			siteIDsRaw                                                       string
		)
		if err := rows.Scan(&roomID, &tableNo, &roomNo, &platformID, &status, &maintaining,
			&siteCount, &online, &operator, &remoteUpd, &since, &lastSeen, &sinceEstimated,
			&siteIDsRaw); err != nil {
			continue
		}
		item := map[string]interface{}{
			"room_id": roomID, "table_no": tableNo, "room_no": roomNo,
			"platform_id": platformID, "status": status, "maintaining": maintaining,
			"maintain_site_count": siteCount, "online_user_total": online,
			"operator": operator, "remote_update_time": remoteUpd,
			"maintain_since": since.String, "last_seen_at": lastSeen.String,
			"since_estimated": sinceEstimated,
			"duration_text":   "", "duration_min": 0,
		}
		if maintaining && since.Valid {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", since.String, time.Local); err == nil {
				d := now.Sub(t)
				item["duration_text"] = taHumanDur(d)
				item["duration_min"] = int(d.Minutes())
			}
		}
		// 列表上只展示关注的站点，全部站点放详情 ——
		// 27 个雪花 ID 平铺在列里没人看得下去
		item["watched_sites"] = []string{}
		item["watched_site_count"] = 0
		if maintaining && siteIDsRaw != "" {
			names := taPickWatched(strings.Split(siteIDsRaw, ","), watchedMap)
			item["watched_sites"] = names
			item["watched_site_count"] = len(names)
		}
		// 带上告警次数、确认状态，以及本次维护属不属于例行窗口
		var alertCount int
		var state, ackedBy, winName string
		var winEnd sql.NullTime
		database.DB.QueryRow(`
			SELECT alert_count, state, acked_by, window_name, window_end_at
			FROM table_alert_events
			WHERE env_id=? AND room_id=? AND maintain_end_at IS NULL
			ORDER BY maintain_start_at DESC LIMIT 1`, envID, roomID).
			Scan(&alertCount, &state, &ackedBy, &winName, &winEnd)
		item["alert_count"] = alertCount
		item["event_state"] = state
		item["acked_by"] = ackedBy
		item["window_name"] = winName
		item["window_overrun"] = false
		if winName != "" && winEnd.Valid {
			item["window_end_at"] = winEnd.Time.Format("2006-01-02 15:04:05")
			if now.After(winEnd.Time) {
				item["window_overrun"] = true
				item["window_overrun_text"] = taHumanDur(now.Sub(winEnd.Time))
			}
		}
		items = append(items, item)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": total, "page": page, "size": size,
	})
}

// HandleTAStats GET /api/table-alert/stats?env_id=
func HandleTAStats(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	envID := r.URL.Query().Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}
	var total, enable, disable, maintaining, alerting int
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_rooms WHERE env_id=?`, envID).Scan(&total)
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_rooms WHERE env_id=? AND status='Enable'`, envID).Scan(&enable)
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_rooms WHERE env_id=? AND status='Disable'`, envID).Scan(&disable)
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_rooms WHERE env_id=? AND maintaining=1`, envID).Scan(&maintaining)
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_events WHERE env_id=? AND state='alerting' AND maintain_end_at IS NULL`, envID).Scan(&alerting)

	respondJSON(w, http.StatusOK, map[string]int{
		"total": total, "enable": enable, "disable": disable,
		"maintaining": maintaining, "alerting": alerting,
	})
}

// ---------------------------------------------------------------------------
// 告警规则
// ---------------------------------------------------------------------------

// HandleTAGetRule GET /api/table-alert/rules?env_id=
func HandleTAGetRule(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	envID := r.URL.Query().Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}
	rule, err := taGetRule(envID)
	if err != nil {
		// 还没配过，回一份默认值给页面
		respondJSON(w, http.StatusOK, &TARule{
			EnvID: envID, Enabled: true, ThresholdMin: 10, IntervalMin: 10,
			MaxTimes: 6, Escalate: true, EscalateIntervalMin: 30, NotifyOnRecover: true,
			ReatEveryTime: true, SilenceAfterAckMin: 30,
			QuietStart: "03:00", QuietEnd: "08:00",
			AlertScope: "all", ListWatchedSites: true, MaxListSites: 5,
			BotIDs: []string{},
		})
		return
	}
	respondJSON(w, http.StatusOK, rule)
}

// HandleTASaveRule PUT /api/table-alert/rules
func HandleTASaveRule(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRuleUpdate) {
		return
	}
	var rule TARule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if rule.EnvID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}

	// 告警间隔不得小于采集间隔 —— 否则是假精度：数据没更新就重复报同一个快照
	if env, err := taGetEnv(rule.EnvID); err == nil {
		minMin := (env.IntervalSec + 59) / 60
		if minMin < 1 {
			minMin = 1
		}
		if rule.IntervalMin < minMin {
			respondError(w, http.StatusBadRequest,
				fmt.Sprintf("告警间隔不能小于采集间隔：该环境每 %d 秒采集一次，告警间隔至少 %d 分钟",
					env.IntervalSec, minMin))
			return
		}
	}
	if rule.ThresholdMin < 0 {
		rule.ThresholdMin = 0
	}
	if rule.MaxTimes < 1 {
		rule.MaxTimes = 1
	}
	if rule.AlertScope != "watched" {
		rule.AlertScope = "all"
	}
	if rule.MaxListSites < 1 {
		rule.MaxListSites = 5
	}

	var existID string
	err := database.DB.QueryRow(`SELECT id FROM table_alert_rules WHERE env_id=?`, rule.EnvID).Scan(&existID)
	if err == sql.ErrNoRows {
		existID = uuid.New().String()
		_, err = database.DB.Exec(`
			INSERT INTO table_alert_rules
			  (id, env_id, enabled, threshold_min, interval_min, max_times, escalate,
			   escalate_interval_min, notify_on_recover, at_lark_ids, escalate_at_lark_ids,
			   reat_every_time, silence_after_ack_min, quiet_enabled, quiet_start, quiet_end,
			   alert_scope, list_watched_sites, max_list_sites)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			existID, rule.EnvID, rule.Enabled, rule.ThresholdMin, rule.IntervalMin, rule.MaxTimes,
			rule.Escalate, rule.EscalateIntervalMin, rule.NotifyOnRecover, rule.AtLarkIDs,
			rule.EscalateAtLarkIDs, rule.ReatEveryTime, rule.SilenceAfterAckMin,
			rule.QuietEnabled, rule.QuietStart, rule.QuietEnd,
			rule.AlertScope, rule.ListWatchedSites, rule.MaxListSites)
	} else if err == nil {
		_, err = database.DB.Exec(`
			UPDATE table_alert_rules SET
			  enabled=?, threshold_min=?, interval_min=?, max_times=?, escalate=?,
			  escalate_interval_min=?, notify_on_recover=?, at_lark_ids=?, escalate_at_lark_ids=?,
			  reat_every_time=?, silence_after_ack_min=?, quiet_enabled=?, quiet_start=?, quiet_end=?,
			  alert_scope=?, list_watched_sites=?, max_list_sites=?
			WHERE id=?`,
			rule.Enabled, rule.ThresholdMin, rule.IntervalMin, rule.MaxTimes, rule.Escalate,
			rule.EscalateIntervalMin, rule.NotifyOnRecover, rule.AtLarkIDs, rule.EscalateAtLarkIDs,
			rule.ReatEveryTime, rule.SilenceAfterAckMin, rule.QuietEnabled,
			rule.QuietStart, rule.QuietEnd,
			rule.AlertScope, rule.ListWatchedSites, rule.MaxListSites, existID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}

	// 绑定的群（多对多，可发多个群）
	database.DB.Exec(`DELETE FROM table_alert_rule_bots WHERE rule_id=?`, existID)
	for _, bid := range rule.BotIDs {
		if strings.TrimSpace(bid) == "" {
			continue
		}
		database.DB.Exec(`INSERT IGNORE INTO table_alert_rule_bots (rule_id, bot_id) VALUES (?,?)`, existID, bid)
	}

	taInfof("环境 %s 的告警规则已保存：阈值 %d 分钟 / 每 %d 分钟一次 / 最多 %d 次 / 绑定 %d 个群（操作人 %s）",
		rule.EnvID, rule.ThresholdMin, rule.IntervalMin, rule.MaxTimes, len(rule.BotIDs), taOperator(r))
	respondJSON(w, http.StatusOK, map[string]string{"message": "已保存", "id": existID})
}

// ---------------------------------------------------------------------------
// Lark 机器人（多个群）
// ---------------------------------------------------------------------------

// HandleTAListBots GET /api/table-alert/bots
func HandleTAListBots(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	rows, err := database.DB.Query(`
		SELECT id, name, webhook, COALESCE(secret,''), description, enabled
		FROM table_alert_lark_bots ORDER BY name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var id, name, webhook, secret, desc string
		var enabled bool
		if rows.Scan(&id, &name, &webhook, &secret, &desc, &enabled) != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id": id, "name": name, "description": desc, "enabled": enabled,
			// webhook 含密钥，只回显掩码
			"webhook_masked": taMaskWebhook(webhook),
			"has_webhook":    webhook != "",
			"has_secret":     secret != "",
		})
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleTASaveBot POST /api/table-alert/bots（无 id 新增，有 id 更新）
func HandleTASaveBot(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermBotManage) {
		return
	}
	var b struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Webhook     string `json:"webhook"`
		Secret      string `json:"secret"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		respondError(w, http.StatusBadRequest, "群名称不能为空")
		return
	}

	if b.ID == "" {
		if strings.TrimSpace(b.Webhook) == "" {
			respondError(w, http.StatusBadRequest, "webhook 不能为空")
			return
		}
		id := uuid.New().String()
		_, err := database.DB.Exec(`
			INSERT INTO table_alert_lark_bots (id, name, webhook, secret, description, enabled)
			VALUES (?,?,?,?,?,?)`, id, b.Name, b.Webhook, b.Secret, b.Description, b.Enabled)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
			return
		}
		taInfof("Lark 群「%s」已添加（操作人 %s）", b.Name, taOperator(r))
		respondJSON(w, http.StatusOK, map[string]string{"id": id})
		return
	}

	// webhook / secret 留空表示不修改
	set := "name=?, description=?, enabled=?"
	args := []interface{}{b.Name, b.Description, b.Enabled}
	if strings.TrimSpace(b.Webhook) != "" {
		set += ", webhook=?"
		args = append(args, b.Webhook)
	}
	if strings.TrimSpace(b.Secret) != "" {
		set += ", secret=?"
		args = append(args, b.Secret)
	}
	args = append(args, b.ID)
	if _, err := database.DB.Exec(`UPDATE table_alert_lark_bots SET `+set+` WHERE id=?`, args...); err != nil {
		respondError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已保存"})
}

// HandleTADeleteBot DELETE /api/table-alert/bots/{id}
func HandleTADeleteBot(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermBotManage) {
		return
	}
	id := mux.Vars(r)["id"]
	database.DB.Exec(`DELETE FROM table_alert_rule_bots WHERE bot_id=?`, id)
	if _, err := database.DB.Exec(`DELETE FROM table_alert_lark_bots WHERE id=?`, id); err != nil {
		respondError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已删除"})
}

// HandleTATestBot POST /api/table-alert/bots/{id}/test  往这个群发一条测试卡片
func HandleTATestBot(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermBotManage) {
		return
	}
	id := mux.Vars(r)["id"]
	var name, webhook, secret string
	err := database.DB.QueryRow(`
		SELECT name, webhook, COALESCE(secret,'') FROM table_alert_lark_bots WHERE id=?`,
		id).Scan(&name, &webhook, &secret)
	if err != nil {
		respondError(w, http.StatusNotFound, "机器人不存在")
		return
	}
	if webhook == "" {
		respondError(w, http.StatusBadRequest, "该机器人未配置 webhook")
		return
	}

	// 允许把页面上还没保存的艾特人一起测
	var body struct {
		AtLarkIDs string `json:"at_lark_ids"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	atIDs := taSplitIDs(body.AtLarkIDs)

	operator := taOperator(r)
	title := "🔧 桌台维护告警 · 测试消息"
	msg := fmt.Sprintf(
		"这是一条来自运维管理平台「桌台维护告警」的测试消息。\n\n**发送时间**：%s\n**触发人**：%s\n\n收到说明该群的 webhook 配置正常。",
		time.Now().Format("2006-01-02 15:04:05"), operator)

	taInfof("向群「%s」发送测试消息（操作人 %s，艾特 %d 人）", name, operator, len(atIDs))

	// 测试不重试，失败立刻把原因回给页面
	sendErr := services.SendLarkCard(context.Background(), webhook, secret,
		title, msg, "blue", "", "", atIDs...)

	database.DB.Exec(`
		INSERT INTO table_alert_notify_logs
		  (id, event_id, env_name, table_no, bot_id, bot_name, kind, seq, at_lark_ids, title, body, ok, error_msg, sent_at)
		VALUES (?,'','','',?,?,'test',0,?,?,?,?,?,?)`,
		uuid.New().String(), id, name, strings.Join(atIDs, ","), title, msg,
		sendErr == nil, taErrText(sendErr), time.Now())

	if sendErr != nil {
		taErrorf("群「%s」测试发送失败: %v", name, sendErr)
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": sendErr.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "已发送，请到群里确认"})
}

// ---------------------------------------------------------------------------
// 通知人
// ---------------------------------------------------------------------------

// HandleTAListContacts GET /api/table-alert/contacts
func HandleTAListContacts(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	rows, err := database.DB.Query(`
		SELECT id, name, lark_id, remark FROM table_alert_contacts ORDER BY name`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, name, larkID, remark string
		if rows.Scan(&id, &name, &larkID, &remark) != nil {
			continue
		}
		out = append(out, map[string]string{"id": id, "name": name, "lark_id": larkID, "remark": remark})
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleTASaveContact POST /api/table-alert/contacts
func HandleTASaveContact(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermContactManage) {
		return
	}
	var c struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		LarkID string `json:"lark_id"`
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if strings.TrimSpace(c.Name) == "" {
		respondError(w, http.StatusBadRequest, "姓名不能为空")
		return
	}
	var err error
	if c.ID == "" {
		_, err = database.DB.Exec(`
			INSERT INTO table_alert_contacts (id, name, lark_id, remark) VALUES (?,?,?,?)`,
			uuid.New().String(), c.Name, c.LarkID, c.Remark)
	} else {
		_, err = database.DB.Exec(`
			UPDATE table_alert_contacts SET name=?, lark_id=?, remark=? WHERE id=?`,
			c.Name, c.LarkID, c.Remark, c.ID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已保存"})
}

// HandleTADeleteContact DELETE /api/table-alert/contacts/{id}
func HandleTADeleteContact(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermContactManage) {
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM table_alert_contacts WHERE id=?`, mux.Vars(r)["id"]); err != nil {
		respondError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已删除"})
}

// ---------------------------------------------------------------------------
// 告警事件 / 确认
// ---------------------------------------------------------------------------

// HandleTAListEvents GET /api/table-alert/events
func HandleTAListEvents(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	q := r.URL.Query()
	where := []string{"1=1"}
	args := []interface{}{}
	if v := q.Get("env_id"); v != "" {
		where = append(where, "env_id=?")
		args = append(args, v)
	}
	if v := q.Get("state"); v != "" {
		where = append(where, "state=?")
		args = append(args, v)
	}
	if q.Get("active") == "1" {
		where = append(where, "maintain_end_at IS NULL")
	}
	if kw := strings.TrimSpace(q.Get("q")); kw != "" {
		where = append(where, "(table_no LIKE ? OR room_no LIKE ?)")
		args = append(args, "%"+kw+"%", "%"+kw+"%")
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int
	database.DB.QueryRow("SELECT COUNT(*) FROM table_alert_events"+whereSQL, args...).Scan(&total)

	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}

	rows, err := database.DB.Query(`
		SELECT id, env_name, room_id, table_no, room_no, platform_id, start_estimated,
		       DATE_FORMAT(maintain_start_at,'%Y-%m-%d %H:%i:%s'),
		       DATE_FORMAT(maintain_end_at,'%Y-%m-%d %H:%i:%s'),
		       site_count, operator, alert_count,
		       DATE_FORMAT(last_alert_at,'%Y-%m-%d %H:%i:%s'),
		       DATE_FORMAT(next_alert_at,'%Y-%m-%d %H:%i:%s'),
		       escalated, state, acked_by,
		       DATE_FORMAT(acked_at,'%Y-%m-%d %H:%i:%s')
		FROM table_alert_events`+whereSQL+`
		ORDER BY maintain_start_at DESC LIMIT ? OFFSET ?`, append(args, size, (page-1)*size)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	items := []map[string]interface{}{}
	for rows.Next() {
		var (
			id, envName, roomID, tableNo, roomNo, platformID, operator, state, ackedBy string
			startAt                                                                    string
			endAt, lastAlert, nextAlert, ackedAt                                       sql.NullString
			siteCount, alertCount                                                      int
			escalated, startEstimated                                                  bool
		)
		if rows.Scan(&id, &envName, &roomID, &tableNo, &roomNo, &platformID, &startEstimated, &startAt, &endAt,
			&siteCount, &operator, &alertCount, &lastAlert, &nextAlert, &escalated,
			&state, &ackedBy, &ackedAt) != nil {
			continue
		}
		dur := ""
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", startAt, time.Local); err == nil {
			end := time.Now()
			if endAt.Valid {
				if te, err := time.ParseInLocation("2006-01-02 15:04:05", endAt.String, time.Local); err == nil {
					end = te
				}
			}
			dur = taHumanDur(end.Sub(t))
		}
		items = append(items, map[string]interface{}{
			"id": id, "env_name": envName, "room_id": roomID, "table_no": tableNo,
			"room_no": roomNo, "platform_id": platformID,
			"maintain_start_at": startAt, "start_estimated": startEstimated,
			"maintain_end_at": endAt.String,
			"duration_text":   dur, "site_count": siteCount, "operator": operator,
			"alert_count": alertCount, "last_alert_at": lastAlert.String,
			"next_alert_at": nextAlert.String, "escalated": escalated,
			"state": state, "acked_by": ackedBy, "acked_at": ackedAt.String,
		})
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": total, "page": page, "size": size,
	})
}

// HandleTAAckEvent POST /api/table-alert/events/{id}/ack  人工确认，停止告警并静默
func HandleTAAckEvent(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermAck) {
		return
	}
	id := mux.Vars(r)["id"]
	operator := taOperator(r)

	var envID, tableNo, envName string
	err := database.DB.QueryRow(`
		SELECT env_id, env_name, table_no FROM table_alert_events WHERE id=?`,
		id).Scan(&envID, &envName, &tableNo)
	if err != nil {
		respondError(w, http.StatusNotFound, "事件不存在")
		return
	}

	silenceMin := 30
	if rule, err := taGetRule(envID); err == nil && rule.SilenceAfterAckMin > 0 {
		silenceMin = rule.SilenceAfterAckMin
	}
	until := time.Now().Add(time.Duration(silenceMin) * time.Minute)

	_, err = database.DB.Exec(`
		UPDATE table_alert_events
		SET state='acked', acked_by=?, acked_at=?, silence_until=?, next_alert_at=?
		WHERE id=?`, operator, time.Now(), until, until, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "确认失败: "+err.Error())
		return
	}
	taInfof("env=%s 桌台 %s 已被 %s 确认，静默至 %s",
		envName, tableNo, operator, until.Format("15:04:05"))
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "已确认", "silence_until": until.Format("2006-01-02 15:04:05"),
	})
}

// HandleTABatchAck POST /api/table-alert/events/ack-batch  批量确认（同一次维护常是整组桌台）
func HandleTABatchAck(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermAck) {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		respondError(w, http.StatusBadRequest, "请选择要确认的事件")
		return
	}
	operator := taOperator(r)
	okCount := 0
	for _, id := range body.IDs {
		var envID string
		if database.DB.QueryRow(`SELECT env_id FROM table_alert_events WHERE id=?`, id).Scan(&envID) != nil {
			continue
		}
		silenceMin := 30
		if rule, err := taGetRule(envID); err == nil && rule.SilenceAfterAckMin > 0 {
			silenceMin = rule.SilenceAfterAckMin
		}
		until := time.Now().Add(time.Duration(silenceMin) * time.Minute)
		if _, err := database.DB.Exec(`
			UPDATE table_alert_events
			SET state='acked', acked_by=?, acked_at=?, silence_until=?, next_alert_at=?
			WHERE id=?`, operator, time.Now(), until, until, id); err == nil {
			okCount++
		}
	}
	taInfof("%s 批量确认了 %d/%d 个事件", operator, okCount, len(body.IDs))
	respondJSON(w, http.StatusOK, map[string]interface{}{"message": "已确认", "count": okCount})
}

// ---------------------------------------------------------------------------
// 采集日志 / 通知记录
// ---------------------------------------------------------------------------

// HandleTAListCollectLogs GET /api/table-alert/collect-logs
func HandleTAListCollectLogs(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	q := r.URL.Query()
	where := []string{"1=1"}
	args := []interface{}{}
	if v := q.Get("env_id"); v != "" {
		where = append(where, "env_id=?")
		args = append(args, v)
	}
	switch q.Get("ok") {
	case "1":
		where = append(where, "ok=1")
	case "0":
		where = append(where, "ok=0")
	}
	if q.Get("has_change") == "1" {
		where = append(where, "change_count > 0")
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int
	database.DB.QueryRow("SELECT COUNT(*) FROM table_alert_collect_logs"+whereSQL, args...).Scan(&total)

	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}

	// 列表里不带 raw_response（可能几百 KB），要看点「查看原始响应」单独取
	rows, err := database.DB.Query(`
		SELECT id, env_name, DATE_FORMAT(started_at,'%Y-%m-%d %H:%i:%s'), duration_ms,
		       http_status, ok, COALESCE(error_msg,''), COALESCE(request_url,''),
		       record_count, total_count, enable_count, disable_count, maintain_count,
		       change_count, COALESCE(changes,'[]'), raw_size
		FROM table_alert_collect_logs`+whereSQL+`
		ORDER BY started_at DESC LIMIT ? OFFSET ?`, append(args, size, (page-1)*size)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	items := []map[string]interface{}{}
	for rows.Next() {
		var (
			id, envName, startedAt, errMsg, reqURL, changes string
			durationMs, httpStatus, recordCount, totalCount int
			enableCount, disableCount, maintainCount        int
			changeCount, rawSize                            int
			ok                                              bool
		)
		if rows.Scan(&id, &envName, &startedAt, &durationMs, &httpStatus, &ok, &errMsg, &reqURL,
			&recordCount, &totalCount, &enableCount, &disableCount, &maintainCount,
			&changeCount, &changes, &rawSize) != nil {
			continue
		}
		var parsed []taChange
		json.Unmarshal([]byte(changes), &parsed)
		items = append(items, map[string]interface{}{
			"id": id, "env_name": envName, "started_at": startedAt,
			"duration_ms": durationMs, "http_status": httpStatus, "ok": ok,
			"error_msg": errMsg, "request_url": reqURL,
			"record_count": recordCount, "total_count": totalCount,
			"enable_count": enableCount, "disable_count": disableCount,
			"maintain_count": maintainCount, "change_count": changeCount,
			"changes": parsed, "raw_size": rawSize, "has_raw": rawSize > 0,
		})
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": total, "page": page, "size": size,
	})
}

// HandleTAGetRawResponse GET /api/table-alert/collect-logs/{id}/raw  取某次采集的原始响应
func HandleTAGetRawResponse(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermViewRaw) {
		return
	}
	id := mux.Vars(r)["id"]
	var raw sql.NullString
	var envName, startedAt string
	err := database.DB.QueryRow(`
		SELECT COALESCE(raw_response,''), env_name, DATE_FORMAT(started_at,'%Y-%m-%d %H:%i:%s')
		FROM table_alert_collect_logs WHERE id=?`, id).Scan(&raw, &envName, &startedAt)
	if err != nil {
		respondError(w, http.StatusNotFound, "日志不存在")
		return
	}
	if !raw.Valid || raw.String == "" {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"raw":  "",
			"note": "该次采集未保存原始响应（环境配置里「记录原始响应」是关闭的，或采集成功且未开启调试）",
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"raw": raw.String, "env_name": envName, "started_at": startedAt,
	})
}

// HandleTAListNotifyLogs GET /api/table-alert/notify-logs
func HandleTAListNotifyLogs(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	q := r.URL.Query()
	where := []string{"1=1"}
	args := []interface{}{}
	if v := q.Get("event_id"); v != "" {
		where = append(where, "event_id=?")
		args = append(args, v)
	}
	if v := q.Get("kind"); v != "" {
		where = append(where, "kind=?")
		args = append(args, v)
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int
	database.DB.QueryRow("SELECT COUNT(*) FROM table_alert_notify_logs"+whereSQL, args...).Scan(&total)

	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}

	rows, err := database.DB.Query(`
		SELECT id, event_id, env_name, table_no, bot_name, kind, seq, at_lark_ids,
		       title, ok, COALESCE(error_msg,''), DATE_FORMAT(sent_at,'%Y-%m-%d %H:%i:%s')
		FROM table_alert_notify_logs`+whereSQL+`
		ORDER BY sent_at DESC LIMIT ? OFFSET ?`, append(args, size, (page-1)*size)...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	items := []map[string]interface{}{}
	for rows.Next() {
		var id, eventID, envName, tableNo, botName, kind, atIDs, title, errMsg, sentAt string
		var seq int
		var ok bool
		if rows.Scan(&id, &eventID, &envName, &tableNo, &botName, &kind, &seq,
			&atIDs, &title, &ok, &errMsg, &sentAt) != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id": id, "event_id": eventID, "env_name": envName, "table_no": tableNo,
			"bot_name": botName, "kind": kind, "seq": seq, "at_lark_ids": atIDs,
			"title": title, "ok": ok, "error_msg": errMsg, "sent_at": sentAt,
		})
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": total, "page": page, "size": size,
	})
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// taTestFetch 只拉一次并解析，不落库 —— 给「测试连接」按钮用
func taTestFetch(env *TAEnv) (*TACollectResult, []map[string]interface{}, error) {
	started := time.Now()
	res := &TACollectResult{EnvName: env.Name}

	reqURL, req, err := taBuildRequest(env)
	if err != nil {
		res.Error = err.Error()
		return res, nil, err
	}
	res.RequestURL = reqURL

	client := &http.Client{Timeout: time.Duration(taDefaultInt(env.TimeoutSec, 10)) * time.Second}
	if env.SkipTLSVerify {
		client.Transport = taInsecureTransport()
	}

	resp, err := client.Do(req)
	if err != nil {
		res.DurationMs = int(time.Since(started).Milliseconds())
		res.Error = err.Error()
		return res, nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	res.DurationMs = int(time.Since(started).Milliseconds())
	res.HTTPStatus = resp.StatusCode
	res.RawSize = len(raw)

	taDebugf("env=%s [测试] HTTP %d 耗时 %dms 响应 %d 字节", env.Name, resp.StatusCode, res.DurationMs, len(raw))

	if resp.StatusCode >= 400 {
		res.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, taTruncate(string(raw), 300))
		return res, nil, fmt.Errorf(res.Error)
	}

	snaps, total, err := taParseRooms(env, raw)
	if err != nil {
		res.Error = err.Error()
		return res, nil, err
	}

	res.OK = true
	res.RecordCount = len(snaps)
	res.TotalCount = total
	for _, s := range snaps {
		if s.Maintaining {
			res.MaintainCnt++
		}
		if strings.EqualFold(s.Status, "Enable") {
			res.EnableCount++
		} else if strings.EqualFold(s.Status, "Disable") {
			res.DisableCount++
		}
	}

	// 回 3 条样例给页面，方便肉眼核对字段映射对不对
	sample := []map[string]interface{}{}
	for i, s := range snaps {
		if i >= 3 {
			break
		}
		sample = append(sample, map[string]interface{}{
			"table_no": s.TableNo, "room_no": s.RoomNo, "status": s.Status,
			"maintaining": s.Maintaining, "site_count": s.SiteCount,
			"operator": s.Operator, "update_time": s.UpdateTime,
		})
	}
	return res, sample, nil
}

func taMaskWebhook(w string) string {
	if w == "" {
		return ""
	}
	if len(w) <= 16 {
		return "****"
	}
	return w[:12] + "****" + w[len(w)-4:]
}

func taErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// taOperator 取当前操作人用户名，取不到就标 unknown（审计用，不参与鉴权）
func taOperator(r *http.Request) string {
	_, username, _ := GetUserFromContext(r)
	if strings.TrimSpace(username) == "" {
		if h := r.Header.Get("X-Operator"); h != "" {
			return h
		}
		return "unknown"
	}
	return username
}

// ---------------------------------------------------------------------------
// 权限
//
// 菜单权限 menu:table_alert 由前端路由守卫与侧边栏控制；
// 这里是按钮级权限的**服务端强制校验** —— 前端隐藏按钮只是体验，
// 真正拦住越权调用得靠这一层，否则直接打接口就绕过去了。
// admin / super_admin 由 UserHasPermission 内部放行。
// ---------------------------------------------------------------------------

const (
	taPermRead          = "table_alert:read"
	taPermEnvCreate     = "table_alert:env_create"
	taPermEnvUpdate     = "table_alert:env_update"
	taPermEnvDelete     = "table_alert:env_delete"
	taPermCollect       = "table_alert:collect"
	taPermRuleUpdate    = "table_alert:rule_update"
	taPermBotManage     = "table_alert:bot_manage"
	taPermContactManage = "table_alert:contact_manage"
	taPermAck           = "table_alert:ack"
	taPermViewRaw       = "table_alert:view_raw"
)

// taRequirePerm 校验按钮权限，不通过时直接写 403 并返回 false
func taRequirePerm(w http.ResponseWriter, r *http.Request, code string) bool {
	_, username, role := GetUserFromContext(r)
	ok, err := UserHasPermission(username, role, code)
	if err != nil {
		taErrorf("权限检查失败 username=%s code=%s err=%v", username, code, err)
		respondError(w, http.StatusInternalServerError, "权限检查失败")
		return false
	}
	if !ok {
		taInfof("拒绝越权操作：username=%s 缺少权限 %s", username, code)
		respondError(w, http.StatusForbidden, "权限不足："+code)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 例行维护窗口
// ---------------------------------------------------------------------------

// HandleTAListWindows GET /api/table-alert/windows?env_id=
func HandleTAListWindows(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	envID := r.URL.Query().Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}
	rows, err := database.DB.Query(`
		SELECT id, env_id, name, enabled, repeat_type, weekdays, month_days, once_date,
		       start_time, end_time, COALESCE(table_nos,''), action, overrun_alert, remark
		FROM table_alert_maint_windows WHERE env_id = ? ORDER BY start_time, name`, envID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	now := time.Now()
	for rows.Next() {
		var win TAMaintWindow
		if rows.Scan(&win.ID, &win.EnvID, &win.Name, &win.Enabled, &win.RepeatType, &win.Weekdays,
			&win.MonthDays, &win.OnceDate, &win.StartTime, &win.EndTime, &win.TableNos,
			&win.Action, &win.OverrunAlert, &win.Remark) != nil {
			continue
		}
		item := map[string]interface{}{
			"id": win.ID, "env_id": win.EnvID, "name": win.Name, "enabled": win.Enabled,
			"repeat_type": win.RepeatType, "weekdays": win.Weekdays, "month_days": win.MonthDays,
			"once_date": win.OnceDate, "start_time": win.StartTime, "end_time": win.EndTime,
			"table_nos": win.TableNos, "action": win.Action,
			"overrun_alert": win.OverrunAlert, "remark": win.Remark,
			"table_count": taCountTableNos(win.TableNos),
			"rule_text":   taWindowRuleText(&win),
		}
		// 当前是否正处于这个窗口内，页面上直接标出来
		if st, en, ok := taWindowInstance(&win, now); ok && !now.Before(st) && now.Before(en) {
			item["active_now"] = true
			item["current_end"] = en.Format("2006-01-02 15:04:05")
		} else {
			item["active_now"] = false
		}
		out = append(out, item)
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleTASaveWindow POST /api/table-alert/windows（无 id 新增，有 id 更新）
func HandleTASaveWindow(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRuleUpdate) {
		return
	}
	var win TAMaintWindow
	if err := json.NewDecoder(r.Body).Decode(&win); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if strings.TrimSpace(win.Name) == "" {
		respondError(w, http.StatusBadRequest, "窗口名称不能为空")
		return
	}
	if win.EnvID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}
	if _, _, ok := taParseHM(win.StartTime); !ok {
		respondError(w, http.StatusBadRequest, "开始时间格式应为 HH:MM")
		return
	}
	if _, _, ok := taParseHM(win.EndTime); !ok {
		respondError(w, http.StatusBadRequest, "结束时间格式应为 HH:MM")
		return
	}
	if strings.TrimSpace(win.TableNos) == "" {
		respondError(w, http.StatusBadRequest, "请至少指定一张桌台，或填 * 表示全部")
		return
	}
	// 重复规则的必填项要当场拦住，否则窗口永远不会命中，问题很难被发现
	switch win.RepeatType {
	case "weekly":
		if strings.TrimSpace(win.Weekdays) == "" {
			respondError(w, http.StatusBadRequest, "按周重复时必须选择星期")
			return
		}
	case "monthly":
		if strings.TrimSpace(win.MonthDays) == "" {
			respondError(w, http.StatusBadRequest, "按月重复时必须选择日期")
			return
		}
	case "once":
		if strings.TrimSpace(win.OnceDate) == "" {
			respondError(w, http.StatusBadRequest, "指定日期不能为空")
			return
		}
	default:
		win.RepeatType = "daily"
	}
	if win.Action != "suppress" {
		win.Action = "annotate"
	}

	var err error
	if win.ID == "" {
		win.ID = uuid.New().String()
		_, err = database.DB.Exec(`
			INSERT INTO table_alert_maint_windows
			  (id, env_id, name, enabled, repeat_type, weekdays, month_days, once_date,
			   start_time, end_time, table_nos, action, overrun_alert, remark, created_by)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			win.ID, win.EnvID, win.Name, win.Enabled, win.RepeatType, win.Weekdays,
			win.MonthDays, win.OnceDate, win.StartTime, win.EndTime, win.TableNos,
			win.Action, win.OverrunAlert, win.Remark, taOperator(r))
	} else {
		_, err = database.DB.Exec(`
			UPDATE table_alert_maint_windows SET
			  name=?, enabled=?, repeat_type=?, weekdays=?, month_days=?, once_date=?,
			  start_time=?, end_time=?, table_nos=?, action=?, overrun_alert=?, remark=?
			WHERE id=?`,
			win.Name, win.Enabled, win.RepeatType, win.Weekdays, win.MonthDays, win.OnceDate,
			win.StartTime, win.EndTime, win.TableNos, win.Action, win.OverrunAlert,
			win.Remark, win.ID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	taInfof("例行维护窗口「%s」已保存：%s，适用 %d 张桌台（操作人 %s）",
		win.Name, taWindowRuleText(&win), taCountTableNos(win.TableNos), taOperator(r))
	respondJSON(w, http.StatusOK, map[string]string{"id": win.ID, "message": "已保存"})
}

// HandleTADeleteWindow DELETE /api/table-alert/windows/{id}
func HandleTADeleteWindow(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRuleUpdate) {
		return
	}
	id := mux.Vars(r)["id"]
	if _, err := database.DB.Exec(`DELETE FROM table_alert_maint_windows WHERE id=?`, id); err != nil {
		respondError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已删除"})
}

// taWindowRuleText 把重复规则拼成一句人话，页面和日志共用
func taWindowRuleText(w *TAMaintWindow) string {
	span := w.StartTime + "-" + w.EndTime
	if sh, sm, ok1 := taParseHM(w.StartTime); ok1 {
		if eh, em, ok2 := taParseHM(w.EndTime); ok2 && (eh*60+em) <= (sh*60+sm) {
			span += "（次日）"
		}
	}
	switch w.RepeatType {
	case "weekly":
		names := map[int]string{1: "一", 2: "二", 3: "三", 4: "四", 5: "五", 6: "六", 7: "日"}
		var ds []string
		for _, p := range strings.Split(w.Weekdays, ",") {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &n); err == nil {
				if nm, ok := names[n]; ok {
					ds = append(ds, "周"+nm)
				}
			}
		}
		return strings.Join(ds, "、") + " " + span
	case "monthly":
		var ds []string
		for _, p := range strings.Split(w.MonthDays, ",") {
			if p = strings.TrimSpace(p); p != "" {
				ds = append(ds, p+"号")
			}
		}
		return "每月 " + strings.Join(ds, "、") + " " + span
	case "once":
		return w.OnceDate + " " + span
	default:
		return "每天 " + span
	}
}

func taCountTableNos(list string) int {
	n := 0
	for _, p := range strings.Split(list, ",") {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// 站点管理
// ---------------------------------------------------------------------------

// HandleTAListSites GET /api/table-alert/sites?env_id=&watched=&named=&q=
func HandleTAListSites(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	q := r.URL.Query()
	envID := q.Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}

	where := []string{"env_id = ?"}
	args := []interface{}{envID}
	if q.Get("watched") == "1" {
		where = append(where, "watched = 1")
	}
	switch q.Get("named") {
	case "0":
		where = append(where, "site_name = ''")
	case "1":
		where = append(where, "site_name <> ''")
	}
	if kw := strings.TrimSpace(q.Get("q")); kw != "" {
		where = append(where, "(site_id LIKE ? OR site_name LIKE ?)")
		args = append(args, "%"+kw+"%", "%"+kw+"%")
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total, named, watched int
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_sites WHERE env_id=?`, envID).Scan(&total)
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_sites WHERE env_id=? AND site_name<>''`, envID).Scan(&named)
	database.DB.QueryRow(`SELECT COUNT(*) FROM table_alert_sites WHERE env_id=? AND watched=1`, envID).Scan(&watched)

	rows, err := database.DB.Query(`
		SELECT id, site_id, site_name, watched, table_count, remark,
		       DATE_FORMAT(last_seen_at,'%Y-%m-%d %H:%i:%s')
		FROM table_alert_sites`+whereSQL+`
		ORDER BY watched DESC, table_count DESC, site_id
		LIMIT 500`, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	items := []map[string]interface{}{}
	for rows.Next() {
		var id, siteID, name, remark string
		var isWatched bool
		var tableCount int
		var lastSeen sql.NullString
		if rows.Scan(&id, &siteID, &name, &isWatched, &tableCount, &remark, &lastSeen) != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id": id, "site_id": siteID, "site_name": name, "watched": isWatched,
			"table_count": tableCount, "remark": remark, "last_seen_at": lastSeen.String,
		})
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"stats": map[string]int{"total": total, "named": named, "watched": watched},
	})
}

// HandleTASaveSite PUT /api/table-alert/sites/{id}  改名 / 设关注
func HandleTASaveSite(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRuleUpdate) {
		return
	}
	id := mux.Vars(r)["id"]
	var body struct {
		SiteName string `json:"site_name"`
		Watched  bool   `json:"watched"`
		Remark   string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if _, err := database.DB.Exec(`
		UPDATE table_alert_sites SET site_name=?, watched=?, remark=? WHERE id=?`,
		strings.TrimSpace(body.SiteName), body.Watched, body.Remark, id); err != nil {
		respondError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "已保存"})
}

// HandleTABatchWatchSites POST /api/table-alert/sites/watch  批量设/取消关注
func HandleTABatchWatchSites(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRuleUpdate) {
		return
	}
	var body struct {
		IDs     []string `json:"ids"`
		Watched bool     `json:"watched"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		respondError(w, http.StatusBadRequest, "请选择站点")
		return
	}
	n := 0
	for _, id := range body.IDs {
		if _, err := database.DB.Exec(`UPDATE table_alert_sites SET watched=? WHERE id=?`,
			body.Watched, id); err == nil {
			n++
		}
	}
	taInfof("%s 批量%s %d 个站点", taOperator(r), map[bool]string{true: "关注", false: "取消关注"}[body.Watched], n)
	respondJSON(w, http.StatusOK, map[string]interface{}{"message": "已保存", "count": n})
}

// HandleTARoomSites GET /api/table-alert/rooms/{room_id}/sites?env_id=
// 桌台详情：列出这次维护涉及的**全部**站点，关注的排在前面并标记
func HandleTARoomSites(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	roomID := mux.Vars(r)["room_id"]
	envID := r.URL.Query().Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}

	var siteIDsRaw, tableNo string
	err := database.DB.QueryRow(`
		SELECT COALESCE(maintain_site_ids,''), table_no FROM table_alert_rooms
		WHERE env_id=? AND room_id=?`, envID, roomID).Scan(&siteIDsRaw, &tableNo)
	if err != nil {
		respondError(w, http.StatusNotFound, "桌台不存在")
		return
	}

	// 一次取出这些 siteId 的名称与关注状态，别在循环里查库
	known := map[string]struct {
		Name    string
		Watched bool
	}{}
	rows, err := database.DB.Query(`
		SELECT site_id, site_name, watched FROM table_alert_sites WHERE env_id=?`, envID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sid, name string
			var watched bool
			if rows.Scan(&sid, &name, &watched) == nil {
				known[sid] = struct {
					Name    string
					Watched bool
				}{name, watched}
			}
		}
	}

	watchedList := []map[string]interface{}{}
	otherList := []map[string]interface{}{}
	for _, sid := range strings.Split(siteIDsRaw, ",") {
		sid = strings.TrimSpace(sid)
		if sid == "" {
			continue
		}
		info := known[sid]
		row := map[string]interface{}{
			"site_id": sid, "site_name": info.Name, "watched": info.Watched,
		}
		if info.Watched {
			watchedList = append(watchedList, row)
		} else {
			otherList = append(otherList, row)
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"table_no": tableNo,
		"watched":  watchedList,
		"others":   otherList,
		"total":    len(watchedList) + len(otherList),
	})
}
