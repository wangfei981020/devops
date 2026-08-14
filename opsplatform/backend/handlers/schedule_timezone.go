package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"opsplatform/database"
)

// v772 跨时区排班
//
// 设计要点（改之前先看这段）：
//  1. 时区存 IANA 名（Europe/Belgrade），不存 UTC+2 这种固定偏移。塞尔维亚 3 月底 -> 10 月底是
//     夏令时 CEST(UTC+2)，其余时间是 CET(UTC+1)，跟北京的时差在 6 / 7 小时之间来回切。
//     存固定偏移的话，切换当天起整张表会静默错 1 小时，不报错也没人会发现。
//  2. 时区带生效日期，一人一段历史。改时区 = 新增一条记录，不会追溯改写历史排班的换算结果。
//  3. 时区换算只在前端做一份（浏览器自带 tzdata 的 Intl），后端只负责存和校验，
//     避免前后端两套换算实现分叉。后端唯一用到 tzdata 的地方就是这里的写入校验。

// EmployeeTimezone 员工某段时间所在的时区
type EmployeeTimezone struct {
	ID            int    `json:"id"`
	EmployeeID    int    `json:"employee_id"`
	Timezone      string `json:"timezone"`
	EffectiveFrom string `json:"effective_from"` // YYYY-MM-DD
}

// getAllEmployeeTimezones 取所有员工的时区历史，按生效日期升序。
// 一次全取：排班页员工数量是十几个的量级，不值得为此加分页或按需查询。
func getAllEmployeeTimezones() (map[int][]EmployeeTimezone, error) {
	rows, err := database.DB.Query(`
		SELECT id, employee_id, timezone, DATE_FORMAT(effective_from, '%Y-%m-%d')
		FROM schedule_employee_timezones
		ORDER BY employee_id, effective_from
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int][]EmployeeTimezone)
	for rows.Next() {
		var tz EmployeeTimezone
		if err := rows.Scan(&tz.ID, &tz.EmployeeID, &tz.Timezone, &tz.EffectiveFrom); err != nil {
			log.Printf("[排班] WARN 扫描员工时区记录失败: %v", err)
			continue
		}
		result[tz.EmployeeID] = append(result[tz.EmployeeID], tz)
	}
	return result, nil
}

// resolveTimezoneAt 解析某员工在某天用哪个时区：取 effective_from <= date 的最后一条。
// 一条都匹配不上（比如日期早于基线记录）时回落默认时区，并且必须 WARN——
// 静默回落等于把「时区没配对」显示成「配好了」。
func resolveTimezoneAt(history []EmployeeTimezone, date string) string {
	sortTimezones(history)
	tz := ""
	for _, h := range history {
		if h.EffectiveFrom > date {
			break
		}
		tz = h.Timezone
	}
	if tz == "" {
		if len(history) > 0 {
			log.Printf("[排班] WARN 日期 %s 早于员工 %d 的最早时区记录(%s)，回落 %s",
				date, history[0].EmployeeID, history[0].EffectiveFrom, database.ScheduleDefaultTimezone)
		} else {
			log.Printf("[排班] WARN 员工没有任何时区记录，日期 %s 回落 %s", date, database.ScheduleDefaultTimezone)
		}
		return database.ScheduleDefaultTimezone
	}
	return tz
}

// HandleGetScheduleTimezones GET /api/schedule/timezones
// 返回全部员工的时区历史，前端据此按天解析每个格子该用哪个时区
func HandleGetScheduleTimezones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	all, err := getAllEmployeeTimezones()
	if err != nil {
		log.Printf("[排班] 获取员工时区历史失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// map 的 key 是 int，JSON 里会变成字符串键，前端按 String(id) 取
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"default_timezone": database.ScheduleDefaultTimezone,
		"timezones":        all,
	})
}

// HandleSaveScheduleTimezone POST /api/schedule/timezone
// 新增或修改一条「某员工从某天起在某时区」。同一员工同一生效日期视为修改。
func HandleSaveScheduleTimezone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EmployeeID    int    `json:"employee_id"`
		Timezone      string `json:"timezone"`
		EffectiveFrom string `json:"effective_from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[排班] 时区请求解析失败: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Timezone = strings.TrimSpace(req.Timezone)
	req.EffectiveFrom = strings.TrimSpace(req.EffectiveFrom)

	if req.EmployeeID == 0 {
		http.Error(w, "缺少 employee_id", http.StatusBadRequest)
		return
	}
	// 校验时区名。这一步依赖 tzdata（main.go 里 import _ "time/tzdata" 内嵌），
	// 目的是把 "Europe/Belgrad" 这种拼错在写入时就打回去，而不是等到页面上时间显示错了才发现。
	if _, err := time.LoadLocation(req.Timezone); err != nil || req.Timezone == "" || req.Timezone == "Local" {
		log.Printf("[排班] WARN 拒绝无法识别的时区: employee_id=%d timezone=%q err=%v", req.EmployeeID, req.Timezone, err)
		http.Error(w, fmt.Sprintf("无法识别的时区: %q（需要 IANA 时区名，如 Europe/Belgrade、Asia/Shanghai）", req.Timezone), http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("2006-01-02", req.EffectiveFrom); err != nil {
		log.Printf("[排班] WARN 生效日期格式错误: employee_id=%d effective_from=%q", req.EmployeeID, req.EffectiveFrom)
		http.Error(w, "生效日期格式应为 YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	var empName string
	if err := database.DB.QueryRow(`SELECT name FROM schedule_employees WHERE id = ?`, req.EmployeeID).Scan(&empName); err != nil {
		log.Printf("[排班] 时区设置失败，员工不存在: id=%d err=%v", req.EmployeeID, err)
		http.Error(w, "员工不存在", http.StatusBadRequest)
		return
	}

	// 记录改动前的值，审计里能看出「从什么改成了什么」
	var before string
	database.DB.QueryRow(`
		SELECT timezone FROM schedule_employee_timezones WHERE employee_id = ? AND effective_from = ?
	`, req.EmployeeID, req.EffectiveFrom).Scan(&before)

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	_, err := database.DB.Exec(`
		INSERT INTO schedule_employee_timezones (employee_id, timezone, effective_from, created_by)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE timezone = VALUES(timezone), created_by = VALUES(created_by)
	`, req.EmployeeID, req.Timezone, req.EffectiveFrom, operator)
	if err != nil {
		log.Printf("[排班] 保存员工时区失败: employee_id=%d err=%v", req.EmployeeID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[排班] 设置员工时区: %s(id=%d) 从 %s 起 %s -> %s, 操作人=%s",
		empName, req.EmployeeID, req.EffectiveFrom, orDash(before), req.Timezone, operator)

	AddAuditLogFromRequest(r, "设置排班员工时区", fmt.Sprintf("%d", req.EmployeeID), operator,
		fmt.Sprintf(`{"timezone":"%s"}`, before),
		fmt.Sprintf(`{"timezone":"%s","effective_from":"%s"}`, req.Timezone, req.EffectiveFrom),
		fmt.Sprintf("设置员工 %s 时区: %s 起 %s", empName, req.EffectiveFrom, req.Timezone))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleDeleteScheduleTimezone DELETE /api/schedule/timezone?id=
// 拒绝删掉某员工最早的一条：那是基线，删了之后早于剩余记录的日期就没有时区可解析。
func HandleDeleteScheduleTimezone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id == 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	var empID int
	var tz, from string
	if err := database.DB.QueryRow(`
		SELECT employee_id, timezone, DATE_FORMAT(effective_from, '%Y-%m-%d')
		FROM schedule_employee_timezones WHERE id = ?
	`, id).Scan(&empID, &tz, &from); err != nil {
		http.Error(w, "记录不存在", http.StatusBadRequest)
		return
	}

	var earliest string
	database.DB.QueryRow(`
		SELECT DATE_FORMAT(MIN(effective_from), '%Y-%m-%d') FROM schedule_employee_timezones WHERE employee_id = ?
	`, empID).Scan(&earliest)
	if earliest == from {
		log.Printf("[排班] 拒绝删除员工 %d 的最早时区记录(%s %s)：这是基线记录", empID, from, tz)
		http.Error(w, "这是该员工最早的一条时区记录（基线），不能删除。可以直接修改它的时区。", http.StatusBadRequest)
		return
	}

	if _, err := database.DB.Exec(`DELETE FROM schedule_employee_timezones WHERE id = ?`, id); err != nil {
		log.Printf("[排班] 删除员工时区记录失败: id=%d err=%v", id, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	log.Printf("[排班] 删除员工时区记录: employee_id=%d %s 起 %s, 操作人=%s", empID, from, tz, operator)
	AddAuditLogFromRequest(r, "删除排班员工时区", fmt.Sprintf("%d", empID), operator,
		fmt.Sprintf(`{"timezone":"%s","effective_from":"%s"}`, tz, from), "",
		fmt.Sprintf("删除员工 ID=%d 的时区记录: %s 起 %s", empID, from, tz))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// parseShiftTimeRange 解析班次时间段，返回「距零点的分钟数」和时长（分钟）。
// 支持 "09:00-18:00"、"24:00-09:00"（24:00 = 次日 00:00）、"全天"（00:00-24:00）。
// "-" / 空 表示不是上班班次（休假类），返回 ok=false。
//
// ⚠️ 时长是算出来的，不是「结束时刻 - 开始时刻」分别换算的结果。跨夏令时切换点的班次
// 如果把起止时刻各自换算，时长会凭空多出或少掉 1 小时。前端 timezone.js 里同一套算法。
func parseShiftTimeRange(timeRange string) (startMin, durationMin int, ok bool) {
	s := strings.TrimSpace(timeRange)
	if s == "" || s == "-" {
		return 0, 0, false
	}
	if s == "全天" || strings.EqualFold(s, "all day") {
		return 0, 1440, true
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err1 := parseHHMM(parts[0])
	end, err2 := parseHHMM(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	d := end - start
	if d <= 0 {
		d += 1440 // 跨天，如 24:00-09:00
	}
	// ⚠️ start 不能对 1440 取模。晚班「24:00-09:00」的 24:00 指当天结束那一刻（次日 0 点），
	// 取模成 0 会把整个班次提前一整天。和前端 timezone.js 的 parseTimeRange 保持一致。
	return start, d, true
}

func parseHHMM(v string) (int, error) {
	v = strings.TrimSpace(v)
	seg := strings.Split(v, ":")
	if len(seg) != 2 {
		return 0, fmt.Errorf("时间格式应为 HH:MM，收到 %q", v)
	}
	h, err := strconv.Atoi(strings.TrimSpace(seg[0]))
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(strings.TrimSpace(seg[1]))
	if err != nil {
		return 0, err
	}
	if h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, fmt.Errorf("时间超出范围: %q", v)
	}
	return h*60 + m, nil
}

// warnShiftDSTHazard 班次起点落在 02:00-02:59 时告警。
// 欧洲（含塞尔维亚）的夏令时切换发生在当地 02:00：3 月那天 02:00 直接跳到 03:00（这个时刻不存在），
// 10 月那天 02:00-03:00 走两遍（这个时刻有歧义）。起点压在这一小时的班次，换算结果必然有一天是错的。
// 现有班次起点是 09:00 / 15:00 / 24:00，都避开了，这里是给以后新增班次的人留的提醒。
func warnShiftDSTHazard(code, timeRange string) {
	startMin, _, ok := parseShiftTimeRange(timeRange)
	if !ok {
		return
	}
	if startMin >= 120 && startMin < 180 {
		log.Printf("[排班] WARN 班次 %s 的开始时刻 %s 落在 02:00-03:00，"+
			"欧洲时区的夏令时切换正好在这一小时（3 月该时刻不存在、10 月走两遍），"+
			"跨时区换算在切换当天会出错，建议避开", code, timeRange)
	}
}

// sortTimezones 按生效日期升序，保证前端拿到的历史是有序的
func sortTimezones(list []EmployeeTimezone) {
	sort.Slice(list, func(i, j int) bool { return list[i].EffectiveFrom < list[j].EffectiveFrom })
}

func orDash(s string) string {
	if s == "" {
		return "(无)"
	}
	return s
}
